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

package model

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupTokenBillingTest opens an isolated database with two organizations: one active
// with an active member (10) and a disabled member (11), and one suspended with member 12.
func setupTokenBillingTest(t *testing.T) (activeOrgId int, suspendedOrgId int, db *gorm.DB) {
	t.Helper()
	db, dialectType := openOrganizationQuotaTestDatabase(t)
	previousDB := DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	DB = db
	common.RedisEnabled = false
	common.SetDatabaseTypes(dialectType, dialectType)
	t.Cleanup(func() {
		DB = previousDB
		common.RedisEnabled = previousRedis
		common.SetDatabaseTypes(previousMain, previousLog)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Organization{}, &OrganizationMembership{}, &Log{}, &Task{},
		&UserSession{}, &UserAccessToken{}, &AuthFlow{}, &PasskeyCredential{}, &TwoFA{}, &TwoFABackupCode{},
		&ExternalIdentityClaim{}, &UserOAuthBinding{}))
	for _, id := range []int{10, 11, 12} {
		require.NoError(t, db.Create(&User{Id: id, Username: fmt.Sprintf("token-billing-%d", id), Password: "not-a-real-password",
			Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: fmt.Sprintf("aff-token-billing-%d", id)}).Error)
	}

	active := &Organization{Name: "Active Co", Status: OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, db.Create(active).Error)
	suspended := &Organization{Name: "Suspended Co", Status: OrganizationStatusSuspended, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, db.Create(suspended).Error)
	require.NoError(t, db.Create(&OrganizationMembership{OrganizationId: active.Id, UserId: 10, Role: OrganizationRoleMember,
		Status: OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1}).Error)
	require.NoError(t, db.Create(&OrganizationMembership{OrganizationId: active.Id, UserId: 11, Role: OrganizationRoleMember,
		Status: OrganizationMembershipDisabled, CreatedTime: 1, UpdatedTime: 1}).Error)
	require.NoError(t, db.Create(&OrganizationMembership{OrganizationId: suspended.Id, UserId: 12, Role: OrganizationRoleMember,
		Status: OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1}).Error)
	return active.Id, suspended.Id, db
}

func createTokenBillingTestToken(t *testing.T, db *gorm.DB, userId int, key string) *Token {
	t.Helper()
	token := &Token{UserId: userId, Key: key, Name: "billing-test", Status: common.TokenStatusEnabled,
		CreatedTime: 1, AccessedTime: 1, ExpiredTime: -1, UnlimitedQuota: true, AllowUserQuotaQuery: true}
	require.NoError(t, db.Create(token).Error)
	return token
}

func TestTokenBindingRequiresActiveMemberOfActiveOrganization(t *testing.T) {
	activeOrgId, suspendedOrgId, db := setupTokenBillingTest(t)
	ownToken := createTokenBillingTestToken(t, db, 10, "sk-binding-active")
	disabledToken := createTokenBillingTestToken(t, db, 11, "sk-binding-disabled")

	bound, err := BindTokenBillingOrganization(ownToken.Id, 10, &activeOrgId)
	require.NoError(t, err)
	require.NotNil(t, bound.BillingOrganizationId)
	assert.Equal(t, activeOrgId, *bound.BillingOrganizationId)
	assert.False(t, bound.AllowUserQuotaQuery, "a company-paid key must not expose the owner's quota")

	_, err = BindTokenBillingOrganization(disabledToken.Id, 11, &activeOrgId)
	assert.ErrorIs(t, err, ErrTokenBillingOrganizationNotMember, "a disabled member cannot bind a key")

	suspendedToken := createTokenBillingTestToken(t, db, 12, "sk-binding-suspended")
	_, err = BindTokenBillingOrganization(suspendedToken.Id, 12, &suspendedOrgId)
	assert.ErrorIs(t, err, ErrTokenBillingOrganizationNotMember, "a member of a suspended organization cannot bind to it")

	missingOrgId := 999
	_, err = BindTokenBillingOrganization(ownToken.Id, 10, &missingOrgId)
	assert.ErrorIs(t, err, ErrTokenBillingOrganizationNotMember, "a missing organization cannot be bound")
}

