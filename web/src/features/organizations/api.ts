import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  OrganizationQuota,
  CreateOrganizationPayload,
  Organization,
  OrganizationDetail,
  OrganizationPage,
  OrganizationDeletionImpact,
  OrganizationQuotaAdjustPayload,
  OrganizationQuotaAdjustment,
  OrganizationStatus,
} from './types'

export async function getOrganizations(
  page: number,
  pageSize: number
): Promise<OrganizationPage> {
  const res = await api.get('/api/organization/', {
    params: { p: page, page_size: pageSize },
  })
  return requireServerSuccess(res.data).data
}

export async function getOrganizationDetail(
  id: number
): Promise<OrganizationDetail> {
  const res = await api.get(`/api/organization/${id}`)
  return requireServerSuccess(res.data).data
}

export async function createOrganization(
  payload: CreateOrganizationPayload
): Promise<Organization> {
  const res = await api.post('/api/organization/', payload)
  return requireServerSuccess(res.data).data
}

export async function updateOrganizationName(
  id: number,
  name: string
): Promise<void> {
  const res = await api.put(`/api/organization/${id}`, { name })
  requireServerSuccess(res.data)
}

export async function updateOrganizationStatus(
  id: number,
  status: OrganizationStatus
): Promise<void> {
  const res = await api.put(`/api/organization/${id}/status`, { status })
  requireServerSuccess(res.data)
}

export async function getOrganizationQuota(
  id: number
): Promise<OrganizationQuota> {
  const res = await api.get(`/api/organization/${id}/quota`)
  return requireServerSuccess(res.data).data
}

export async function adjustOrganizationQuota(
  id: number,
  payload: OrganizationQuotaAdjustPayload
): Promise<OrganizationQuotaAdjustment> {
  const res = await api.post(`/api/organization/${id}/quota/adjust`, payload)
  return requireServerSuccess(res.data).data
}

export async function setOrganizationMemberAllocation(
  id: number,
  userId: number,
  allocated: number
): Promise<void> {
  const res = await api.put(`/api/organization/${id}/members/${userId}/quota`, {
    allocated,
  })
  requireServerSuccess(res.data)
}

export async function getOrganizationDeletionImpact(
  id: number
): Promise<OrganizationDeletionImpact> {
  const res = await api.get(`/api/organization/${id}/deletion-impact`)
  return requireServerSuccess(res.data).data
}

export async function deleteOrganization(id: number): Promise<void> {
  const res = await api.delete(`/api/organization/${id}`)
  requireServerSuccess(res.data)
}
