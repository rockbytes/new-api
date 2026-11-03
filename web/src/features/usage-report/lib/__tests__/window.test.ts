import { describe, expect, test } from 'vitest'

import {
  buildUsageWindow,
  localTzOffsetMinutes,
} from '@/features/usage-report/lib/window'

describe('buildUsageWindow', () => {
  test('covers whole days back from now, ending now', () => {
    const window = buildUsageWindow(7, 1_000_000, 480)

    expect(window).toEqual({
      start_timestamp: 1_000_000 - 7 * 86400,
      end_timestamp: 1_000_000,
      tz_offset_minutes: 480,
    })
  })

  test('keeps a negative offset for zones west of UTC', () => {
    expect(buildUsageWindow(30, 1_000_000, -300).tz_offset_minutes).toBe(-300)
  })
})

describe('localTzOffsetMinutes', () => {
  test('reports the offset east of UTC, the opposite sign of getTimezoneOffset', () => {
    const fakeDate = { getTimezoneOffset: () => -480 } as Date

    expect(localTzOffsetMinutes(fakeDate)).toBe(480)
  })
})
