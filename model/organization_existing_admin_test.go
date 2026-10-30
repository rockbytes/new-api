package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createEligibilityTestUser(t *testing.T, username string, role int, status int) int {
	t.Helper()
	user := &User{Username: username, Password: "not-a-real-password", Role: role, Status: status, AffCode: "aff-" + username}
	require.NoError(t, DB.Create(user).Error)
	return user.Id
}

// An existing common user who belongs to no organization can become the first admin of a
// new organization. Anyone else is refused, and a refused attempt creates no organization.
func TestCreateOrganizationWithExistingAdminAcceptsOnlyEligibleUsers(t *testing.T) {
	_, memberId := setupOrganizationQuotaTestDB(t)

	eligible := createEligibilityTestUser(t, "picked-admin", common.RoleCommonUser, common.UserStatusEnabled)
	organization, err := CreateOrganizationWithExistingAdmin("Picked Co", eligible, 1, 2)
	require.NoError(t, err)

	var membership OrganizationMembership
	require.NoError(t, DB.Where("user_id = ?", eligible).First(&membership).Error)
	assert.Equal(t, organization.Id, membership.OrganizationId)
	assert.Equal(t, OrganizationRoleAdmin, membership.Role)
	assert.Equal(t, OrganizationMembershipActive, membership.Status)
	assert.Equal(t, 1, membership.InvitedBy, "the operator is recorded as the inviter")

	var picked User
	require.NoError(t, DB.First(&picked, eligible).Error)
	assert.Equal(t, common.RoleCommonUser, picked.Role, "the platform role is unchanged")
	assert.Equal(t, "not-a-real-password", picked.Password, "the password is left as it was")
	assert.False(t, picked.MustChangePassword, "an existing account is not forced to change its password")

	var before int64
	require.NoError(t, DB.Model(&Organization{}).Count(&before).Error)

	platformAdmin := createEligibilityTestUser(t, "platform-admin", common.RoleAdminUser, common.UserStatusEnabled)
	root := createEligibilityTestUser(t, "root-user", common.RoleRootUser, common.UserStatusEnabled)
	disabled := createEligibilityTestUser(t, "disabled-user", common.RoleCommonUser, common.UserStatusDisabled)
	deleted := createEligibilityTestUser(t, "deleted-user", common.RoleCommonUser, common.UserStatusEnabled)
	require.NoError(t, DB.Delete(&User{}, deleted).Error)

	refused := map[string]int{
		"platform administrator":  platformAdmin,
		"root":                    root,
		"disabled account":        disabled,
		"deleted account":         deleted,
		"user in an organization": memberId,
		"missing account":         99999,
	}
	for reason, userId := range refused {
		_, err := CreateOrganizationWithExistingAdmin("Refused Co", userId, 1, 2)
		assert.ErrorIs(t, err, ErrOrganizationAdminNotEligible, reason)
	}

	var after int64
	require.NoError(t, DB.Model(&Organization{}).Count(&after).Error)
	assert.Equal(t, before, after, "refused attempts create no organization")
}
