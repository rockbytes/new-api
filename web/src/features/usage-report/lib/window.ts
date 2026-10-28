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

// Date.getTimezoneOffset is positive west of UTC, while the API counts east of UTC.
export function localTzOffsetMinutes(date: Date): number {
  return -date.getTimezoneOffset()
}
