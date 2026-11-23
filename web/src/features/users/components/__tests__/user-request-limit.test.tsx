import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AxiosResponse } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { UserRequestLimitSection } from '../user-request-limit-section'

const USER_ID = 12
const LIMIT_PATH = `/api/user/${USER_ID}/rate_limit_override`

function response(body: unknown): AxiosResponse {
  return { data: body } as AxiosResponse
}

// The server reports the stored override, the global default and the limits that apply.
function mockLimit(totalCount: number, successCount: number) {
  return vi.spyOn(api, 'get').mockResolvedValue(
    response({
      success: true,
      data: {
        user_id: USER_ID,
        total_count: totalCount,
        success_count: successCount,
        default_total: 100,
        default_success: 20,
        effective_total: totalCount > 0 ? totalCount : 100,
        effective_success: successCount > 0 ? successCount : 20,
      },
    })
  )
}

function renderSection(canManage: boolean) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <UserRequestLimitSection userId={USER_ID} canManage={canManage} />
    </QueryClientProvider>
  )
}

// The edit button stays disabled until the limits have loaded.
async function openEditDialog(user: ReturnType<typeof userEvent.setup>) {
  const editButton = screen.getByRole('button', { name: 'Edit request limits' })
  await waitFor(() => expect(editButton).toBeEnabled())
  await user.click(editButton)
}

beforeEach(() => {
  vi.spyOn(api, 'put').mockResolvedValue(
    response({ success: true, data: null })
  )
  vi.spyOn(api, 'delete').mockResolvedValue(
    response({ success: true, data: null })
  )
})

afterEach(() => {
  vi.restoreAllMocks()
})

it('disables editing for a user the signed-in administrator may not manage', async () => {
  mockLimit(0, 0)
  renderSection(false)

  const editButton = await screen.findByRole('button', {
    name: 'Edit request limits',
  })
  expect(editButton).toBeDisabled()
})

it('refuses to save when neither limit has a value and sends nothing', async () => {
  mockLimit(0, 0)
  const user = userEvent.setup()
  renderSection(true)
  await openEditDialog(user)

  await user.click(screen.getByRole('button', { name: 'Save' }))

  const totalInput = await screen.findByRole('spinbutton', {
    name: 'Maximum requests per window',
  })
  await waitFor(() =>
    expect(totalInput).toHaveAttribute('aria-invalid', 'true')
  )
  expect(api.put).not.toHaveBeenCalled()
})

it('saves a total limit and sends the empty successful count as 0', async () => {
  mockLimit(0, 0)
  const user = userEvent.setup()
  renderSection(true)
  await openEditDialog(user)

  await user.type(
    await screen.findByRole('spinbutton', {
      name: 'Maximum requests per window',
    }),
    '5'
  )
  await user.click(screen.getByRole('button', { name: 'Save' }))

  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith(LIMIT_PATH, {
      total_count: 5,
      success_count: 0,
    })
  )
})

it('removes the limits of a user that has an override', async () => {
  mockLimit(5, 0)
  const user = userEvent.setup()
  renderSection(true)
  await openEditDialog(user)

  await user.click(await screen.findByRole('button', { name: 'Remove limits' }))

  await waitFor(() => expect(api.delete).toHaveBeenCalledWith(LIMIT_PATH))
})
