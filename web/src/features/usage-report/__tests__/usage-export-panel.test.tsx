import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  createUsageExport,
  downloadUsageExport,
  downloadUsageExportDirect,
  getUsageExportEstimate,
  getUsageExports,
} from '../api'
import { UsageExportPanel } from '../components/usage-export-panel'
import type { UsageExportJob, UsageQueryParams } from '../types'

vi.mock('../api', () => ({
  createUsageExport: vi.fn(),
  downloadUsageExport: vi.fn(),
  downloadUsageExportDirect: vi.fn(),
  getUsageExportEstimate: vi.fn(),
  getUsageExports: vi.fn(),
}))

const mockedCreate = vi.mocked(createUsageExport)
const mockedDownload = vi.mocked(downloadUsageExport)
const mockedDirect = vi.mocked(downloadUsageExportDirect)
const mockedEstimate = vi.mocked(getUsageExportEstimate)
const mockedList = vi.mocked(getUsageExports)

const scope: UsageQueryParams = {
  view: 'platform',
  organization_id: 3,
  user_id: 9,
  start_timestamp: 1_700_000_000,
  end_timestamp: 1_700_600_000,
  tz_offset_minutes: -480,
}

const scopeLabels = {
  organization: 'Acme Labs',
  member: 'alice',
  model: 'All models',
}

const recordsRequest = {
  view: 'platform',
  organization_id: 3,
  user_id: 9,
  start_timestamp: scope.start_timestamp,
  end_timestamp: scope.end_timestamp,
  tz_offset_minutes: -480,
  format: 'csv',
  kind: 'records',
  with_bom: true,
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
      <UsageExportPanel params={scope} scope={scopeLabels} />
    </QueryClientProvider>
  )
}

// Choosing an export opens its confirmation; the export starts only after the user confirms.
async function startExport(label: string) {
  await userEvent.click(await screen.findByRole('button', { name: label }))
  await userEvent.click(
    await screen.findByRole('button', { name: 'Start export' })
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

  it('shows the scope, rows, and delivery before a queued export starts', async () => {
    mockedList.mockResolvedValue([])
    mockedEstimate.mockResolvedValue({ delivery: 'queued', row_count: 9000 })

    renderPanel()
    await userEvent.click(
      await screen.findByRole('button', { name: 'Usage records (CSV)' })
    )

    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText('Acme Labs')).toBeInTheDocument()
    expect(within(dialog).getByText('alice')).toBeInTheDocument()
    expect(within(dialog).getByText('9,000')).toBeInTheDocument()
    expect(
      within(dialog).getByText(
        'Added to the export list. The file is kept for 24 hours.'
      )
    ).toBeInTheDocument()
    expect(mockedCreate).not.toHaveBeenCalled()
  })

  it('shows the row count a summary export will write', async () => {
    mockedList.mockResolvedValue([])
    mockedEstimate.mockResolvedValue({ delivery: 'direct', row_count: 4 })

    renderPanel()
    await userEvent.click(
      await screen.findByRole('button', { name: 'Usage summary (CSV)' })
    )

    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText('4')).toBeInTheDocument()
    expect(within(dialog).queryByText('-')).not.toBeInTheDocument()
  })

  it('does not start an export when the confirmation is cancelled', async () => {
    mockedList.mockResolvedValue([])
    mockedEstimate.mockResolvedValue({ delivery: 'direct', row_count: 12 })

    renderPanel()
    await userEvent.click(
      await screen.findByRole('button', { name: 'Usage records (CSV)' })
    )
    await userEvent.click(await screen.findByRole('button', { name: 'Cancel' }))

    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    expect(mockedDirect).not.toHaveBeenCalled()
    expect(mockedCreate).not.toHaveBeenCalled()
  })

  it('queues a large records CSV export for the scope and window on screen', async () => {
    mockedList.mockResolvedValue([])
    mockedEstimate.mockResolvedValue({ delivery: 'queued', row_count: 9000 })
    mockedCreate.mockResolvedValue(job({ status: 'queued' }))

    renderPanel()
    await startExport('Usage records (CSV)')

    await waitFor(() =>
      expect(mockedCreate).toHaveBeenCalledWith(recordsRequest)
    )
    expect(mockedEstimate).toHaveBeenCalledWith(recordsRequest)
    expect(mockedDirect).not.toHaveBeenCalled()
  })

  it('saves a small export directly under the name the server gave it', async () => {
    mockedList.mockResolvedValue([])
    mockedEstimate.mockResolvedValue({ delivery: 'direct', row_count: 12 })
    mockedDirect.mockResolvedValue({
      blob: new Blob(['usage']),
      fileName: 'usage-records-now.csv',
    })
    Object.defineProperty(URL, 'createObjectURL', {
      value: vi.fn(() => 'blob:direct'),
      configurable: true,
    })
    Object.defineProperty(URL, 'revokeObjectURL', {
      value: vi.fn(),
      configurable: true,
    })
    const savedNames: string[] = []
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement
    ) {
      savedNames.push(this.download)
    })

    renderPanel()
    await startExport('Usage records (CSV)')

    await waitFor(() => expect(savedNames).toEqual(['usage-records-now.csv']))
    expect(mockedCreate).not.toHaveBeenCalled()
  })

  it('explains a refused direct download when the download slots are full', async () => {
    mockedList.mockResolvedValue([])
    mockedEstimate.mockResolvedValue({ delivery: 'direct', row_count: 12 })
    mockedDirect.mockRejectedValue(
      Object.assign(new Error('Too many downloads are running'), {
        response: {
          status: 429,
          data: { success: false, code: 'USAGE_DOWNLOAD_BUSY' },
        },
      })
    )
    const errorToast = vi.spyOn(toast, 'error')

    renderPanel()
    await startExport('Usage records (CSV)')

    await waitFor(() =>
      expect(errorToast).toHaveBeenCalledWith(
        'Too many downloads are running. Try again shortly.'
      )
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
    mockedEstimate.mockResolvedValue({ delivery: 'queued', row_count: 9000 })
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
    await startExport('Usage records (CSV)')

    expect(await screen.findByText('Generating')).toBeInTheDocument()
    expect(
      await screen.findByRole('button', { name: 'Download' }, { timeout: 6000 })
    ).toBeInTheDocument()
  })

  it('explains a refused export with the row-limit message for its server code', async () => {
    mockedEstimate.mockResolvedValue({ delivery: 'queued', row_count: 9000 })
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
    await startExport('Usage workbook (XLSX)')

    await waitFor(() =>
      expect(errorToast).toHaveBeenCalledWith(
        'The export exceeds the row limit. Narrow the range or the scope, or use CSV.'
      )
    )
  })
})
