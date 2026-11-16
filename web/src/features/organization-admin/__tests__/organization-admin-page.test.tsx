import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError, type AxiosResponse } from 'axios'
import i18next from 'i18next'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { OrganizationAdmin } from '..'

function response(body: unknown): AxiosResponse {
  return { data: body } as AxiosResponse
}

function mockOrganizationApi(role: 'admin' | 'member') {
  return vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url === '/api/organization/self') {
      return response({
        success: true,
        data: {
          organization: {
            id: 7,
            name: 'Acme',
            status: 'active',
            created_time: 1700000000,
            updated_time: 1700000000,
          },
          role,
          status: 'active',
          permissions: [],
        },
      })
    }
    if (url === '/api/organization/self/quota') {
      return response({
        success: true,
        data: {
          granted_quota: 10_000_000,
          consumed_quota: 2_000_000,
          remaining_quota: 8_000_000,
          member_used: 2_000_000,
          allocated: 10_000_000,
          unallocated: 0,
          balanced: true,
        },
      })
    }
    if (url === '/api/organization/self/members') {
      return response({
        success: true,
        data: [
          {
            user_id: 2,
            username: 'bob',
            display_name: 'Bob',
            role: 'member',
            status: 'active',
            created_time: 1700000000,
            allocated_quota: 0,
            used_quota: 0,
          },
          {
            user_id: 3,
            username: 'carol',
            display_name: 'Carol',
            role: 'member',
            status: 'active',
            created_time: 1700000000,
            allocated_quota: 0,
            used_quota: 0,
          },
        ],
      })
    }
    throw new Error(`unexpected GET ${url}`)
  })
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <OrganizationAdmin />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'alice',
    role: 1,
    permissions: {},
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

it('hides member management and never loads members for a member account', async () => {
  const get = mockOrganizationApi('member')

  renderPage()

  expect(
    await screen.findByText('Organization admins only')
  ).toBeInTheDocument()
  expect(get).not.toHaveBeenCalledWith(
    '/api/organization/self/members',
    expect.anything()
  )
  expect(
    screen.queryByRole('button', { name: 'Create member account' })
  ).not.toBeInTheDocument()
})

it('shows the not-in-organization state when the self lookup returns 404', async () => {
  vi.spyOn(api, 'get').mockRejectedValue(
    new AxiosError('Request failed', 'ERR_BAD_REQUEST', undefined, undefined, {
      status: 404,
    } as AxiosResponse)
  )

  renderPage()

  expect(
    await screen.findByText('You are not in an organization')
  ).toBeInTheDocument()
})

it('lists the organization members and offers member actions for an admin account', async () => {
  mockOrganizationApi('admin')

  renderPage()

  expect(await screen.findByText('bob')).toBeInTheDocument()
  expect(screen.getByText('Acme')).toBeInTheDocument()
  expect(
    screen.getByRole('button', { name: 'Create member account' })
  ).toBeInTheDocument()
})

it('filters the member list by username without case sensitivity as the admin types', async () => {
  mockOrganizationApi('admin')
  const user = userEvent.setup()

  renderPage()

  expect(await screen.findByText('bob')).toBeInTheDocument()
  expect(screen.getByText('carol')).toBeInTheDocument()

  await user.type(
    screen.getByRole('searchbox', { name: 'Search by username' }),
    'CA'
  )

  expect(screen.getByText('carol')).toBeInTheDocument()
  expect(screen.queryByText('bob')).not.toBeInTheDocument()
})

it('shows member status labels in the current language', async () => {
  i18next.addResourceBundle('en', 'translation', { Active: '生效' }, true, true)
  try {
    mockOrganizationApi('admin')

    renderPage()

    expect(await screen.findAllByText('生效')).toHaveLength(2)
  } finally {
    i18next.removeResourceBundle('en', 'translation')
  }
})

