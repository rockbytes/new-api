import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  FlowQuotaDataItem,
  OverviewAccountStatus,
  QuotaDataItem,
  UptimeGroupResult,
} from './types'

// ============================================================================
// Dashboard APIs
// ============================================================================

// ----------------------------------------------------------------------------
// Quota & Usage Data
// ----------------------------------------------------------------------------

export async function getOverviewAccountStatus(signal: AbortSignal) {
  const res = await api.get<{ success: boolean; data: OverviewAccountStatus }>(
    '/api/data/self/account-status',
    // React Query cancels superseded requests. Do not share a canceled request
    // with its replacement through the Axios deduplication layer.
    { signal, disableDuplicate: true }
  )
  return requireServerSuccess(res.data).data
}

// Get user quota data within a time range
// Admin users get all users' data by default.
export async function getUserQuotaDates(
  params: {
    start_timestamp: number
    end_timestamp: number
    default_time?: string
    username?: string
  },
  isAdmin = false
) {
  const endpoint = isAdmin ? '/api/data' : '/api/data/self'
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    endpoint,
    { params }
  )
  return res.data
}

// ----------------------------------------------------------------------------
// System Monitoring
// ----------------------------------------------------------------------------

export async function getUserQuotaDataByUsers(params: {
  start_timestamp: number
  end_timestamp: number
}) {
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    '/api/data/users',
    { params }
  )
  return res.data
}

// Flow data is an administrator view. Ordinary users and organization administrators use
// the usage report instead.
export async function getFlowQuotaDates(params: {
  start_timestamp: number
  end_timestamp: number
  default_time?: string
  username?: string
}) {
  const res = await api.get<{
    success: boolean
    data?: FlowQuotaDataItem[]
    message?: string
  }>('/api/data/flow', { params })
  return res.data
}

// Get uptime monitoring status for all services
export async function getUptimeStatus() {
  const res = await api.get<{ success: boolean; data: UptimeGroupResult[] }>(
    '/api/uptime/status'
  )
  return res.data
}