func TestTokenBindingIsOwnerOnlyAndClearsToPersonal(t *testing.T) {
	activeOrgId, _, db := setupTokenBillingTest(t)
	token := createTokenBillingTestToken(t, db, 10, "sk-binding-owner")

	// Another user, even one who is a member of the organization, cannot bind this key.
	_, err := BindTokenBillingOrganization(token.Id, 11, &activeOrgId)
	assert.Error(t, err)

	_, err = BindTokenBillingOrganization(token.Id, 10, &activeOrgId)
	require.NoError(t, err)
	_, err = BindTokenBillingOrganization(token.Id, 10, nil)
	require.NoError(t, err)

	var stored Token
	require.NoError(t, db.First(&stored, token.Id).Error)
	assert.Nil(t, stored.BillingOrganizationId, "clearing the binding returns the key to personal billing")
}

func TestBillingOrganizationSnapshotIsStoredOnLogsAndTasks(t *testing.T) {
	activeOrgId, _, db := setupTokenBillingTest(t)
	paid := activeOrgId
	require.NoError(t, db.Create(&Log{UserId: 10, CreatedAt: 100, Type: LogTypeConsume, BillingOrganizationId: paid}).Error)
	require.NoError(t, db.Create(&Log{UserId: 10, CreatedAt: 101, Type: LogTypeConsume}).Error)
	require.NoError(t, db.Create(&Task{UserId: 10, TaskID: "task-paid", BillingOrganizationId: paid}).Error)
	require.NoError(t, db.Create(&Task{UserId: 10, TaskID: "task-personal"}).Error)

	var logs []Log
	require.NoError(t, db.Where("billing_organization_id = ?", paid).Find(&logs).Error)
	assert.Len(t, logs, 1, "organization-paid logs can be found by their snapshot")

	var personal int64
	require.NoError(t, db.Model(&Log{}).Where("billing_organization_id = ?", 0).Count(&personal).Error)
	assert.Equal(t, int64(1), personal)

	var tasks []Task
	require.NoError(t, db.Where("billing_organization_id = ?", paid).Find(&tasks).Error)
	require.Len(t, tasks, 1)
	assert.Equal(t, "task-paid", tasks[0].TaskID)
}

