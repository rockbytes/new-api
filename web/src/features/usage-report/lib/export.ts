import type { UsageExportOption } from '../constants'
import type {
  UsageExportJob,
  UsageExportRequest,
  UsageQueryParams,
} from '../types'

// An export covers the same scope and window as the report on screen. The server authorizes
// the scope again, so these fields are only the caller's intent.
export function buildUsageExportRequest(
  params: UsageQueryParams,
  option: UsageExportOption
): UsageExportRequest {
  const request: UsageExportRequest = {
    view: params.view,
    organization_id: params.organization_id,
    user_id: params.user_id,
    model_name: params.model_name,
    start_timestamp: params.start_timestamp,
    end_timestamp: params.end_timestamp,
    tz_offset_minutes: params.tz_offset_minutes,
    format: option.format,
  }
  // The byte-order mark helps spreadsheet apps read UTF-8 CSV. A workbook needs no kind or BOM.
  if (option.format === 'csv') {
    request.kind = option.kind
    request.with_bom = true
  }
  return request
}

// Polling continues only while a job can still change state.
export function hasActiveUsageExports(jobs: UsageExportJob[]): boolean {
  return jobs.some((job) => job.status === 'queued' || job.status === 'running')
}

export function saveUsageExportFile(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = fileName
  anchor.click()
  URL.revokeObjectURL(url)
}

// A direct export names its file in Content-Disposition. The name is a fallback for a response
// that lacks the header, so the saved file still has a neutral name.
export function usageExportFileName(disposition: string | undefined): string {
  const match = /filename="([^"]+)"/.exec(disposition ?? '')
  return match ? match[1] : 'usage-export'
}
