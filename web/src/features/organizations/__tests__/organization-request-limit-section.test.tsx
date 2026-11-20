import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AxiosResponse } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { OrganizationRequestLimitSection } from '../components/organization-request-limit-section'

const ORGANIZATION_ID = 7

function response(body: unknown): AxiosResponse {
  return { data: body } as AxiosResponse
}

function renderSection() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <OrganizationRequestLimitSection organizationId={ORGANIZATION_ID} />
    </QueryClientProvider>
  )
}

function mockOverrides(totalCount: number | null) {
  const rows =
    totalCount === null
      ? []
      : [
          {
            id: 1,
            subject_type: 'organization',
            subject_id: ORGANIZATION_ID,
            group_name: '',
            total_count: totalCount,
            updated_by: 1,
            updated_time: 1700000000,
          },
        ]
  return vi.spyOn(api, 'get').mockResolvedValue(
    response({ success: true, data: rows })
  )
}

beforeEach(() => {
  vi.spyOn(api, 'put').mockResolvedValue(
    response({ success: true, data: { total_count: 500 } })
  )
  vi.spyOn(api, 'delete').mockResolvedValue(
    response({ success: true, data: null })
  )
})

afterEach(() => {
  vi.restoreAllMocks()
})

it('shows the organization limit that is currently set', async () => {
  mockOverrides(300)

  renderSection()

  expect(
    await screen.findByText('Current limit: 300 requests per window.')
  ).toBeInTheDocument()
})

it('saves a new limit for the organization and reports success', async () => {
  mockOverrides(300)
  const put = vi.mocked(api.put)
  const user = userEvent.setup()
  renderSection()
  // Wait for the saved limit to load, so the form is not reset under the typing.
  await screen.findByText('Current limit: 300 requests per window.')

  const input = await screen.findByRole('spinbutton', {
    name: 'Maximum requests per window',
  })
  await user.clear(input)
  await user.type(input, '500')
  await user.click(screen.getByRole('button', { name: 'Save' }))

  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/rate-limit-overrides/', {
      subject_type: 'organization',
      subject_id: ORGANIZATION_ID,
      total_count: 500,
    })
  )
})

it('refuses a limit outside the allowed range without calling the server', async () => {
  mockOverrides(null)
  const put = vi.mocked(api.put)
  const user = userEvent.setup()
  renderSection()
  await screen.findByText('No request limit is set for this organization.')

  const input = await screen.findByRole('spinbutton', {
    name: 'Maximum requests per window',
  })
  await user.clear(input)
  await user.type(input, '0')
  await user.click(screen.getByRole('button', { name: 'Save' }))

  expect(
    await screen.findByText('Enter a number between 1 and 100000000.')
  ).toBeInTheDocument()
  expect(put).not.toHaveBeenCalled()
})

it('removes an existing limit and leaves the remove action unavailable when none is set', async () => {
  mockOverrides(300)
  const remove = vi.mocked(api.delete)
  const user = userEvent.setup()
  renderSection()

  await user.click(await screen.findByRole('button', { name: 'Remove limit' }))

  await waitFor(() =>
    expect(remove).toHaveBeenCalledWith(
      `/api/rate-limit-overrides/organization/${ORGANIZATION_ID}`
    )
  )
})

it('disables removing the limit when the organization has none', async () => {
  mockOverrides(null)

  renderSection()

  expect(
    await screen.findByText('No request limit is set for this organization.')
  ).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Remove limit' })).toBeDisabled()
})
