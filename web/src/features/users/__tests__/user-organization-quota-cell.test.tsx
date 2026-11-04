import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'

import { UserOrganizationQuotaCell } from '../components/user-organization-quota-cell'

it('labels the organization allocation and its allocated amount as separate from the personal balance', () => {
  render(<UserOrganizationQuotaCell allocated={5000} used={300} />)

  expect(screen.getByText('Organization allocation')).toBeTruthy()
  expect(screen.getByText('Allocated quota')).toBeTruthy()
})
