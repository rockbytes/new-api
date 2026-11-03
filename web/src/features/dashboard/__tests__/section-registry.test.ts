import { describe, expect, it } from 'vitest'

import {
  DASHBOARD_ADMIN_ONLY_SECTION_IDS,
  DASHBOARD_SECTION_IDS,
} from '../section-registry'

describe('dashboard section registry', () => {
  it('no longer offers the traffic flow section, which moved to platform operations', () => {
    expect(DASHBOARD_SECTION_IDS).not.toContain('flow')
  })

  it('limits the model and user sections to administrators', () => {
    expect(DASHBOARD_ADMIN_ONLY_SECTION_IDS.has('models')).toBe(true)
    expect(DASHBOARD_ADMIN_ONLY_SECTION_IDS.has('users')).toBe(true)
  })

  it('keeps the personal overview available to every user', () => {
    expect(DASHBOARD_ADMIN_ONLY_SECTION_IDS.has('overview')).toBe(false)
  })
})
