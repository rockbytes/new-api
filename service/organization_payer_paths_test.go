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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupPayerPathsTest opens an isolated database with an organization that has a granted
// total and one active member with an allocation, plus the member's personal wallet.
func setupPayerPathsTest(t *testing.T) (organizationId int, userId int) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/payer-paths.db?_pragma=busy_timeout(30000)&_txlock=immediate"), &gorm.Config{})
	require.NoError(t, err)
	previousDB := model.DB
	previousLogDB, previousLogEnabled := model.LOG_DB, common.LogConsumeEnabled
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	model.DB = db
	model.LOG_DB = db
	common.LogConsumeEnabled = false
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.LogConsumeEnabled = previousLogEnabled
		common.RedisEnabled = previousRedis
		common.SetDatabaseTypes(previousMain, previousLog)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Organization{}, &model.OrganizationMembership{},
		&model.OrganizationQuotaAccount{}, &model.OrganizationMemberQuota{}, &model.OrganizationQuotaLedgerEntry{}, &model.Log{}, &model.Task{}, &model.UserSubscription{}))

	member := &model.User{Id: 30, Username: "payer-member", Password: "not-a-real-password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, AffCode: "aff-payer-member", Quota: 1000}
	require.NoError(t, db.Create(member).Error)
	organization := &model.Organization{Name: "Payer Co", Status: model.OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, db.Create(organization).Error)
	require.NoError(t, db.Create(&model.OrganizationMembership{OrganizationId: organization.Id, UserId: member.Id,
		Role: model.OrganizationRoleMember, Status: model.OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1}).Error)
	_, err = model.GrantOrganizationQuota(organization.Id, 10000, 1, "grant", "grant-payer", 1)
	require.NoError(t, err)
	require.NoError(t, model.SetOrganizationMemberAllocation(organization.Id, member.Id, 5000, false, 2))
	return organization.Id, member.Id
}

// newPayerTestContext returns a relay-like context with a request, which the billing
// session reads for the response language.
func newPayerTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

// Each request path settles through the same billing session. The table covers the path
// kinds that differ in their relay flags: synchronous text, streaming, image, audio and
// asynchronous tasks (which pre-consume the full amount).
func TestOrganizationPayerIsUsedByEveryRequestPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name      string
		isStream  bool
		isImage   bool
		isAudio   bool
		isTask    bool
		forceFull bool
	}{
		{name: "synchronous text"},
		{name: "streaming text", isStream: true},
		{name: "image generation", isImage: true},
		{name: "audio", isAudio: true},
		{name: "asynchronous task", isTask: true, forceFull: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orgId, userId := setupPayerPathsTest(t)
			c := newPayerTestContext()
			info := &relaycommon.RelayInfo{
				UserId:               userId,
				RequestId:            "req-" + tc.name,
				TokenUnlimited:       true,
				PayingOrganizationId: orgId,
				IsStream:             tc.isStream,
				ForcePreConsume:      tc.forceFull,
			}
			if tc.isImage {
				info.RelayFormat = types.RelayFormatOpenAIImage
			}
			if tc.isAudio {
				info.RelayFormat = types.RelayFormatOpenAIAudio
			}

			require.Nil(t, PreConsumeBilling(c, 100, info))
			require.Nil(t, SettleBilling(c, info, 60))

			reconciliation, err := model.GetOrganizationQuotaReconciliation(orgId)
			require.NoError(t, err)
			assert.Equal(t, int64(60), reconciliation.ConsumedQuota, "organization usage follows the actual charge")
			assert.True(t, reconciliation.Balanced)

			var user model.User
			require.NoError(t, model.DB.First(&user, userId).Error)
			assert.Equal(t, 1000, user.Quota, "the member's personal wallet is not charged for an organization-paid request")
		})
	}
}

// A request without a paying organization keeps the personal wallet path, and the
// organization is not charged.
func TestPersonalKeyRequestIsNotChargedToOrganization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orgId, userId := setupPayerPathsTest(t)
	c := newPayerTestContext()
	info := &relaycommon.RelayInfo{UserId: userId, RequestId: "req-personal", TokenUnlimited: true}

	require.Nil(t, PreConsumeBilling(c, 100, info))
	require.Nil(t, SettleBilling(c, info, 60))

	reconciliation, err := model.GetOrganizationQuotaReconciliation(orgId)
	require.NoError(t, err)
	assert.Equal(t, int64(0), reconciliation.ConsumedQuota, "a personal key does not use organization quota")

	var user model.User
	require.NoError(t, model.DB.First(&user, userId).Error)
	assert.Equal(t, 940, user.Quota, "the personal wallet pays for a personal key")
}

// An organization-paid task settles and refunds through the organization ledger. The
// member's wallet is never charged or refunded for it.
func TestOrganizationPaidTaskSettlesAndRefundsThroughLedger(t *testing.T) {
	orgId, userId := setupPayerPathsTest(t)
	require.NoError(t, model.ConsumeOrganizationQuota(orgId, userId, "req-paid-task", 1, 100, common.GetTimestamp()))
	task := &model.Task{UserId: userId, TaskID: "task-org-paid", Quota: 100, BillingOrganizationId: orgId,
		PrivateData: model.TaskPrivateData{BillingSource: BillingSourceOrganization, BillingRequestId: "req-paid-task"}}
	require.NoError(t, model.DB.Create(task).Error)

	RecalculateTaskQuota(context.Background(), task, 70, common.NewMessage("settled"))
	reconciliation, err := model.GetOrganizationQuotaReconciliation(orgId)
	require.NoError(t, err)
	assert.Equal(t, int64(70), reconciliation.ConsumedQuota, "the organization keeps the settled usage")

	require.True(t, RefundTaskQuota(context.Background(), task, "failed"))
	reconciliation, err = model.GetOrganizationQuotaReconciliation(orgId)
	require.NoError(t, err)
	assert.Equal(t, int64(0), reconciliation.ConsumedQuota, "a failed task returns its organization usage")
	assert.True(t, reconciliation.Balanced)

	var user model.User
	require.NoError(t, model.DB.First(&user, userId).Error)
	assert.Equal(t, 1000, user.Quota, "the member's personal wallet is untouched by an organization-paid task")
}
