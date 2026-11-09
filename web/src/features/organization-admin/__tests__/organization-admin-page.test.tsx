import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError, type AxiosResponse } from 'axios'
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
          },
          {
            user_id: 3,
            username: 'carol',
            display_name: 'Carol',
            role: 'member',
            status: 'active',
            created_time: 1700000000,
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
