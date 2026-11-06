import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { UsageSummaryCards } from '../components/usage-summary-cards'

describe('UsageSummaryCards amounts', () => {
  it('shows consumption as a currency amount and requests as a plain count', () => {
    render(
      <UsageSummaryCards
        locale='en-US'
        summary={{
          consume_quota: 500000,
          requests: 1234,
          prompt_tokens: 0,
          completion_tokens: 0,
        }}
      />
    )

    expect(screen.getByText(/^\$1/)).toBeInTheDocument()
    expect(screen.queryByText('500,000')).not.toBeInTheDocument()
    expect(screen.getByText('1,234')).toBeInTheDocument()
  })
})
