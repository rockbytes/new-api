import { render, screen, within } from '@testing-library/react'
import { expect, it } from 'vitest'

import { UserOrganizationQuotaCell } from '../components/user-organization-quota-cell'

it('labels the organization row and its used amount as separate from the personal balance', () => {
  render(
    <UserOrganizationQuotaCell
      allocated={5_000_000}
      used={300_000}
      organizationRemaining={100_000_000}
    />
  )

  const trigger = screen.getByRole('button', {
    name: /^Organization allocation Spendable quota/,
  })
  expect(within(trigger).getByText('Organization')).toBeInTheDocument()
  expect(within(trigger).getByText('Used amount')).toBeInTheDocument()
  // The allocation has $9.40 left and the organization can cover it, so that is the spendable
  // figure rather than the whole allocation.
  expect(within(trigger).getByText('9.4')).toBeInTheDocument()
})

// A member can only spend what the organization can still cover, even when its own allocation
// has more left. Reporting the allocation remainder alone would overstate what is usable.
it('reports the organization remaining as the spendable figure when it is the smaller limit', () => {
  render(
    <UserOrganizationQuotaCell
      allocated={5_000_000}
      used={300_000}
      organizationRemaining={1_000_000}
    />
  )

  const trigger = screen.getByRole('button', {
    name: /^Organization allocation Spendable quota/,
  })
  expect(within(trigger).getByText('2')).toBeInTheDocument()
  expect(within(trigger).queryByText('9.4')).not.toBeInTheDocument()
})
