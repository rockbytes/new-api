import { createFileRoute } from '@tanstack/react-router'

import { UsageReport } from '@/features/usage-report'

export const Route = createFileRoute('/_authenticated/usage-report/')({
  component: UsageReport,
})
