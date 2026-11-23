import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export type RateLimitSubjectType = 'organization' | 'user' | 'channel'

export type RateLimitOverviewLayer = {
  layer: 'organization' | 'user_total' | 'user_success' | 'channel'
  enabled: boolean
  limit: number
  window_seconds: number
}

export type RateLimitOverview = {
  subject_type: RateLimitSubjectType
  subject_id: number
  layers: RateLimitOverviewLayer[]
}

// Read-only: the overview writes nothing, so the request has no body.
export async function getRateLimitOverview(
  subjectType: RateLimitSubjectType,
  subjectId: number
): Promise<RateLimitOverview> {
  const res = await api.get('/api/rate-limit-overview/', {
    params: { subject_type: subjectType, subject_id: subjectId },
  })
  return requireServerSuccess(res.data).data
}
