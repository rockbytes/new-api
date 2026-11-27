import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { downloadUsageExport, getUsageExports, getUsageSummary } from '../api'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

const params = {
  payer_type: 'personal' as const,
  start_timestamp: 1,
  end_timestamp: 2,
  tz_offset_minutes: 0,
}

describe('versioned usage API', () => {
  beforeEach(() => vi.clearAllMocks())

  it('requests a payer-specific report from v2', async () => {
    vi.mocked(api.get).mockResolvedValueOnce({
      data: { success: true, data: {} },
    })
    await getUsageSummary(params)
    expect(api.get).toHaveBeenCalledWith('/api/usage/v2/summary', { params })
  })

  it('does not retry against the legacy API when v2 is unavailable', async () => {
    vi.mocked(api.get).mockRejectedValueOnce(new Error('not found'))
    await expect(getUsageSummary(params)).rejects.toThrow('not found')
    expect(api.get).toHaveBeenCalledTimes(1)
  })

  it('rejects a JSON error returned as a successful blob', async () => {
    vi.mocked(api.get).mockResolvedValueOnce({
      data: new Blob(['{"success":false,"message":"unavailable"}'], {
        type: 'application/json',
      }),
    })
    await expect(downloadUsageExport('old-job')).rejects.toThrow('unavailable')
  })

  it('opts in to direct download history on the versioned list endpoint', async () => {
    vi.mocked(api.get).mockResolvedValueOnce({
      data: {
        success: true,
        data: { items: [{ status: 'direct_downloaded' }] },
      },
    })
    const { signal } = new AbortController()
    await expect(getUsageExports(signal)).resolves.toEqual([
      { status: 'direct_downloaded' },
    ])
    expect(api.get).toHaveBeenCalledWith('/api/usage/v2/exports', {
      params: { include_direct: true },
      signal,
      disableDuplicate: true,
    })
  })

  it('downloads historical jobs through the legacy endpoint', async () => {
    vi.mocked(api.get).mockResolvedValueOnce({ data: new Blob() })
    await downloadUsageExport('old-job', '')
    expect(api.get).toHaveBeenCalledWith(
      '/api/usage/exports/old-job/download',
      expect.any(Object)
    )
  })

  it('downloads payer-specific jobs through the versioned endpoint', async () => {
    vi.mocked(api.get).mockResolvedValueOnce({ data: new Blob() })
    await downloadUsageExport('new-job', 'organization')
    expect(api.get).toHaveBeenCalledWith(
      '/api/usage/v2/exports/new-job/download',
      expect.any(Object)
    )
  })
})
