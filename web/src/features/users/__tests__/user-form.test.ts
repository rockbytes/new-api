import { describe, expect, it } from 'vitest'

import {
  USER_FORM_DEFAULT_VALUES,
  transformFormDataToPayload,
  transformUserToFormDefaults,
} from '../lib/user-form'
import type { User } from '../types'

function userWith(organization: User['organization']): User {
  return {
    id: 7,
    username: 'member',
    display_name: 'member',
    quota: 0,
    used_quota: 0,
    request_count: 0,
    group: 'default',
    status: 1,
    role: 1,
    organization,
  } as unknown as User
}

describe('user organization placement form', () => {
  it('starts with no organization and the member role', () => {
    expect(USER_FORM_DEFAULT_VALUES.organization_id).toBe('0')
    expect(USER_FORM_DEFAULT_VALUES.organization_role).toBe('member')
  })

  it('shows the current organization and role when editing', () => {
    const defaults = transformUserToFormDefaults(
      userWith({ id: 3, name: 'Acme', role: 'admin' })
    )
    expect(defaults.organization_id).toBe('3')
    expect(defaults.organization_role).toBe('admin')
  })

  it('shows no organization for a user outside any organization', () => {
    const defaults = transformUserToFormDefaults(userWith(null))
    expect(defaults.organization_id).toBe('0')
    expect(defaults.organization_role).toBe('member')
  })

  it('sends the organization on create and on update', () => {
    const created = transformFormDataToPayload({
      ...USER_FORM_DEFAULT_VALUES,
      username: 'new-member',
      organization_id: '3',
      organization_role: 'admin',
    })
    expect(created.organization_id).toBe(3)
    expect(created.organization_role).toBe('admin')

    const updated = transformFormDataToPayload(
      { ...USER_FORM_DEFAULT_VALUES, username: 'member', organization_id: '0' },
      7
    )
    expect(updated.organization_id).toBe(0)
  })
})