it('adds to a member allocation through the adjust quota dialog', async () => {
  mockOrganizationApi('admin')
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue(response({ success: true }))
  const user = userEvent.setup()

  renderPage()

  const bobRow = within(await screen.findByRole('row', { name: /bob/ }))
  await user.click(bobRow.getByRole('button', { name: 'Member actions' }))
  await user.click(
    await screen.findByRole('menuitem', { name: 'Allocate quota' })
  )
  const dialog = await screen.findByRole('dialog', { name: 'Adjust Quota' })
  await user.type(within(dialog).getByRole('spinbutton'), '1')
  await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))

  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/organization/self/members/2/quota', {
      allocated: expect.any(Number),
    })
  )
  const body = put.mock.calls[0]?.[1] as { allocated: number }
  expect(body.allocated).toBeGreaterThan(0)
})

it('offers quota allocation on the admin own row but not on another admin row', async () => {
  mockOrganizationApi('admin')
  const membersOnly = vi.mocked(api.get).getMockImplementation()
  vi.mocked(api.get).mockImplementation(async (url: string, config) => {
    if (url !== '/api/organization/self/members') return membersOnly?.(url, config)
    return response({
      success: true,
      data: [
        {
          user_id: 1,
          username: 'alice',
          display_name: 'Alice',
          role: 'admin',
          status: 'active',
          created_time: 1700000000,
          allocated_quota: 0,
          used_quota: 0,
        },
        {
          user_id: 4,
          username: 'dave',
          display_name: 'Dave',
          role: 'admin',
          status: 'active',
          created_time: 1700000000,
          allocated_quota: 0,
          used_quota: 0,
        },
      ],
    })
  })
  const user = userEvent.setup()

  renderPage()

  const aliceRow = within(await screen.findByRole('row', { name: /alice/ }))
  await user.click(aliceRow.getByRole('button', { name: 'Member actions' }))
  expect(
    await screen.findByRole('menuitem', { name: 'Allocate quota' })
  ).toBeInTheDocument()
  await user.keyboard('{Escape}')

  const daveRow = within(await screen.findByRole('row', { name: /dave/ }))
  await user.click(daveRow.getByRole('button', { name: 'Member actions' }))
  await screen.findByRole('menuitem', { name: 'Make member' })
  expect(
    screen.queryByRole('menuitem', { name: 'Allocate quota' })
  ).not.toBeInTheDocument()
})

// An allocation is a limit, so the allocatable figure and the spendable figure differ: the
// first is what is still free to hand out, the second is what the organization can still spend.
it('shows allocatable, spendable and used quota as separate figures for an admin', async () => {
  mockOrganizationApi('admin')

  renderPage()

  await screen.findByText('bob')

  // Read each figure from its own card, so the assertions do not depend on how the amounts
  // happen to collide with the member rows.
  // "Used quota" is also a member table column header, so scope the lookup to the cards only.
  const allocatable = screen
    .getByText('Allocatable quota')
    .closest('[data-slot="card"]') as HTMLElement
  const grid = allocatable.parentElement as HTMLElement
  const spendable = within(grid)
    .getByText('Spendable quota')
    .closest('[data-slot="card"]') as HTMLElement
  const used = within(grid)
    .getByText('Used quota')
    .closest('[data-slot="card"]') as HTMLElement
  // The organization is fully allocated (unallocated 0) yet still has quota it can spend.
  expect(within(allocatable as HTMLElement).getByText('$0')).toBeInTheDocument()
  expect(within(spendable as HTMLElement).getByText('$16')).toBeInTheDocument()
  expect(within(used as HTMLElement).getByText('$4')).toBeInTheDocument()
})

// An override hides the target behind a single number, which reads as a balance even though a
// member allocation is a limit, so the member allocation dialog does not offer it.
it('does not offer the override mode when adjusting a member allocation', async () => {
  mockOrganizationApi('admin')
  const user = userEvent.setup()

  renderPage()

  const bobRow = within(await screen.findByRole('row', { name: /bob/ }))
  await user.click(bobRow.getByRole('button', { name: 'Member actions' }))
  await user.click(
    await screen.findByRole('menuitem', { name: 'Allocate quota' })
  )

  const dialog = await screen.findByRole('dialog', { name: 'Adjust Quota' })
  expect(within(dialog).getByRole('button', { name: 'Add' })).toBeInTheDocument()
  expect(
    within(dialog).getByRole('button', { name: 'Subtract' })
  ).toBeInTheDocument()
  expect(
    within(dialog).queryByRole('button', { name: 'Override' })
  ).not.toBeInTheDocument()
})
