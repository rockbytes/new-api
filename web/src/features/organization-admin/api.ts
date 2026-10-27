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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  CreateOrganizationMemberPayload,
  OrganizationMemberRow,
  OrganizationRole,
  OrganizationSelf,
} from './types'

export async function getOrganizationSelf(): Promise<OrganizationSelf> {
  const res = await api.get('/api/organization/self')
  return requireServerSuccess(res.data).data
}

export async function getOrganizationMembers(): Promise<
  OrganizationMemberRow[]
> {
  const res = await api.get('/api/organization/self/members')
  return requireServerSuccess(res.data).data
}

export async function createOrganizationMember(
  payload: CreateOrganizationMemberPayload
): Promise<void> {
  const res = await api.post('/api/organization/self/members', payload)
  requireServerSuccess(res.data)
}

export async function updateOrganizationMember(
  userId: number,
  changes: { role?: OrganizationRole; status?: 'active' | 'disabled' }
): Promise<void> {
  const res = await api.put(`/api/organization/self/members/${userId}`, changes)
  requireServerSuccess(res.data)
}

export async function resetOrganizationMemberPassword(
  userId: number,
  password: string
): Promise<void> {
  const res = await api.post(
    `/api/organization/self/members/${userId}/password`,
    { password }
  )
  requireServerSuccess(res.data)
}

export async function deleteOrganizationMember(userId: number): Promise<void> {
  const res = await api.delete(`/api/organization/self/members/${userId}`)
  requireServerSuccess(res.data)
}
