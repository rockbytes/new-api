import { createSectionRegistry } from '@/features/system-settings/utils/section-registry'

/**
 * Dashboard page section definitions
 */
const DASHBOARD_SECTIONS = [
  {
    id: 'overview',
    titleKey: 'Overview',
    build: () => null,
  },
  {
    id: 'models',
    titleKey: 'Model Call Analytics',
    adminOnly: true,
    build: () => null,
  },
  {
    id: 'users',
    titleKey: 'User Analytics',
    adminOnly: true,
    build: () => null,
  },
] as const

export type DashboardSectionId = (typeof DASHBOARD_SECTIONS)[number]['id']

// Ordinary users and organization administrators see the usage report instead of these.
export const DASHBOARD_ADMIN_ONLY_SECTION_IDS: ReadonlySet<string> =
  new Set<string>(['models', 'users'])

const dashboardRegistry = createSectionRegistry<
  DashboardSectionId,
  Record<string, never>,
  []
>({
  sections: DASHBOARD_SECTIONS,
  defaultSection: 'overview',
  basePath: '/dashboard',
  urlStyle: 'path',
})

export const DASHBOARD_SECTION_IDS = dashboardRegistry.sectionIds
export const DASHBOARD_DEFAULT_SECTION = dashboardRegistry.defaultSection
