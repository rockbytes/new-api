import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'

import { QuotaRow } from '../components/quota-row'

it('keeps the secondary figure on its own line under the amount for long and short numbers', () => {
  const { container } = render(
    <>
      <QuotaRow
        label='Available'
        value='99,999,999.95'
        secondaryLabel='Used amount'
        secondaryValue='100'
      />
      <QuotaRow
        label='Available'
        value='0'
        secondaryLabel='Used amount'
        secondaryValue='0'
      />
    </>
  )

  const rows = container.querySelectorAll<HTMLElement>('span.grid')
  expect(rows).toHaveLength(2)
  for (const row of rows) {
    // The secondary figure keeps its own line, and the two figures are told apart by their
    // words rather than by a size or weight difference.
    const [amountLine, secondaryLine] = row.querySelectorAll<HTMLElement>(
      ':scope > span'
    )
    expect(amountLine).not.toHaveClass('font-normal')
    expect(secondaryLine).not.toHaveClass('font-normal')
    expect(secondaryLine).not.toHaveAttribute('data-table-text')
  }
  expect(screen.getByText('99,999,999.95')).toHaveClass('break-all')
})

it('renders a badge without a secondary line when the row has no amount', () => {
  render(<QuotaRow label='Personal' badge={<span>No Quota</span>} />)

  expect(screen.getByText('No Quota')).toBeInTheDocument()
  expect(screen.queryByText('Used amount')).not.toBeInTheDocument()
})
