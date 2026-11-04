import { describe, expect, it } from 'vitest'

import {
  getPayerOptions,
  organizationIdToPayerValue,
  payerValueToOrganizationId,
} from '../billing-organization'

const labels = {
  personalLabel: 'Personal',
  unavailableLabel: 'Unavailable organization',
}

describe('getPayerOptions', () => {
  it('offers only personal billing when the user belongs to no organization', () => {
    const options = getPayerOptions({
      ...labels,
      membershipOrganization: null,
      currentOrganizationId: null,
    })

    expect(options).toEqual([{ value: 'personal', label: 'Personal' }])
  })

  it('adds the active organization the user is a member of, next to personal billing', () => {
    const options = getPayerOptions({
      ...labels,
      membershipOrganization: { id: 7, name: 'Shanghai Co' },
      currentOrganizationId: null,
    })

    expect(options.map((option) => option.value)).toEqual(['personal', '7'])
    expect(options[1]).toEqual({ value: '7', label: 'Shanghai Co' })
  })

  it('keeps a disabled entry for a key still bound to an organization the user no longer serves', () => {
    const options = getPayerOptions({
      ...labels,
      membershipOrganization: null,
      currentOrganizationId: 9,
    })

    expect(options).toContainEqual({
      value: '9',
      label: 'Unavailable organization',
      disabled: true,
    })
    expect(
      options.find((option) => option.value === 'personal')?.disabled
    ).toBeUndefined()
  })

  it('does not duplicate the organization when the bound organization is the current membership', () => {
    const options = getPayerOptions({
      ...labels,
      membershipOrganization: { id: 7, name: 'Shanghai Co' },
      currentOrganizationId: 7,
    })

    expect(options.filter((option) => option.value === '7')).toHaveLength(1)
    expect(options.every((option) => !option.disabled)).toBe(true)
  })
})

describe('payer value conversion', () => {
  it('maps the personal option to null and an organization option to its id', () => {
    expect(payerValueToOrganizationId('personal')).toBeNull()
    expect(payerValueToOrganizationId('7')).toBe(7)
    expect(organizationIdToPayerValue(null)).toBe('personal')
    expect(organizationIdToPayerValue(7)).toBe('7')
  })
})
