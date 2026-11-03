import { createFileRoute, redirect } from '@tanstack/react-router'

import { RequestPolicies } from '@/features/system-settings/request-policies'
import { POLICY_SECTION_IDS } from '@/features/system-settings/request-policies/section-registry'

export const Route = createFileRoute(
  '/_authenticated/system-settings/request-policies/$section'
)({
  beforeLoad: ({ params }) => {
    if (!(POLICY_SECTION_IDS as readonly string[]).includes(params.section)) {
      throw redirect({
        to: '/system-settings/request-policies/$section',
        params: { section: 'routing' },
        replace: true,
      })
    }
  },
  component: RequestPolicies,
})