func TestConsumeLogStoresPayingOrganizationSnapshot(t *testing.T) {
	activeOrgId, _, db := setupTokenBillingTest(t)
	previousLogDB, previousEnabled := LOG_DB, common.LogConsumeEnabled
	LOG_DB = db
	common.LogConsumeEnabled = true
	t.Cleanup(func() {
		LOG_DB = previousLogDB
		common.LogConsumeEnabled = previousEnabled
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	RecordConsumeLog(c, 10, RecordConsumeLogParams{ModelName: "snapshot-model", Quota: 5, BillingOrganizationId: activeOrgId})
	RecordConsumeLog(c, 10, RecordConsumeLogParams{ModelName: "snapshot-model", Quota: 7})

	var paid []Log
	require.NoError(t, db.Where("billing_organization_id = ?", activeOrgId).Find(&paid).Error)
	require.Len(t, paid, 1)
	assert.Equal(t, 5, paid[0].Quota)

	var personal []Log
	require.NoError(t, db.Where("billing_organization_id = ?", 0).Find(&personal).Error)
	require.Len(t, personal, 1)
	assert.Equal(t, 7, personal[0].Quota)
}

func TestDisablingMemberDisablesOrganizationPaidKeysOnly(t *testing.T) {
	activeOrgId, _, db := setupTokenBillingTest(t)
	paid := createTokenBillingTestToken(t, db, 10, "sk-step4-paid")
	personal := createTokenBillingTestToken(t, db, 10, "sk-step4-personal")
	_, err := BindTokenBillingOrganization(paid.Id, 10, &activeOrgId)
	require.NoError(t, err)

	disabled := OrganizationMembershipDisabled
	require.NoError(t, UpdateOrganizationMember(activeOrgId, 10, nil, &disabled, 5))

	var storedPaid, storedPersonal Token
	require.NoError(t, db.First(&storedPaid, paid.Id).Error)
	require.NoError(t, db.First(&storedPersonal, personal.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, storedPaid.Status, "the company-paid key is disabled")
	require.NotNil(t, storedPaid.BillingOrganizationId)
	assert.Equal(t, activeOrgId, *storedPaid.BillingOrganizationId, "the key stays bound, it is not moved to personal billing")
	assert.Equal(t, common.TokenStatusEnabled, storedPersonal.Status, "the member's personal keys are unaffected")

	// Re-enabling the membership does not re-enable the company-paid key.
	enabled := OrganizationMembershipActive
	require.NoError(t, UpdateOrganizationMember(activeOrgId, 10, nil, &enabled, 6))
	require.NoError(t, db.First(&storedPaid, paid.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, storedPaid.Status, "the key stays disabled until its owner re-enables it")
}

func TestRemovingMemberDisablesOrganizationPaidKeys(t *testing.T) {
	activeOrgId, _, db := setupTokenBillingTest(t)
	paid := createTokenBillingTestToken(t, db, 10, "sk-step4-removed")
	_, err := BindTokenBillingOrganization(paid.Id, 10, &activeOrgId)
	require.NoError(t, err)

	require.NoError(t, DeleteOrganizationMember(activeOrgId, 10))

	// Removing the account also removes its keys, so the key can no longer authenticate.
	var stored Token
	err = db.Unscoped().First(&stored, paid.Id).Error
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// A consume log written before the removal keeps the organization snapshot.
	require.NoError(t, db.Create(&Log{UserId: 10, CreatedAt: 200, Type: LogTypeConsume, TokenId: paid.Id, BillingOrganizationId: activeOrgId}).Error)
	var history []Log
	require.NoError(t, db.Where("token_id = ?", paid.Id).Find(&history).Error)
	require.Len(t, history, 1)
	assert.Equal(t, activeOrgId, history[0].BillingOrganizationId)
}

func TestBillingOrganizationRoundTripsThroughRedisHashCache(t *testing.T) {
	useUserCacheMiniRedis(t)
	organizationId := 5
	paid := Token{Id: 44, UserId: 10, Key: "token-billing-cache-paid", Name: "billing-cache", BillingOrganizationId: &organizationId}
	personal := Token{Id: 45, UserId: 10, Key: "token-billing-cache-personal", Name: "billing-cache"}

	for _, token := range []Token{paid, personal} {
		initialized, err := cacheInitToken(token)
		require.NoError(t, err)
		require.Equal(t, 1, initialized)
	}

	cachedPaid, err := cacheGetTokenByKey(paid.Key)
	require.NoError(t, err)
	assert.Equal(t, organizationId, cachedPaid.BillingOrganizationIdValue(), "a cached company-paid key keeps its payer")

	cachedPersonal, err := cacheGetTokenByKey(personal.Key)
	require.NoError(t, err)
	assert.Nil(t, cachedPersonal.BillingOrganizationId, "a cached personal key stays on the owner's wallet")
}

func TestSuspendingOrganizationDisablesItsPaidKeysOnly(t *testing.T) {
	activeOrgId, _, db := setupTokenBillingTest(t)
	paid := createTokenBillingTestToken(t, db, 10, "sk-suspend-paid")
	personal := createTokenBillingTestToken(t, db, 10, "sk-suspend-personal")
	_, err := BindTokenBillingOrganization(paid.Id, 10, &activeOrgId)
	require.NoError(t, err)

	require.NoError(t, ChangeOrganizationStatus(activeOrgId, OrganizationStatusSuspended, 5))

	var storedPaid, storedPersonal Token
	require.NoError(t, db.First(&storedPaid, paid.Id).Error)
	require.NoError(t, db.First(&storedPersonal, personal.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, storedPaid.Status, "the company-paid key is disabled while the organization is suspended")
	require.NotNil(t, storedPaid.BillingOrganizationId)
	assert.Equal(t, activeOrgId, *storedPaid.BillingOrganizationId, "the key stays bound to the organization")
	assert.Equal(t, common.TokenStatusEnabled, storedPersonal.Status, "the owner's personal keys are unaffected")

	// Resuming needs an active admin; promote member 10 so the organization can resume.
	require.NoError(t, db.Model(&OrganizationMembership{}).Where("organization_id = ? AND user_id = ?", activeOrgId, 10).
		Update("role", OrganizationRoleAdmin).Error)
	require.NoError(t, ChangeOrganizationStatus(activeOrgId, OrganizationStatusActive, 6))
	require.NoError(t, db.First(&storedPaid, paid.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, storedPaid.Status, "resuming the organization does not re-enable its keys")
}
