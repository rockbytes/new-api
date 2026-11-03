export const USAGE_RANGE_DAYS = [7, 30, 90] as const

export type UsageRangeDays = (typeof USAGE_RANGE_DAYS)[number]

const SECONDS_PER_DAY = 86400

export interface UsageWindow {
  start_timestamp: number
  end_timestamp: number
  tz_offset_minutes: number
}

// The window ends now and covers whole days back from it. The offset is the caller's local
// offset in minutes, so the server buckets trend points by the caller's local day.
export function buildUsageWindow(
  days: UsageRangeDays,
  now: number,
  tzOffsetMinutes: number
): UsageWindow {
  return {
    start_timestamp: now - days * SECONDS_PER_DAY,
    end_timestamp: now,
    tz_offset_minutes: tzOffsetMinutes,
  }
}

// A window from an explicit start and end chosen by the caller, bounded to whole seconds.
// It is used only when the end is after the start.
export function buildCustomUsageWindow(
  start: Date,
  end: Date,
  tzOffsetMinutes: number
): UsageWindow {
  return {
    start_timestamp: Math.floor(start.getTime() / 1000),
    end_timestamp: Math.floor(end.getTime() / 1000),
    tz_offset_minutes: tzOffsetMinutes,
  }
}

// Date.getTimezoneOffset is positive west of UTC, while the API counts east of UTC.
export function localTzOffsetMinutes(date: Date): number {
  return -date.getTimezoneOffset()
}
