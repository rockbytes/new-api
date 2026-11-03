import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  UsageCompanyRecord,
  UsageExportJob,
  UsageExportRequest,
  UsageMeta,
  UsageModelRow,
  UsagePlatformRecord,
  UsageQueryParams,
  UsageRankBy,
  UsageRankRow,
  UsageSelfRecord,
  UsageSummary,
  UsageTrendPoint,
} from './types'

// Every usage endpoint answers with the same meta block. The caller's identity decides the
// scope on the server; these parameters only narrow it.
export interface UsageSummaryResponse {
  meta: UsageMeta
  summary: UsageSummary
}

export interface UsageTrendResponse {
  meta: UsageMeta
  points: UsageTrendPoint[]
}

export interface UsageModelsResponse {
  meta: UsageMeta
  models: UsageModelRow[]
}

export interface UsageRankingsResponse {
  meta: UsageMeta
  rank_by: UsageRankBy | ''
  rows: UsageRankRow[]
}

export type UsageRecordItem =
  | UsageCompanyRecord
  | UsagePlatformRecord
  | UsageSelfRecord

export interface UsageRecordsResponse {
  meta: UsageMeta
  total: number
  page: number
  page_size: number
  items: UsageRecordItem[]
}

async function getUsage<T>(path: string, params: UsageQueryParams): Promise<T> {
  const res = await api.get(`/api/usage/${path}`, { params })
  return requireServerSuccess(res.data).data
}

export function getUsageSummary(params: UsageQueryParams) {
  return getUsage<UsageSummaryResponse>('summary', params)
}

export function getUsageTrend(params: UsageQueryParams) {
  return getUsage<UsageTrendResponse>('trend', params)
}

export function getUsageModels(params: UsageQueryParams) {
  return getUsage<UsageModelsResponse>('models', params)
}

export function getUsageRankings(params: UsageQueryParams) {
  return getUsage<UsageRankingsResponse>('rankings', params)
}

export function getUsageRecords(params: UsageQueryParams) {
  return getUsage<UsageRecordsResponse>('records', params)
}

// Exports are generated on the server. A small export is ready in the create response; a large
// one is queued and the caller polls the list until it succeeds, fails, or expires.
export async function createUsageExport(
  request: UsageExportRequest
): Promise<UsageExportJob> {
  const res = await api.post('/api/usage/exports', request)
  return requireServerSuccess(res.data).data
}

export async function getUsageExports(): Promise<UsageExportJob[]> {
  const res = await api.get('/api/usage/exports')
  return requireServerSuccess(res.data).data.items
}

// The file is fetched with the session token, so the browser cannot open it as a plain link.
// Failures are reported by the caller, which needs the HTTP status to tell an expired file apart.
export async function downloadUsageExport(jobId: string): Promise<Blob> {
  const res = await api.get<Blob>(`/api/usage/exports/${jobId}/download`, {
    responseType: 'blob',
    skipErrorHandler: true,
    skipBusinessError: true,
  })
  return res.data
}
