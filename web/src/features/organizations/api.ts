import type { CreateOrganizationMemberPayload } from '@/features/organization-admin/types'
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
  RateLimitOverride,
} from './types'

export async function getOrganizations(
  page: number,
  pageSize: number,
  keyword?: string
): Promise<OrganizationPage> {
  const res = await api.get('/api/organization/', {
    params: { p: page, page_size: pageSize, keyword },
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

// A platform administrator adds a member account to the organization they are viewing.
export async function createOrganizationMemberForAdmin(
  id: number,
  payload: CreateOrganizationMemberPayload
): Promise<void> {
  const res = await api.post(`/api/organization/${id}/members`, payload)
  requireServerSuccess(res.data)
}

export async function getOrganizationRequestLimits(): Promise<
  RateLimitOverride[]
> {
  const res = await api.get('/api/rate-limit-overrides/', {
    params: { subject_type: 'organization' },
  })
  return requireServerSuccess(res.data).data ?? []
}

export async function setOrganizationRequestLimit(
  organizationId: number,
  totalCount: number
): Promise<RateLimitOverride> {
  const res = await api.put('/api/rate-limit-overrides/', {
    subject_type: 'organization',
    subject_id: organizationId,
    total_count: totalCount,
  })
  return requireServerSuccess(res.data).data
}

export async function clearOrganizationRequestLimit(
  organizationId: number
): Promise<void> {
  const res = await api.delete(
    `/api/rate-limit-overrides/organization/${organizationId}`
  )
  requireServerSuccess(res.data)
}
