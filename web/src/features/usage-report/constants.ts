import type { UsageExportStatus, UsageRankBy } from './types'

export const USAGE_RANK_BY_LABEL_KEYS: Record<UsageRankBy, string> = {
  user: 'Ranking by member',
  organization: 'Ranking by organization',
}

export const USAGE_PERSONAL_BILLING_KEY =
  'Personal billing (not in an organization)'

// The Trend, Models, and Ranking lists each show this many rows before an expand toggle appears.
export const USAGE_DEFAULT_VISIBLE_ROWS = 10

// Historical job labels remain available for exports created before summary-only exports.
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
