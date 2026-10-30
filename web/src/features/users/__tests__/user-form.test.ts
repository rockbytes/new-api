/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
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
