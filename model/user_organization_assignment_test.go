package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createPaidKeyForMember(t *testing.T, db *gorm.DB, userId int, organizationId int, key string) *Token {
	t.Helper()
	token := &Token{UserId: userId, Key: key, Name: "paid", Status: common.TokenStatusEnabled,
		CreatedTime: 1, AccessedTime: 1, ExpiredTime: -1, UnlimitedQuota: true, BillingOrganizationId: &organizationId}
	require.NoError(t, db.Create(token).Error)
	return token
}

func countMemberships(t *testing.T, userId int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&OrganizationMembership{}).Where("user_id = ?", userId).Count(&count).Error)
	return count
}

// Deleting a user from the user list removes the membership and deletes the account,
// the same as deleting any other user.
func TestAdminDeleteRemovesOrganizationMembershipsAndDisablesPaidKeys(t *testing.T) {
	organizationId, memberId := setupOrganizationQuotaTestDB(t)
	useLogDatabaseForDeletion(t)
	createPaidKeyForMember(t, DB, memberId, organizationId, "sk-admin-delete-paid")

	_, err := HardDeleteUserById(memberId)
	require.NoError(t, err)

	assert.Equal(t, int64(0), countMemberships(t, memberId))
	var users int64
	require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", memberId).Count(&users).Error)
	assert.Equal(t, int64(0), users)
	var tokens int64
	require.NoError(t, DB.Unscoped().Model(&Token{}).Where("user_id = ?", memberId).Count(&tokens).Error)
	assert.Equal(t, int64(0), tokens)
}

func TestRemovingMembershipsDisablesKeysBoundToTheOrganization(t *testing.T) {
	organizationId, memberId := setupOrganizationQuotaTestDB(t)
	token := createPaidKeyForMember(t, DB, memberId, organizationId, "sk-remove-paid")

	var keys []string
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		keys, err = removeUserOrganizationMembershipsTx(tx, memberId)
		return err
	}))

	assert.Equal(t, []string{token.Key}, keys)
	assert.Equal(t, int64(0), countMemberships(t, memberId))
	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, stored.Status)
}

func TestSetUserOrganizationKeepsRolesAndMovesMembers(t *testing.T) {
	organizationId, memberId := setupOrganizationQuotaTestDB(t)
	other := &Organization{Name: "Other Co", Status: OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, DB.Create(other).Error)

	set := func(target int, role string) []string {
		t.Helper()
		var keys []string
		require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
			var err error
			keys, err = SetUserOrganizationWithTx(tx, memberId, target, role)
			return err
		}))
		return keys
	}
	membershipOf := func() *OrganizationMembership {
		t.Helper()
		var membership OrganizationMembership
		require.NoError(t, DB.Where("user_id = ?", memberId).First(&membership).Error)
		return &membership
	}

	set(organizationId, "")
	assert.Equal(t, OrganizationRoleMember, membershipOf().Role, "an empty role keeps the current role")

	set(organizationId, OrganizationRoleAdmin)
	assert.Equal(t, OrganizationRoleAdmin, membershipOf().Role)
	assert.Equal(t, organizationId, membershipOf().OrganizationId)

	set(other.Id, "")
	assert.Equal(t, other.Id, membershipOf().OrganizationId, "moving keeps a single membership")
	assert.Equal(t, OrganizationRoleMember, membershipOf().Role, "a new organization defaults to member")
	assert.Equal(t, int64(1), countMemberships(t, memberId))

	set(UserOrganizationNone, "")
	assert.Equal(t, int64(0), countMemberships(t, memberId))
}

func TestMovingAMemberDisablesKeysBoundToThePreviousOrganization(t *testing.T) {
	organizationId, memberId := setupOrganizationQuotaTestDB(t)
	other := &Organization{Name: "Other Co", Status: OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, DB.Create(other).Error)
	token := createPaidKeyForMember(t, DB, memberId, organizationId, "sk-move-paid")

	var keys []string
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		keys, err = SetUserOrganizationWithTx(tx, memberId, other.Id, "")
		return err
	}))

	assert.Equal(t, []string{token.Key}, keys)
	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, stored.Status)
}

func TestSetUserOrganizationRejectsInvalidTargets(t *testing.T) {
	organizationId, memberId := setupOrganizationQuotaTestDB(t)
	gone := &Organization{Name: "Gone Co", Status: OrganizationStatusDeleted, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, DB.Create(gone).Error)

	attempt := func(target int, role string) error {
		return DB.Transaction(func(tx *gorm.DB) error {
			_, err := SetUserOrganizationWithTx(tx, memberId, target, role)
			return err
		})
	}

	assert.ErrorIs(t, attempt(organizationId, "owner"), ErrOrganizationInvalidRole)
	assert.ErrorIs(t, attempt(gone.Id, OrganizationRoleMember), ErrOrganizationNotFound, "deleted organizations cannot receive members")
	assert.ErrorIs(t, attempt(99999, OrganizationRoleMember), ErrOrganizationNotFound)
	assert.Equal(t, int64(1), countMemberships(t, memberId), "rejected changes leave the membership as it was")
}
