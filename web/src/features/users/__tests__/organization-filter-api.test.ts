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
