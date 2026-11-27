import { isAxiosError } from 'axios'

import { api } from '@/lib/api'
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'

import { usageExportFileName } from './lib/export'
import type {
  UsageCompanyRecord,
  UsageExportJob,
  UsageExportRequest,
  UsageMeta,
  UsageModelRow,
  UsagePayerType,
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

// The models a report can be filtered by. The list ignores the model filter itself.
export interface UsageModelOptionsResponse {
  meta: UsageMeta
  models: string[]
}

async function getUsage<T>(path: string, params: UsageQueryParams): Promise<T> {
  const res = await api.get(`/api/usage/v2/${path}`, { params })
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

export function getUsageModelOptions(params: UsageQueryParams) {
  return getUsage<UsageModelOptionsResponse>('model-options', params)
}

// Exports are generated on the server. A small export is ready in the create response; a large
// one is queued and the caller polls the list until it succeeds, fails, or expires.
export async function createUsageExport(
  request: UsageExportRequest
): Promise<UsageExportJob> {
  const res = await api.post('/api/usage/v2/exports', request)
  return requireServerSuccess(res.data).data
}

export async function getUsageExports(
  signal?: AbortSignal
): Promise<UsageExportJob[]> {
  const res = await api.get('/api/usage/v2/exports', {
    params: { include_direct: true },
    signal,
    // Invalidating after a download must not reuse an earlier, in-flight list request.
    disableDuplicate: true,
  })
  return requireServerSuccess(res.data).data.items
}

// The file is fetched with the session token, so the browser cannot open it as a plain link.
// Failures are reported by the caller, which needs the HTTP status to tell an expired file apart.
export async function downloadUsageExport(
  jobId: string,
  payerType?: UsagePayerType | ''
): Promise<Blob> {
  const version = payerType ? '/v2' : ''
  const res = await api.get<Blob>(
    `/api/usage${version}/exports/${jobId}/download`,
    {
      responseType: 'blob',
      skipErrorHandler: true,
      skipBusinessError: true,
    }
  )
  await rejectInvalidExportBlob(res.data)
  return res.data
}

async function rejectInvalidExportBlob(blob: Blob): Promise<void> {
  if (!blob.type.includes('json') && !blob.type.includes('html')) return
  const body = await blob.text()
  try {
    const response: unknown = JSON.parse(body)
    throw createServerError(response, 'The export could not be downloaded')
  } catch (error) {
    if (!(error instanceof SyntaxError)) throw error
    throw new Error('The export could not be downloaded')
  }
}

// How an export is delivered. A direct export is streamed to the browser, a queued one becomes a
// job that stays in the list.
export interface UsageExportEstimate {
  delivery: 'direct' | 'queued'
  row_count: number
}

export async function getUsageExportEstimate(
  request: UsageExportRequest
): Promise<UsageExportEstimate> {
  const res = await api.get('/api/usage/v2/exports/estimate', {
    params: request,
  })
  return requireServerSuccess(res.data).data
}

// A failed download answers with JSON inside a blob. Read it back so the error code is visible.
async function readBlobErrorBody(error: unknown): Promise<void> {
  if (!isAxiosError(error)) return
  const response = error.response
  const body = response?.data
  if (!response || !(body instanceof Blob)) return
  try {
    response.data = JSON.parse(await body.text())
  } catch {
    // Not JSON, so the blob stays as the body.
  }
}

export async function downloadUsageExportDirect(
  request: UsageExportRequest
): Promise<{ blob: Blob; fileName: string }> {
  try {
    const res = await api.get<Blob>('/api/usage/v2/exports/direct', {
      params: request,
      responseType: 'blob',
      skipErrorHandler: true,
      skipBusinessError: true,
    })
    await rejectInvalidExportBlob(res.data)
    return {
      blob: res.data,
      fileName: usageExportFileName(res.headers['content-disposition']),
    }
  } catch (error) {
    await readBlobErrorBody(error)
    throw error
  }
}
