package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The user list reports each user's organization by name and filters by it. Users in a
// deleted organization count as having no organization.
func TestUserListFiltersByOrganizationAndReportsItsName(t *testing.T) {
	organizationId, _ := setupOrganizationQuotaTestDB(t)

	other := &Organization{Name: "Other Co", Status: OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, DB.Create(other).Error)
	gone := &Organization{Name: "Gone Co", Status: OrganizationStatusDeleted, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, DB.Create(gone).Error)

	plain := &User{Id: 11, Username: "plain-user", Password: "not-a-real-password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, AffCode: "aff-plain-user"}
	require.NoError(t, DB.Create(plain).Error)
	departed := &User{Id: 12, Username: "departed-user", Password: "not-a-real-password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, AffCode: "aff-departed-user"}
	require.NoError(t, DB.Create(departed).Error)
	require.NoError(t, DB.Create(&OrganizationMembership{OrganizationId: gone.Id, UserId: departed.Id,
		Role: OrganizationRoleMember, Status: OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1}).Error)

	search := func(organizationFilter *int) map[string]*User {
		users, _, err := SearchUsers("", "", nil, nil, organizationFilter, 0, 20, NewUserSortOptions("id", "asc"))
		require.NoError(t, err)
		byName := make(map[string]*User, len(users))
		for _, user := range users {
			byName[user.Username] = user
		}
		return byName
	}

	all := search(nil)
	require.Contains(t, all, "quota-member")
	require.NotNil(t, all["quota-member"].Organization)
	assert.Equal(t, organizationId, all["quota-member"].Organization.Id)
	assert.Equal(t, "Quota Co", all["quota-member"].Organization.Name)
	assert.Nil(t, all["plain-user"].Organization)
	assert.Nil(t, all["departed-user"].Organization, "a user in a deleted organization reports no organization")

	inOrganization := search(&organizationId)
	assert.Contains(t, inOrganization, "quota-member")
	assert.NotContains(t, inOrganization, "plain-user")
	assert.NotContains(t, inOrganization, "departed-user")

	none := search(intPtr(UserOrganizationNone))
	assert.Contains(t, none, "plain-user")
	assert.Contains(t, none, "departed-user", "users whose organization is deleted belong to no organization")
	assert.NotContains(t, none, "quota-member")

	otherMembers := search(&other.Id)
	assert.Empty(t, otherMembers)

	deletedMembers := search(&gone.Id)
	assert.Empty(t, deletedMembers, "deleted organizations cannot be filtered")
}

func TestGetAllUsersReportsOrganizationNames(t *testing.T) {
	setupOrganizationQuotaTestDB(t)

	users, total, err := GetAllUsers(&common.PageInfo{Page: 1, PageSize: 20}, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(len(users)), total)
	for _, user := range users {
		if user.Username == "quota-member" {
			require.NotNil(t, user.Organization)
			assert.Equal(t, "Quota Co", user.Organization.Name)
			return
		}
	}
	t.Fatal("quota-member missing from the user list")
}

func intPtr(value int) *int {
	return &value
}
