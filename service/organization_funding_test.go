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

package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupOrganizationFundingTest opens an isolated database with one active organization
// and one active member, whose allocation is set by each test.
func setupOrganizationFundingTest(t *testing.T) (organizationId int, userId int) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/funding.db?_pragma=busy_timeout(30000)&_txlock=immediate"), &gorm.Config{})
	require.NoError(t, err)
	previousDB := model.DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	model.DB = db
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedis
		common.SetDatabaseTypes(previousMain, previousLog)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Organization{}, &model.OrganizationMembership{},
		&model.OrganizationQuotaAccount{}, &model.OrganizationMemberQuota{}, &model.OrganizationQuotaLedgerEntry{}))

	organization := &model.Organization{Name: "Funding Co", Status: model.OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, db.Create(organization).Error)
	member := &model.User{Id: 20, Username: "funding-member", Password: "not-a-real-password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, AffCode: "aff-funding-member"}
	require.NoError(t, db.Create(member).Error)
	require.NoError(t, db.Create(&model.OrganizationMembership{OrganizationId: organization.Id, UserId: member.Id,
		Role: model.OrganizationRoleMember, Status: model.OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1}).Error)
	return organization.Id, member.Id
}

func TestOrganizationQuotaFundingRejectsByMemberLimitWithoutFallback(t *testing.T) {
	orgId, userId := setupOrganizationFundingTest(t)
	_, err := model.GrantOrganizationQuota(orgId, 1000, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, model.SetOrganizationMemberAllocation(orgId, userId, 40, false, 2))
	funding := &OrganizationQuotaFunding{organizationId: orgId, userId: userId, requestId: "req-a"}

	require.NoError(t, funding.PreConsume(30))
	assert.ErrorIs(t, funding.PreConsume(20), ErrInsufficientMemberQuota, "the member's allocation has 10 left")
	assert.Equal(t, BillingSourceOrganization, funding.Source())
	assert.Equal(t, 30, funding.outstanding, "a rejected reservation holds nothing")
}

func TestOrganizationQuotaFundingReportsDisabledMemberAndSuspendedOrganization(t *testing.T) {
	orgId, userId := setupOrganizationFundingTest(t)
	_, err := model.GrantOrganizationQuota(orgId, 1000, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, model.SetOrganizationMemberAllocation(orgId, userId, 500, false, 2))

	disabled := model.OrganizationMembershipDisabled
	require.NoError(t, model.UpdateOrganizationMember(orgId, userId, nil, &disabled, 3))
	funding := &OrganizationQuotaFunding{organizationId: orgId, userId: userId, requestId: "req-d"}
	assert.ErrorIs(t, funding.PreConsume(10), ErrOrganizationMemberInactive)
	assert.Equal(t, 0, funding.outstanding)

	enabled := model.OrganizationMembershipActive
	require.NoError(t, model.UpdateOrganizationMember(orgId, userId, nil, &enabled, 4))
	require.NoError(t, model.ChangeOrganizationStatus(orgId, model.OrganizationStatusSuspended, 5))
	funding = &OrganizationQuotaFunding{organizationId: orgId, userId: userId, requestId: "req-e"}
	assert.ErrorIs(t, funding.PreConsume(10), ErrOrganizationInactive)
}

func TestOrganizationQuotaFundingRejectsByOrganizationLimit(t *testing.T) {
	orgId, userId := setupOrganizationFundingTest(t)
	_, err := model.GrantOrganizationQuota(orgId, 50, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, model.SetOrganizationMemberAllocation(orgId, userId, 500, false, 2))
	funding := &OrganizationQuotaFunding{organizationId: orgId, userId: userId, requestId: "req-b"}

	require.NoError(t, funding.PreConsume(40))
	assert.ErrorIs(t, funding.PreConsume(20), ErrInsufficientOrganizationQuota, "the organization has 10 left")
}

func TestOrganizationQuotaFundingSettlesCappedAndRefundsOnce(t *testing.T) {
	orgId, userId := setupOrganizationFundingTest(t)
	_, err := model.GrantOrganizationQuota(orgId, 100, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, model.SetOrganizationMemberAllocation(orgId, userId, 200, false, 2))
	funding := &OrganizationQuotaFunding{organizationId: orgId, userId: userId, requestId: "req-c"}

	require.NoError(t, funding.PreConsume(60))
	// The organization has 40 left, so settling 50 more charges 40 and records 10 as shortfall.
	require.NoError(t, funding.Settle(50))
	assert.Equal(t, 100, funding.outstanding)

	require.NoError(t, funding.Refund())
	assert.Equal(t, 0, funding.outstanding)
	require.NoError(t, funding.Refund(), "a second refund holds nothing and must not refund again")

	reconciliation, err := model.GetOrganizationQuotaReconciliation(orgId)
	require.NoError(t, err)
	assert.Equal(t, int64(0), reconciliation.ConsumedQuota)
	assert.Equal(t, int64(0), reconciliation.MemberUsed)
	assert.True(t, reconciliation.Balanced)
}
