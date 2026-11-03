import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { searchUsers } from '../api'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn() },
}))

const mockedGet = vi.mocked(api.get)

describe('searchUsers organization filter', () => {
  beforeEach(() => {
    mockedGet.mockResolvedValue({ data: { success: true, data: {} } })
  })

  it('sends the organization ID when one is selected', async () => {
    await searchUsers({ keyword: '', organization_id: '12' })

    const url = String(mockedGet.mock.calls[0][0])
    expect(
      new URL(url, 'http://localhost').searchParams.get('organization_id')
    ).toBe('12')
  })

  it('sends 0 to select users without an organization', async () => {
    await searchUsers({ keyword: '', organization_id: '0' })

    const url = String(mockedGet.mock.calls[0][0])
    expect(
      new URL(url, 'http://localhost').searchParams.get('organization_id')
    ).toBe('0')
  })

  it('omits the parameter when no organization filter is set', async () => {
    await searchUsers({ keyword: '' })

    const url = String(mockedGet.mock.calls[0][0])
    expect(
      new URL(url, 'http://localhost').searchParams.has('organization_id')
    ).toBe(false)
  })
})
