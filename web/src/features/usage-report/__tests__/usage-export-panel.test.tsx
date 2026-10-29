/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { createUsageExport, downloadUsageExport, getUsageExports } from '../api'
import { UsageExportPanel } from '../components/usage-export-panel'
import type { UsageExportJob, UsageQueryParams } from '../types'

vi.mock('../api', () => ({
  createUsageExport: vi.fn(),
  downloadUsageExport: vi.fn(),
  getUsageExports: vi.fn(),
}))

const mockedCreate = vi.mocked(createUsageExport)
const mockedDownload = vi.mocked(downloadUsageExport)
const mockedList = vi.mocked(getUsageExports)

const scope: UsageQueryParams = {
  view: 'platform',
  organization_id: 3,
  user_id: 9,
  start_timestamp: 1_700_000_000,
  end_timestamp: 1_700_600_000,
  tz_offset_minutes: -480,
}

function job(overrides: Partial<UsageExportJob>): UsageExportJob {
  return {
    job_id: 'job-ready',
    status: 'succeeded',
    view: 'platform',
    organization_id: 3,
    target_user_id: 9,
    start_timestamp: scope.start_timestamp,
    end_timestamp: scope.end_timestamp,
    tz_offset_minutes: -480,
    kind: 'records',
    format: 'csv',
    with_bom: true,
    row_count: 42,
    file_name: 'usage-records-job-ready.csv',
    error_code: '',
    created_time: 1_700_000_000,
    started_time: 1_700_000_001,
    finished_time: 1_700_000_002,
    expires_time: 1_700_600_000,
    download_url: '/api/usage/exports/job-ready/download',
    ...overrides,
  }
}

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <UsageExportPanel params={scope} />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
  Reflect.deleteProperty(URL, 'createObjectURL')
  Reflect.deleteProperty(URL, 'revokeObjectURL')
})

describe('UsageExportPanel', () => {
  it('offers a download only for jobs whose file is still available', async () => {
    mockedList.mockResolvedValue([
      job({ job_id: 'ready', status: 'succeeded' }),
      job({ job_id: 'old', status: 'expired' }),
      job({ job_id: 'broken', status: 'failed', row_count: 0 }),
    ])

    renderPanel()

    expect(await screen.findByText('Expired')).toBeInTheDocument()
    expect(screen.getByText('Failed')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Download' })).toHaveLength(1)
  })

  it('starts a records CSV export for the scope and window on screen', async () => {
    mockedList.mockResolvedValue([])
    mockedCreate.mockResolvedValue(job({ status: 'queued' }))

    renderPanel()
    await userEvent.click(
      await screen.findByRole('button', { name: 'Usage records (CSV)' })
    )

    await waitFor(() =>
      expect(mockedCreate).toHaveBeenCalledWith({
        view: 'platform',
        organization_id: 3,
        user_id: 9,
        start_timestamp: scope.start_timestamp,
        end_timestamp: scope.end_timestamp,
        tz_offset_minutes: -480,
        format: 'csv',
        kind: 'records',
        with_bom: true,
      })
    )
  })

  it('tells the user an expired file must be exported again', async () => {
    mockedList.mockResolvedValue([job({ job_id: 'ready' })])
    mockedDownload.mockRejectedValue(
      Object.assign(new Error('gone'), {
        response: { status: 410, data: {} },
      })
    )
    const errorToast = vi.spyOn(toast, 'error')

    renderPanel()
    await userEvent.click(
      await screen.findByRole('button', { name: 'Download' })
    )

    await waitFor(() =>
      expect(errorToast).toHaveBeenCalledWith('This export has expired')
    )
  })

  it('saves a ready file under the name the server gave it', async () => {
    mockedList.mockResolvedValue([job({ job_id: 'ready' })])
    mockedDownload.mockResolvedValue(new Blob(['usage']))
    const createObjectURL = vi.fn(() => 'blob:usage')
    const revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', {
      value: createObjectURL,
      configurable: true,
    })
    Object.defineProperty(URL, 'revokeObjectURL', {
      value: revokeObjectURL,
      configurable: true,
    })
    const savedNames: string[] = []
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement
    ) {
      savedNames.push(this.download)
    })

    renderPanel()
    await userEvent.click(
      await screen.findByRole('button', { name: 'Download' })
    )

    await waitFor(() =>
      expect(savedNames).toEqual(['usage-records-job-ready.csv'])
    )
    expect(mockedDownload).toHaveBeenCalledWith('ready')
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:usage')
  })

  it('keeps polling a generating export until it is ready', async () => {
    mockedList
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        job({ job_id: 'busy', status: 'running', row_count: 0 }),
      ])
      .mockResolvedValue([job({ job_id: 'busy', status: 'succeeded' })])
    mockedCreate.mockResolvedValue(
      job({ job_id: 'busy', status: 'queued', row_count: 0 })
    )

    renderPanel()
    await userEvent.click(
      await screen.findByRole('button', { name: 'Usage records (CSV)' })
    )

    expect(await screen.findByText('Generating')).toBeInTheDocument()
    expect(
      await screen.findByRole('button', { name: 'Download' }, { timeout: 6000 })
    ).toBeInTheDocument()
  })

  it('explains a refused export with the row-limit message for its server code', async () => {
    mockedList.mockResolvedValue([])
    mockedCreate.mockRejectedValue(
      Object.assign(new Error('The export exceeds the row limit'), {
        response: {
          status: 400,
          data: { success: false, code: 'USAGE_EXPORT_TOO_LARGE' },
        },
      })
    )
    const errorToast = vi.spyOn(toast, 'error')

    renderPanel()
    await userEvent.click(
      await screen.findByRole('button', { name: 'Usage workbook (XLSX)' })
    )

    await waitFor(() =>
      expect(errorToast).toHaveBeenCalledWith(
        'The export exceeds the row limit. Narrow the range or the scope, or use CSV.'
      )
    )
  })
})
