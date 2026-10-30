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
import type { UsageExportFormat, UsageExportStatus, UsageRankBy } from './types'

export const USAGE_RANK_BY_LABEL_KEYS: Record<UsageRankBy, string> = {
  user: 'Ranking by member',
  organization: 'Ranking by organization',
}

export const USAGE_PERSONAL_BILLING_KEY =
  'Personal billing (not in an organization)'

export interface UsageExportOption {
  key: 'records' | 'summary' | 'workbook'
  labelKey: string
  format: UsageExportFormat
  kind?: 'records' | 'summary'
}

// The three exports on the usage page. The workbook holds every table, so it takes no kind.
export const USAGE_EXPORT_OPTIONS: UsageExportOption[] = [
  {
    key: 'records',
    labelKey: 'Usage records (CSV)',
    format: 'csv',
    kind: 'records',
  },
  {
    key: 'summary',
    labelKey: 'Usage summary (CSV)',
    format: 'csv',
    kind: 'summary',
  },
  { key: 'workbook', labelKey: 'Usage workbook (XLSX)', format: 'xlsx' },
]

// A job's status is shown as a badge. The variant follows StatusBadge's color names.
export const USAGE_EXPORT_STATUS_BADGES: Record<
  UsageExportStatus,
  { labelKey: string; variant: 'neutral' | 'info' | 'success' | 'danger' }
> = {
  queued: { labelKey: 'Queued', variant: 'neutral' },
  running: { labelKey: 'Generating', variant: 'info' },
  succeeded: { labelKey: 'Ready', variant: 'success' },
  failed: { labelKey: 'Failed', variant: 'danger' },
  expired: { labelKey: 'Expired', variant: 'neutral' },
}
