import { render, screen, within } from '@testing-library/react'
import { expect, it } from 'vitest'

import { UserOrganizationQuotaCell } from '../components/user-organization-quota-cell'

it('labels the organization row and its allocated amount as separate from the personal balance', () => {
  render(<UserOrganizationQuotaCell allocated={5000} used={300} />)

  const trigger = screen.getByRole('button', {
    name: /^Organization allocation Remaining/,
  })
  expect(within(trigger).getByText('Organization')).toBeInTheDocument()
  expect(within(trigger).getByText('Allocated quota')).toBeInTheDocument()
})
