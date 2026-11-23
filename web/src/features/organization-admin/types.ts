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
  // Platform administrators also choose the user group and organization role.
  group?: string
  role?: OrganizationRole
}

export type { OrganizationQuota }
