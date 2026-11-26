import { describe, expect, it } from 'vitest'

import { buildUsageExportRequest, hasActiveUsageExports } from '../lib/export'
import type { UsageExportJob, UsageQueryParams } from '../types'

const scope: UsageQueryParams = {
  view: 'platform',
  payer_type: 'organization',
  organization_id: 3,
  user_id: 9,
  start_timestamp: 1_700_000_000,
  end_timestamp: 1_700_600_000,
  tz_offset_minutes: -480,
  page: 4,
  page_size: 20,
}

describe('buildUsageExportRequest', () => {
  it('carries the report scope and window, but no paging, for a summary CSV', () => {
    expect(buildUsageExportRequest(scope, 'csv')).toEqual({
      view: 'platform',
      payer_type: 'organization',
      organization_id: 3,
      user_id: 9,
      start_timestamp: 1_700_000_000,
      end_timestamp: 1_700_600_000,
      tz_offset_minutes: -480,
      format: 'csv',
      kind: 'summary',
      with_bom: true,
    })
  })

  it('requests summary data for a workbook without a BOM', () => {
    const request = buildUsageExportRequest(scope, 'xlsx')
    expect(request.format).toBe('xlsx')
    expect(request.kind).toBe('summary')
    expect(request).not.toHaveProperty('with_bom')
  })

  it('carries the selected model so the file matches the report on screen', () => {
    const request = buildUsageExportRequest(
      { ...scope, model_name: 'gpt-4o' },
      'csv'
    )
    expect(request.model_name).toBe('gpt-4o')
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
