export type UsageView = 'platform' | 'organization' | 'self'

interface UsageScopeInfo {
  view: UsageView
  organization: { id: number; name: string } | null
  user_id: number
}

export interface UsageMeta {
  scope: UsageScopeInfo
  window: {
    start_timestamp: number
    end_timestamp: number
    tz_offset_minutes: number
  }
  basis: 'settled_consumption'
  unit: 'platform_quota'
}

export interface UsageSummary {
  consume_quota: number
  requests: number
  prompt_tokens: number
  completion_tokens: number
}

export interface UsageTrendPoint {
  day_start: number
  consume_quota: number
  requests: number
}

export interface UsageModelRow {
  model_name: string
  consume_quota: number
  requests: number
  prompt_tokens: number
  completion_tokens: number
}

export type UsageRankBy = 'user' | 'organization'

export interface UsageRankRow {
  key: number
  name: string
  consume_quota: number
  requests: number
}

export interface UsageCompanyRecord {
  id: number
  created_at: number
  type: number
  username: string
  token_name: string
  model_name: string
  quota: number
  prompt_tokens: number
  completion_tokens: number
  use_time: number
  is_stream: boolean
  request_id: string
}

export interface UsagePlatformRecord extends UsageCompanyRecord {
  user_id: number
  token_id: number
  group: string
  channel_id: number
  ip: string
  billing_organization_id: number
}

export interface UsageSelfRecord extends UsageCompanyRecord {
  billing_organization_id: number
  billing_organization_name: string
}


export interface UsageQueryParams {
  view?: UsageView
  organization_id?: number
  user_id?: number
  start_timestamp: number
  end_timestamp: number
  tz_offset_minutes: number
  page?: number
  page_size?: number
}

type UsageExportKind = 'records' | 'summary' | 'workbook'

export type UsageExportFormat = 'csv' | 'xlsx'

export type UsageExportStatus =
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'expired'

// The request carries the scope and window of the report on screen. The export never
// includes paging fields, because it covers the whole range.
export interface UsageExportRequest {
  view?: UsageView
  organization_id?: number
  user_id?: number
  start_timestamp: number
  end_timestamp: number
  tz_offset_minutes: number
  format: UsageExportFormat
  kind?: Exclude<UsageExportKind, 'workbook'>
  with_bom?: boolean
}

export interface UsageExportJob {
  job_id: string
  status: UsageExportStatus
  view: UsageView
  organization_id: number
  target_user_id: number
  start_timestamp: number
  end_timestamp: number
  tz_offset_minutes: number
  kind: UsageExportKind
  format: UsageExportFormat
  with_bom: boolean
  row_count: number
  file_name: string
  error_code: string
  created_time: number
  started_time: number
  finished_time: number
  expires_time: number
  download_url?: string
}
