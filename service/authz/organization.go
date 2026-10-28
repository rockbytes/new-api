package authz

import (
	"slices"

	"github.com/QuantumNous/new-api/model"
)

// Organization-scoped actions. They are checked against the caller's organization
// membership role, not against Casbin policies, so platform-level overrides cannot
// widen them and organization roles cannot widen platform permissions.
const (
	OrganizationPermissionView          = "organization.view"
	OrganizationPermissionMembersList   = "organization.members.list"
	OrganizationPermissionMembersCreate = "organization.members.create"
	OrganizationPermissionMembersUpdate = "organization.members.update"
	OrganizationPermissionMembersRemove = "organization.members.remove"
	OrganizationPermissionQuotaUpdate   = "organization.quota.update"
)

var organizationRolePermissions = map[string][]string{
	model.OrganizationRoleMember: {
		OrganizationPermissionView,
	},
	model.OrganizationRoleAdmin: {
		OrganizationPermissionView,
		OrganizationPermissionMembersList,
		OrganizationPermissionMembersCreate,
		OrganizationPermissionMembersUpdate,
		OrganizationPermissionMembersRemove,
		OrganizationPermissionQuotaUpdate,
	},
}

// OrganizationRoleAllows reports whether an organization role grants a
// permission. Unknown roles grant nothing.
func OrganizationRoleAllows(role string, permission string) bool {
	return slices.Contains(organizationRolePermissions[role], permission)
}

// OrganizationRolePermissions lists the organization permissions a role grants.
func OrganizationRolePermissions(role string) []string {
	return organizationRolePermissions[role]
}
