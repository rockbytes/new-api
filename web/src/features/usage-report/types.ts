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
export type UsageView = 'platform' | 'organization' | 'self'

export interface UsageScopeInfo {
  view: UsageView
  organization: { id: number; name: string } | null
  user_id: number
}

export interface UsageMeta {
  scope: UsageScopeInfo
  window: {
    start_timestamp: number
    end_timestamp: number
    tz_offset_minutes: number
  }
  basis: 'settled_net'
  unit: 'platform_quota'
}

export interface UsageSummary {
  net_quota: number
  consume_quota: number
  refund_quota: number
  requests: number
  prompt_tokens: number
  completion_tokens: number
}

export interface UsageTrendPoint {
  day_start: number
  net_quota: number
  requests: number
}

export interface UsageModelRow {
  model_name: string
  net_quota: number
  requests: number
  prompt_tokens: number
  completion_tokens: number
}

export type UsageRankBy = 'user' | 'organization'

export interface UsageRankRow {
  key: number
  name: string
  net_quota: number
  requests: number
}

export interface UsageCompanyRecord {
  id: number
  created_at: number
  type: number
  username: string
  token_name: string
  model_name: string
  quota: number
  prompt_tokens: number
  completion_tokens: number
  use_time: number
  is_stream: boolean
  request_id: string
}

export interface UsagePlatformRecord extends UsageCompanyRecord {
  user_id: number
  token_id: number
  group: string
  channel_id: number
  ip: string
  billing_organization_id: number
}

export interface UsageSelfRecord extends UsageCompanyRecord {
  billing_organization_id: number
  billing_organization_name: string
}

export interface UsageRecordPage<TItem> {
  total: number
  page: number
  page_size: number
  items: TItem[]
}

export interface UsageQueryParams {
  view?: UsageView
  organization_id?: number
  user_id?: number
  start_timestamp: number
  end_timestamp: number
  tz_offset_minutes: number
  page?: number
  page_size?: number
}
