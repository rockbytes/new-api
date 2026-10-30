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
import { afterEach, describe, expect, it, vi } from 'vitest'

import { searchUsers } from '@/features/users/api'

import { createOrganization } from '../api'
import { CreateOrganizationSheet } from '../components/create-organization-sheet'

vi.mock('../api', () => ({
  createOrganization: vi.fn(),
}))

vi.mock('@/features/users/api', () => ({
  searchUsers: vi.fn(),
}))

const mockedCreate = vi.mocked(createOrganization)
const mockedSearch = vi.mocked(searchUsers)

function renderSheet() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <CreateOrganizationSheet open onOpenChange={vi.fn()} />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.clearAllMocks()
})

describe('CreateOrganizationSheet first admin', () => {
  it('creates the first admin account when a new account is chosen', async () => {
    mockedCreate.mockResolvedValue({} as never)
    const user = userEvent.setup()
    renderSheet()

    await user.type(screen.getByLabelText('Organization name'), 'Acme')
    await user.type(screen.getByLabelText('First admin username'), 'acme-admin')
    await user.type(
      screen.getByLabelText('Initial password'),
      'long-enough-pass'
    )
    await user.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(mockedCreate).toHaveBeenCalledTimes(1))
    expect(mockedCreate).toHaveBeenCalledWith({
      name: 'Acme',
      admin_username: 'acme-admin',
      admin_password: 'long-enough-pass',
    })
  })

  it('uses an existing eligible user as the first admin without sending a new account', async () => {
    mockedCreate.mockResolvedValue({} as never)
    mockedSearch.mockResolvedValue({
      success: true,
      data: {
        items: [{ id: 5, username: 'alice', display_name: 'Alice' }],
        total: 1,
        page: 1,
        page_size: 20,
      },
    } as never)
    const user = userEvent.setup()
    renderSheet()

    await user.click(
      screen.getByRole('button', { name: 'Use an existing user' })
    )
    await user.type(screen.getByLabelText('Organization name'), 'Acme')
    const candidate = await screen.findByRole('button', { name: /alice/ })
    await user.click(candidate)
    await user.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(mockedCreate).toHaveBeenCalledTimes(1))
    expect(mockedCreate).toHaveBeenCalledWith({
      name: 'Acme',
      admin_user_id: 5,
    })
    expect(mockedSearch).toHaveBeenCalledWith(
      expect.objectContaining({ role: '1', organization_id: '0', status: '1' })
    )
  })

  it('asks for a selected user before creating in existing-user mode', async () => {
    mockedSearch.mockResolvedValue({
      success: true,
      data: { items: [], total: 0, page: 1, page_size: 20 },
    } as never)
    const user = userEvent.setup()
    renderSheet()

    await user.click(
      screen.getByRole('button', { name: 'Use an existing user' })
    )
    await user.type(screen.getByLabelText('Organization name'), 'Acme')
    await user.click(screen.getByRole('button', { name: 'Create' }))

    expect(
      await screen.findByText('Select an existing user as the first admin.')
    ).toBeVisible()
    expect(mockedCreate).not.toHaveBeenCalled()
  })
})
