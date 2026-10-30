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

import { deleteOrganization, getOrganizationDeletionImpact } from '../api'
import { OrganizationDeleteDialog } from '../components/organization-delete-dialog'
import type { OrganizationDeletionImpact } from '../types'

vi.mock('../api', () => ({
  getOrganizationDeletionImpact: vi.fn(),
  deleteOrganization: vi.fn(),
}))

const mockedImpact = vi.mocked(getOrganizationDeletionImpact)
const mockedDelete = vi.mocked(deleteOrganization)

function impact(
  overrides: Partial<OrganizationDeletionImpact>
): OrganizationDeletionImpact {
  return {
    organization_id: 7,
    status: 'suspended',
    member_count: 0,
    admin_count: 1,
    allocated_quota: 0,
    used_quota: 0,
    unallocated_quota: 0,
    enabled_paid_keys: 0,
    ledger_entries: 2,
    consume_logs: 3,
    tasks: 1,
    blockers: [],
    ...overrides,
  }
}

function renderDialog(onDeleted = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <OrganizationDeleteDialog
        organizationId={7}
        open
        onOpenChange={vi.fn()}
        onDeleted={onDeleted}
      />
    </QueryClientProvider>
  )
  return { onDeleted }
}

afterEach(() => {
  vi.clearAllMocks()
})

describe('OrganizationDeleteDialog', () => {
  it('lists the remaining blockers and keeps deletion disabled while any remain', async () => {
    mockedImpact.mockResolvedValue(
      impact({
        status: 'active',
        blockers: ['status_not_suspended', 'members_remaining'],
        member_count: 2,
      })
    )

    renderDialog()

    expect(
      await screen.findByText('Suspend the organization before deleting it.')
    ).toBeVisible()
    expect(screen.getByText('Delete the remaining members.')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Delete organization' })
    ).toBeDisabled()
    expect(mockedDelete).not.toHaveBeenCalled()
  })

  it('deletes the organization once the impact has no blockers', async () => {
    mockedImpact.mockResolvedValue(impact({ blockers: [] }))
    mockedDelete.mockResolvedValue(undefined)
    const user = userEvent.setup()

    const { onDeleted } = renderDialog()

    const confirm = await screen.findByRole('button', {
      name: 'Delete organization',
    })
    await waitFor(() => expect(confirm).toBeEnabled())
    await user.click(confirm)

    await waitFor(() => expect(mockedDelete).toHaveBeenCalledWith(7))
    await waitFor(() => expect(onDeleted).toHaveBeenCalled())
  })
})
