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
import { render, screen } from '@testing-library/react'
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
