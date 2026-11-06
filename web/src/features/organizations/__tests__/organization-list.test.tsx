import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { Organizations } from '..'
import {
  adjustOrganizationQuota,
  getOrganizationDetail,
  getOrganizations,
  updateOrganizationStatus,
} from '../api'
import type { OrganizationSummary } from '../types'

vi.mock('../api', () => ({
  adjustOrganizationQuota: vi.fn(),
  createOrganizationMemberForAdmin: vi.fn(),
  createOrganization: vi.fn(),
  getOrganizationDeletionImpact: vi.fn(),
  getOrganizationDetail: vi.fn(),
  getOrganizations: vi.fn(),
  updateOrganizationStatus: vi.fn(),
  deleteOrganization: vi.fn(),
}))

vi.mock('@/features/users/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/users/api')>()),
  searchUsers: vi.fn(),
}))

const mockedList = vi.mocked(getOrganizations)
const mockedDetail = vi.mocked(getOrganizationDetail)
const mockedStatus = vi.mocked(updateOrganizationStatus)
const mockedAdjust = vi.mocked(adjustOrganizationQuota)

function organization(
  overrides: Partial<OrganizationSummary>
): OrganizationSummary {
  return {
    id: 1,
    name: 'Acme Labs',
    status: 'active',
    created_time: 1_700_000_000,
    updated_time: 1_700_000_000,
    member_count: 3,
    granted_quota: 1_000_000,
    allocated_quota: 500_000,
    unallocated_quota: 250_000,
    ...overrides,
  }
}

function renderList() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Organizations />
    </QueryClientProvider>
  )
}

function rowFor(name: string) {
  return screen.getByRole('row', { name: new RegExp(name) })
}

afterEach(() => {
  vi.clearAllMocks()
})

describe('organization list', () => {
  it('shows the member count and the quota figures on each row', async () => {
    mockedList.mockResolvedValue({
      page: 1,
      page_size: 100,
      total: 1,
      items: [organization({})],
    })
    mockedDetail.mockResolvedValue({
      organization: organization({}) as never,
      members: [],
    })

    renderList()

    const row = await screen.findByRole('row', { name: /Acme Labs/ })
    expect(within(row).getByText('3')).toBeInTheDocument()
    expect(within(row).getByText('$2')).toBeInTheDocument()
    expect(within(row).getByText('$1')).toBeInTheDocument()
    expect(within(row).getByText('$0.5')).toBeInTheDocument()
  })

  it('opens the quota adjustment for the row and sends the change for that organization', async () => {
    const user = userEvent.setup()
    mockedList.mockResolvedValue({
      page: 1,
      page_size: 100,
      total: 1,
      items: [organization({})],
    })
    mockedAdjust.mockResolvedValue({ applied: true, unallocated: 0 })

    renderList()

    await user.click(
      within(await screen.findByRole('row', { name: /Acme Labs/ })).getByRole(
        'button',
        { name: 'Adjust Quota' }
      )
    )
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
    expect(mockedAdjust).not.toHaveBeenCalled()
  })

  it('asks before suspending an active organization and resumes a suspended one directly', async () => {
    const user = userEvent.setup()
    mockedList.mockResolvedValue({
      page: 1,
      page_size: 100,
      total: 2,
      items: [
        organization({ id: 1, name: 'Acme Labs' }),
        organization({ id: 2, name: 'Paused Co', status: 'suspended' }),
      ],
    })
    mockedStatus.mockResolvedValue({} as never)

    renderList()

    await user.click(
      within(await screen.findByRole('row', { name: /Acme Labs/ })).getByRole(
        'button',
        { name: 'Suspend' }
      )
    )
    const confirm = await screen.findByRole('alertdialog')
    expect(mockedStatus).not.toHaveBeenCalled()
    await user.click(within(confirm).getByRole('button', { name: 'Suspend' }))
    await waitFor(() =>
      expect(mockedStatus).toHaveBeenCalledWith(1, 'suspended')
    )

    await user.click(
      within(rowFor('Paused Co')).getByRole('button', { name: 'Resume' })
    )
    await waitFor(() =>
      expect(mockedStatus).toHaveBeenLastCalledWith(2, 'active')
    )
  })
})
