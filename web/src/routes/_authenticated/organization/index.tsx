import { createFileRoute } from '@tanstack/react-router'

import { OrganizationAdmin } from '@/features/organization-admin'

export const Route = createFileRoute('/_authenticated/organization/')({
  component: OrganizationAdmin,
})
