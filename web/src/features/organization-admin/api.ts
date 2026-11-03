import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  OrganizationQuota,
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

export async function getOwnOrganizationQuota(): Promise<OrganizationQuota> {
  const res = await api.get('/api/organization/self/quota')
  return requireServerSuccess(res.data).data
}

export async function setOwnMemberAllocation(
  userId: number,
  allocated: number
): Promise<void> {
  const res = await api.put(`/api/organization/self/members/${userId}/quota`, {
    allocated,
  })
  requireServerSuccess(res.data)
}
