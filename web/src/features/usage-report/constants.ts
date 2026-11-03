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
