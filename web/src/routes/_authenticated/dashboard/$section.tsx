import { createFileRoute, redirect } from '@tanstack/react-router'

import { Dashboard } from '@/features/dashboard'
import {
  DASHBOARD_SECTION_IDS,
  DASHBOARD_DEFAULT_SECTION,
  DASHBOARD_ADMIN_ONLY_SECTION_IDS,
} from '@/features/dashboard/section-registry'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/dashboard/$section')({
  beforeLoad: ({ params }) => {
    const { auth } = useAuthStore.getState()
    const isAdmin = Boolean(auth.user && auth.user.role >= ROLE.ADMIN)
    // The traffic flow moved to the platform operations page.
    if (params.section === 'flow') {
      throw redirect({ to: isAdmin ? '/platform-ops' : '/usage-report' })
    }
    const validSections = DASHBOARD_SECTION_IDS as unknown as string[]
    if (!validSections.includes(params.section)) {
      throw redirect({
        to: '/dashboard/$section',
        params: { section: DASHBOARD_DEFAULT_SECTION },
      })
    }
    if (DASHBOARD_ADMIN_ONLY_SECTION_IDS.has(params.section) && !isAdmin) {
      throw redirect({ to: '/usage-report' })
    }
  },
  component: Dashboard,
})
