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

import { buildCustomUsageWindow, buildUsageWindow } from '../lib/window'

describe('usage report window', () => {
  it('covers whole days back from now for a day range', () => {
    const now = 1_000_000
    expect(buildUsageWindow(7, now, 480)).toEqual({
      start_timestamp: now - 7 * 86400,
      end_timestamp: now,
      tz_offset_minutes: 480,
    })
  })

  it('uses the picked start and end as whole seconds', () => {
    const start = new Date('2026-10-01T00:00:00.900Z')
    const end = new Date('2026-10-05T12:30:00.400Z')
    expect(buildCustomUsageWindow(start, end, 480)).toEqual({
      start_timestamp: Math.floor(start.getTime() / 1000),
      end_timestamp: Math.floor(end.getTime() / 1000),
      tz_offset_minutes: 480,
    })
  })
})
