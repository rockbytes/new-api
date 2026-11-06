import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { Organizations } from '..'
import {
  createOrganizationMemberForAdmin,
  getOrganizationDetail,
  getOrganizations,
} from '../api'
import type { OrganizationMember, OrganizationSummary } from '../types'

vi.mock('../api', () => ({
  adjustOrganizationQuota: vi.fn(),
  createOrganizationMemberForAdmin: vi.fn(),
  createOrganization: vi.fn(),
  deleteOrganization: vi.fn(),
  getOrganizationDeletionImpact: vi.fn(),
  getOrganizationDetail: vi.fn(),
  getOrganizations: vi.fn(),
  setOrganizationMemberAllocation: vi.fn(),
  updateOrganizationName: vi.fn(),
  updateOrganizationStatus: vi.fn(),
}))

vi.mock('@/features/users/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/users/api')>()),
  searchUsers: vi.fn(),
}))

const mockedList = vi.mocked(getOrganizations)
const mockedDetail = vi.mocked(getOrganizationDetail)
const mockedCreateMember = vi.mocked(createOrganizationMemberForAdmin)

const organization: OrganizationSummary = {
  id: 1,
  name: 'Acme Labs',
  status: 'active',
  created_time: 1_700_000_000,
  updated_time: 1_700_000_000,
  member_count: 2,
  granted_quota: 0,
  allocated_quota: 0,
  unallocated_quota: 0,
}

function member(username: string): OrganizationMember {
  return {
    user_id: username.length,
    username,
    display_name: username,
    role: 'member',
    status: 'active',
    created_time: 1_700_000_000,
    allocated_quota: 0,
    used_quota: 0,
  }
}

function renderOrganizations() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Organizations />
    </QueryClientProvider>
  )
}

async function openDetail() {
  mockedList.mockResolvedValue({
    page: 1,
    page_size: 100,
    total: 1,
    items: [organization],
  })
  mockedDetail.mockResolvedValue({
    organization: organization as never,
    members: [member('alice'), member('bob')],
  })
  const user = userEvent.setup()
  renderOrganizations()
  await user.click(
    within(await screen.findByRole('row', { name: /Acme Labs/ })).getByRole(
      'button',
      { name: 'Edit' }
    )
  )
  return user
}

afterEach(() => {
  vi.clearAllMocks()
})

describe('organization members', () => {
  it('filters the member list by username as the administrator types', async () => {
    const user = await openDetail()

    const search = await screen.findByRole('searchbox', {
      name: 'Search by username',
    })
    await screen.findByText('alice')
    expect(screen.getByText('bob')).toBeInTheDocument()

    await user.type(search, 'ali')
    expect(screen.getByText('alice')).toBeInTheDocument()
    expect(screen.queryByText('bob')).not.toBeInTheDocument()
  })

  it('creates a member account in the organization being viewed', async () => {
    const user = await openDetail()
    mockedCreateMember.mockResolvedValue(undefined)

    await user.click(await screen.findByRole('button', { name: 'Add member' }))
    const dialog = await screen.findByRole('dialog', {
      name: 'Create member account',
    })
    await user.type(within(dialog).getByLabelText('Username'), 'carol')
    await user.type(
      within(dialog).getByLabelText('Initial password'),
      'long-enough-pass'
    )
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))

    await waitFor(() =>
      expect(mockedCreateMember).toHaveBeenCalledWith(1, {
        username: 'carol',
        password: 'long-enough-pass',
      })
    )
  })

  it('names the per-member action Allocate quota', async () => {
    await openDetail()

    expect(
      (await screen.findAllByRole('button', { name: 'Allocate quota' })).length
    ).toBe(2)
    expect(
      screen.queryByRole('button', { name: 'Set allocation' })
    ).not.toBeInTheDocument()
  })

  it('shows each member status with its translated label', async () => {
    i18next.addResourceBundle(
      'en',
      'translation',
      { Active: '生效' },
      true,
      true
    )
    try {
      await openDetail()
      const sheet = await screen.findByRole('dialog')
      expect(await within(sheet).findAllByText('生效')).toHaveLength(2)
    } finally {
      i18next.removeResourceBundle('en', 'translation')
    }
  })
})
