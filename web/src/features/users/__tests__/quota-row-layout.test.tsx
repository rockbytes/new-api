import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'

import { QuotaRow } from '../components/quota-row'

it('keeps the secondary figure on its own line under the amount for long and short numbers', () => {
  const { container } = render(
    <>
      <QuotaRow
        label='Organization'
        value='99,999,999.95'
        secondaryLabel='Allocated quota'
        secondaryValue='100'
      />
      <QuotaRow
        label='Organization'
        value='0'
        secondaryLabel='Allocated quota'
        secondaryValue='0'
      />
    </>
  )

  const rows = container.querySelectorAll<HTMLElement>('span.grid')
  expect(rows).toHaveLength(2)
  for (const row of rows) {
    const secondary = row.querySelector<HTMLElement>(
      '[data-table-text="secondary"]'
    )
    expect(secondary).toHaveClass('col-start-2')
  }
  expect(screen.getByText('99,999,999.95')).toHaveClass('break-all')
})

it('renders a badge without a secondary line when the row has no amount', () => {
  render(<QuotaRow label='Personal' badge={<span>No Quota</span>} />)

  expect(screen.getByText('No Quota')).toBeInTheDocument()
  expect(document.querySelector('[data-table-text="secondary"]')).toBeNull()
})
