export const PERSONAL_PAYER_VALUE = 'personal'

export type PayerOption = {
  value: string
  label: string
  disabled?: boolean
}

type PayerOptionsInput = {
  personalLabel: string
  unavailableLabel: string
  membershipOrganization: { id: number; name: string } | null
  currentOrganizationId: number | null
}

/**
 * Options for the "Paid by" select. Personal billing is always offered. The user's active
 * organization is offered only when they are a member. A Key still bound to an organization
 * the user no longer serves keeps a disabled entry, so the stored value is never shown as
 * if it were personal.
 */
export function getPayerOptions(input: PayerOptionsInput): PayerOption[] {
  const options: PayerOption[] = [
    { value: PERSONAL_PAYER_VALUE, label: input.personalLabel },
  ]
  const member = input.membershipOrganization
  if (member) {
    options.push({ value: String(member.id), label: member.name })
  }
  const current = input.currentOrganizationId
  if (current !== null && current !== member?.id) {
    options.push({
      value: String(current),
      label: input.unavailableLabel,
      disabled: true,
    })
  }
  return options
}

export function payerValueToOrganizationId(value: string): number | null {
  return value === PERSONAL_PAYER_VALUE ? null : Number(value)
}

export function organizationIdToPayerValue(id: number | null): string {
  return id === null ? PERSONAL_PAYER_VALUE : String(id)
}
