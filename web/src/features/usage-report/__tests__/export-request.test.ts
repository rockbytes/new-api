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
import { describe, expect, it } from 'vitest'

import { USAGE_EXPORT_OPTIONS } from '../constants'
import { buildUsageExportRequest, hasActiveUsageExports } from '../lib/export'
import type { UsageExportJob, UsageQueryParams } from '../types'

const scope: UsageQueryParams = {
  view: 'platform',
  organization_id: 3,
  user_id: 9,
  start_timestamp: 1_700_000_000,
  end_timestamp: 1_700_600_000,
  tz_offset_minutes: -480,
  page: 4,
  page_size: 20,
}

function optionFor(key: string) {
  const option = USAGE_EXPORT_OPTIONS.find((item) => item.key === key)
  if (!option) throw new Error(`no export option ${key}`)
  return option
}

describe('buildUsageExportRequest', () => {
  it('carries the report scope and window, but no paging, for a records CSV', () => {
    expect(buildUsageExportRequest(scope, optionFor('records'))).toEqual({
      view: 'platform',
      organization_id: 3,
      user_id: 9,
      start_timestamp: 1_700_000_000,
      end_timestamp: 1_700_600_000,
      tz_offset_minutes: -480,
      format: 'csv',
      kind: 'records',
      with_bom: true,
    })
  })

  it('asks for the summary tables when the summary CSV is chosen', () => {
    const request = buildUsageExportRequest(scope, optionFor('summary'))
    expect(request.format).toBe('csv')
    expect(request.kind).toBe('summary')
  })

  it('sends only the format for a workbook, since it always holds every table', () => {
    const request = buildUsageExportRequest(scope, optionFor('workbook'))
    expect(request.format).toBe('xlsx')
    expect(request).not.toHaveProperty('kind')
    expect(request).not.toHaveProperty('with_bom')
  })
})

describe('hasActiveUsageExports', () => {
  function job(status: UsageExportJob['status']): UsageExportJob {
    return { status } as UsageExportJob
  }

  it.each([
    { name: 'no jobs', jobs: [], active: false },
    {
      name: 'only finished jobs',
      jobs: [job('succeeded'), job('failed'), job('expired')],
      active: false,
    },
    {
      name: 'a queued job',
      jobs: [job('succeeded'), job('queued')],
      active: true,
    },
    { name: 'a running job', jobs: [job('running')], active: true },
  ])('is $active for $name', ({ jobs, active }) => {
    expect(hasActiveUsageExports(jobs)).toBe(active)
  })
})
