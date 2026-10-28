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
