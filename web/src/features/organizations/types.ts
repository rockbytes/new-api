export type OrganizationStatus = 'active' | 'suspended'

export type OrganizationMemberRole = 'admin' | 'member'

export type OrganizationMemberStatus = 'active' | 'disabled'

export interface Organization {
  id: number
  name: string
  status: OrganizationStatus
  created_time: number
  updated_time: number
}

export interface OrganizationMember {
  user_id: number
  username: string
  display_name: string
  role: OrganizationMemberRole
  status: OrganizationMemberStatus
  created_time: number
  allocated_quota: number
  used_quota: number
}

export interface OrganizationPage {
  page: number
  page_size: number
  total: number
  items: Organization[]
}

export interface OrganizationDetail {
  organization: Organization
  members: OrganizationMember[]
}

export interface CreateOrganizationPayload {
  name: string
  // Either a new first admin account, or the ID of an existing eligible user.
  admin_username?: string
  admin_password?: string
  admin_user_id?: number
}

export interface OrganizationQuota {
  granted_quota: number
  consumed_quota: number
  remaining_quota: number
  member_used: number
  allocated: number
  unallocated: number
  balanced: boolean
}

export interface OrganizationQuotaAdjustPayload {
  mode: 'add' | 'subtract' | 'override'
  amount: number
  reason: string
  reference: string
}

export interface OrganizationQuotaAdjustment {
  applied: boolean
  unallocated: number
}

// Counts and unmet preconditions for deleting an organization. Blockers are codes that
// the dialog maps to labels; an empty list means the organization can be deleted.
export interface OrganizationDeletionImpact {
  organization_id: number
  status: string
  member_count: number
  admin_count: number
  allocated_quota: number
  used_quota: number
  unallocated_quota: number
  enabled_paid_keys: number
  ledger_entries: number
  consume_logs: number
  tasks: number
  blockers: string[]
}
