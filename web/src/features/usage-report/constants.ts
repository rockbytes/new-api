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
import type { UsageRankBy, UsageView } from './types'

// Labels are translation keys, so the page translates them where it renders.
export const USAGE_VIEW_LABEL_KEYS: Record<UsageView, string> = {
  platform: 'Platform view',
  organization: 'Organization view',
  self: 'Personal view',
}

export const USAGE_RANK_BY_LABEL_KEYS: Record<UsageRankBy, string> = {
  user: 'Ranking by member',
  organization: 'Ranking by organization',
}

export const USAGE_PERSONAL_BILLING_KEY =
  'Personal billing (not in an organization)'
