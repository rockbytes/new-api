import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { UsageTrendBars } from '../components/usage-trend-bars'
import type { UsageTrendPoint } from '../types'

function points(count: number): UsageTrendPoint[] {
  return Array.from({ length: count }, (_, index) => ({
    day_start: index,
    consume_quota: index,
    requests: index,
  }))
}

describe('UsageTrendBars day limit', () => {
  it('shows only the first 10 days and no expand toggle at exactly 10 days', () => {
    render(<UsageTrendBars points={points(10)} locale='en-US' />)

    expect(screen.getAllByRole('listitem')).toHaveLength(10)
    expect(
      screen.queryByRole('button', { name: /Show/ })
    ).not.toBeInTheDocument()
  })

  it('reveals the remaining days after the expand toggle is clicked', async () => {
    render(<UsageTrendBars points={points(23)} locale='en-US' />)

    expect(screen.getAllByRole('listitem')).toHaveLength(10)
    await userEvent.click(screen.getByRole('button', { name: 'Show 13 more' }))

    expect(screen.getAllByRole('listitem')).toHaveLength(23)
    expect(
      screen.getByRole('button', { name: 'Show less' })
    ).toBeInTheDocument()
  })

  it('still reports no usage when the range has no days', () => {
    render(<UsageTrendBars points={[]} locale='en-US' />)

    expect(screen.getByText('No usage in this range')).toBeInTheDocument()
  })
})
