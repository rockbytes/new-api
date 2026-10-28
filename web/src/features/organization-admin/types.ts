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
import type {
  Organization,
  OrganizationQuota,
} from '@/features/organizations/types'

export type OrganizationRole = 'admin' | 'member'

export interface OrganizationSelf {
  organization: Organization
  role: OrganizationRole
  status: string
  permissions: string[]
}

export interface OrganizationMemberRow {
  user_id: number
  username: string
  display_name: string
  role: OrganizationRole
  status: 'active' | 'disabled'
  created_time: number
  allocated_quota: number
  used_quota: number
}

export interface CreateOrganizationMemberPayload {
  username: string
  password: string
}

export type { OrganizationQuota }
