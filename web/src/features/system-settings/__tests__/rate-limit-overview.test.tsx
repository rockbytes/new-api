import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { getRateLimitOverview } from '../request-limits/rate-limit-overview-api'
import { RateLimitOverviewSection } from '../request-limits/rate-limit-overview'

vi.mock('../request-limits/rate-limit-overview-api', () => ({
  getRateLimitOverview: vi.fn(),
}))

const mockedOverview = vi.mocked(getRateLimitOverview)

function renderSection() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RateLimitOverviewSection />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.clearAllMocks()
})

describe('rate limit overview', () => {
  test('shows the enabled organization layer with its limit and window', async () => {
    mockedOverview.mockResolvedValue({
      subject_type: 'organization',
      subject_id: 5,
      layers: [
        {
          layer: 'organization',
          enabled: true,
          limit: 7,
          window_seconds: 120,
        },
      ],
    })
    const user = userEvent.setup()
    renderSection()

    await user.type(screen.getByRole('spinbutton', { name: 'Subject ID' }), '5')

    const table = await screen.findByRole('table')
    expect(
      within(table).getByText('Organization total requests')
    ).toBeInTheDocument()
    expect(within(table).getByText('7')).toBeInTheDocument()
    expect(within(table).getByText('120')).toBeInTheDocument()
    expect(mockedOverview).toHaveBeenCalledWith('organization', 5)
  })

  test('does not query until a positive subject ID is entered', async () => {
    const user = userEvent.setup()
    renderSection()

    await user.type(screen.getByRole('spinbutton', { name: 'Subject ID' }), '0')

    expect(
      screen.getByText('Enter a subject ID to see its limits.')
    ).toBeInTheDocument()
    expect(mockedOverview).not.toHaveBeenCalled()
  })
})
