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
