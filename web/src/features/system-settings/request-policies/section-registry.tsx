import { createSectionRegistry } from '../utils/section-registry'
import { ChannelHealthSection } from './channel-health-section'
import type { RequestPolicySettings } from './defaults'
import { RequestChecksSection } from './request-checks-section'
import { RoutingPolicySection } from './routing-section'

const POLICY_SECTIONS = [
  {
    id: 'filtering',
    titleKey: 'Request checks',
    build: (settings: RequestPolicySettings) => (
      <RequestChecksSection
        defaultValues={{
          CheckSensitiveEnabled: settings.CheckSensitiveEnabled,
          CheckSensitiveOnPromptEnabled: settings.CheckSensitiveOnPromptEnabled,
          SensitiveWords: settings.SensitiveWords,
        }}
      />
    ),
  },
  {
    id: 'routing',
    titleKey: 'Sessions and retries',
    build: () => <RoutingPolicySection />,
  },
  {
    id: 'health',
    titleKey: 'Channel health',
    build: (settings: RequestPolicySettings) => (
      <ChannelHealthSection defaultValues={settings} />
    ),
  },
] as const

export type PolicySectionId = (typeof POLICY_SECTIONS)[number]['id']
const registry = createSectionRegistry<PolicySectionId, RequestPolicySettings>({
  sections: POLICY_SECTIONS,
  defaultSection: 'routing',
  basePath: '/system-settings/request-policies',
  urlStyle: 'path',
})
export const POLICY_SECTION_IDS = registry.sectionIds
export const getPolicySectionNavItems = registry.getSectionNavItems
export const getPolicySectionContent = registry.getSectionContent
export const getPolicySectionMeta = registry.getSectionMeta
