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
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	sqlmysql "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// openOrganizationQuotaTestDatabase opens an isolated database for one test. SQLite
// uses a temporary file with the production busy timeout. MySQL and PostgreSQL use the
// loopback instance named by TEST_<DIALECT>_DSN, create a fresh database, and drop it
// after the test. Set TEST_ORGANIZATION_QUOTA_DIALECT to choose the dialect.
func openOrganizationQuotaTestDatabase(t *testing.T) (*gorm.DB, common.DatabaseType) {
	t.Helper()
	dialect := os.Getenv("TEST_ORGANIZATION_QUOTA_DIALECT")
	if dialect == "" || dialect == "sqlite" {
		db, err := gorm.Open(sqlite.Open(t.TempDir()+"/quota.db?_pragma=busy_timeout(30000)&_txlock=immediate"), &gorm.Config{})
		require.NoError(t, err)
		return db, common.DatabaseTypeSQLite
	}
	dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
	require.NotEmpty(t, dsn)
	name := fmt.Sprintf("newapi_orgquota_%d", time.Now().UnixNano())
	switch dialect {
	case "mysql":
		config, err := sqlmysql.ParseDSN(dsn)
		require.NoError(t, err)
		require.Equal(t, "tcp", config.Net)
		host, _, err := net.SplitHostPort(config.Addr)
		require.NoError(t, err)
		require.True(t, net.ParseIP(host).IsLoopback(), "database tests only permit loopback instances")
		admin, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, admin.Exec("CREATE DATABASE `"+name+"`").Error)
		config.DBName = name
		db, err := gorm.Open(mysql.Open(config.FormatDSN()), &gorm.Config{})
		require.NoError(t, err)
		t.Cleanup(func() {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
			_ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`").Error
			if sqlDB, err := admin.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		return db, common.DatabaseTypeMySQL
	case "postgres":
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.True(t, net.ParseIP(parsed.Hostname()).IsLoopback(), "database tests only permit loopback instances")
		admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, admin.Exec(`CREATE DATABASE "`+name+`"`).Error)
		parsed.Path = "/" + name
		db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
		require.NoError(t, err)
		t.Cleanup(func() {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
			_ = admin.Exec(`DROP DATABASE IF EXISTS "` + name + `"`).Error
			if sqlDB, err := admin.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		return db, common.DatabaseTypePostgreSQL
	}
	t.Fatalf("unsupported organization quota test dialect %q", dialect)
	return nil, common.DatabaseTypeSQLite
}

// setupOrganizationQuotaTestDB opens an isolated database with one active member.
func setupOrganizationQuotaTestDB(t *testing.T) (organizationId int, memberId int) {
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
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Organization{}, &OrganizationMembership{}, &OrganizationQuotaAccount{},
		&OrganizationMemberQuota{}, &OrganizationQuotaLedgerEntry{}))

	organization := &Organization{Name: "Quota Co", Status: OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, db.Create(organization).Error)
	member := &User{Id: 10, Username: "quota-member", Password: "not-a-real-password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, AffCode: "aff-quota-member"}
	require.NoError(t, db.Create(member).Error)
	require.NoError(t, db.Create(&OrganizationMembership{OrganizationId: organization.Id, UserId: member.Id,
		Role: OrganizationRoleMember, Status: OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1}).Error)
	return organization.Id, member.Id
}

func reconciliationOf(t *testing.T, organizationId int) *OrganizationQuotaReconciliation {
	t.Helper()
	reconciliation, err := GetOrganizationQuotaReconciliation(organizationId)
	require.NoError(t, err)
	return reconciliation
}

func TestOrganizationQuotaAllocationIsALimitAndMayExceedRemaining(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 1000, 1, "grant", "grant-1", 1)
	require.NoError(t, err)

	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 5000, false, 2))

	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(1000), reconciliation.GrantedQuota)
	assert.Equal(t, int64(1000), reconciliation.RemainingQuota)
	assert.Equal(t, int64(5000), reconciliation.Allocated, "allocations are limits, so they may sum above the remaining quota")
	assert.True(t, reconciliation.Balanced)
}

func TestOrganizationQuotaConsumeChecksMemberAndOrganizationLimits(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 100, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 50, false, 2))

	require.NoError(t, ConsumeOrganizationQuota(orgId, memberId, "req-1", 1, 40, 3))
	err = ConsumeOrganizationQuota(orgId, memberId, "req-2", 1, 20, 4)
	assert.ErrorIs(t, err, ErrOrganizationMemberQuotaInsufficient, "the member's allocation has 10 left")

	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 500, false, 5))
	err = ConsumeOrganizationQuota(orgId, memberId, "req-3", 1, 80, 6)
	assert.ErrorIs(t, err, ErrOrganizationQuotaInsufficient, "the organization has 60 left")

	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(40), reconciliation.ConsumedQuota)
	assert.Equal(t, int64(40), reconciliation.MemberUsed)
	assert.True(t, reconciliation.Balanced)
}

func TestOrganizationQuotaSettleCapsAndRecordsShortfall(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 100, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 200, false, 2))
	require.NoError(t, ConsumeOrganizationQuota(orgId, memberId, "req-1", 1, 60, 3))

	// The organization has 40 left, so settling 50 more charges 40 and records 10 as shortfall.
	charged, shortfall, err := SettleOrganizationQuota(orgId, memberId, "req-1", 2, 50, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(40), charged)
	assert.Equal(t, int64(10), shortfall)

	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(100), reconciliation.ConsumedQuota)
	assert.Equal(t, int64(0), reconciliation.RemainingQuota)
	assert.True(t, reconciliation.Balanced)
}

func TestOrganizationQuotaAllocationCannotDropBelowUsage(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 1000, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 100, false, 2))
	require.NoError(t, ConsumeOrganizationQuota(orgId, memberId, "req-1", 1, 60, 3))

	assert.ErrorIs(t, SetOrganizationMemberAllocation(orgId, memberId, 50, false, 4), ErrOrganizationMemberQuotaBelowUsage)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 60, false, 5), "the allocation may equal the usage")
}

func TestOrganizationQuotaManualRefundIsCappedAndIdempotent(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 1000, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 300, false, 2))
	require.NoError(t, ConsumeOrganizationQuota(orgId, memberId, "req-3", 1, 100, 3))
	_, _, err = SettleOrganizationQuota(orgId, memberId, "req-3", 2, -40, 4)
	require.NoError(t, err)

	// Net charged to the request is 60: 100 pre-consumed, 40 settled back.
	applied, err := RefundOrganizationRequest(orgId, "req-3", 70, 1, "refund-over", 5)
	assert.ErrorIs(t, err, ErrOrganizationRefundExceeded)
	assert.False(t, applied)

	applied, err = RefundOrganizationRequest(orgId, "req-3", 60, 1, "refund-1", 6)
	require.NoError(t, err)
	assert.True(t, applied)
	applied, err = RefundOrganizationRequest(orgId, "req-3", 60, 1, "refund-1", 7)
	require.NoError(t, err)
	assert.False(t, applied, "replaying the same manual refund must not refund twice")

	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(0), reconciliation.ConsumedQuota)
	assert.Equal(t, int64(0), reconciliation.MemberUsed)
	assert.True(t, reconciliation.Balanced)
}

func TestOrganizationRequestSettlesToTargetThroughLedger(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 1000, 1, "grant", "grant-task", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 300, false, 2))
	require.NoError(t, ConsumeOrganizationQuota(orgId, memberId, "req-task", 1, 100, 3))

	// Settling to 70 refunds 30. Repeating the same settlement changes nothing.
	charged, shortfall, err := SettleOrganizationRequestTo(orgId, memberId, "req-task", 70, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(0), charged)
	assert.Equal(t, int64(0), shortfall)
	_, _, err = SettleOrganizationRequestTo(orgId, memberId, "req-task", 70, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(70), reconciliationOf(t, orgId).ConsumedQuota, "a repeated settlement must not refund twice")

	// Raising to 120 charges 50 more, within the member's allocation of 300.
	charged, shortfall, err = SettleOrganizationRequestTo(orgId, memberId, "req-task", 120, 6)
	require.NoError(t, err)
	assert.Equal(t, int64(50), charged)
	assert.Equal(t, int64(0), shortfall)

	// A failed task settles to zero, which returns everything it still holds.
	_, _, err = SettleOrganizationRequestTo(orgId, memberId, "req-task", 0, 7)
	require.NoError(t, err)
	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(0), reconciliation.ConsumedQuota)
	assert.Equal(t, int64(0), reconciliation.MemberUsed)
	assert.True(t, reconciliation.Balanced)
}

func TestOrganizationQuotaDisabledMemberKeepsAllocationAndUsage(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 1000, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 300, false, 2))
	require.NoError(t, ConsumeOrganizationQuota(orgId, memberId, "req-4", 1, 100, 3))

	// Disabling blocks use only. The allocation and the recorded usage stay as they are.
	disabled := OrganizationMembershipDisabled
	require.NoError(t, UpdateOrganizationMember(orgId, memberId, nil, &disabled, 4))
	err = ConsumeOrganizationQuota(orgId, memberId, "req-5", 1, 10, 5)
	assert.ErrorIs(t, err, ErrOrganizationMembershipMissing, "a disabled member cannot consume quota")

	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(300), reconciliation.Allocated)
	assert.Equal(t, int64(100), reconciliation.ConsumedQuota)
	assert.Equal(t, int64(900), reconciliation.RemainingQuota)
	assert.True(t, reconciliation.Balanced)
}

// A mixed workload across several members, with concurrent usage, settlements in both
// directions, manual refunds and a disable. The ledger must reproduce the organization
// and member totals exactly, and no limit may be exceeded.
func TestOrganizationQuotaLedgerMatchesSnapshotUnderMixedWorkload(t *testing.T) {
	orgId, firstMember := setupOrganizationQuotaTestDB(t)
	members := []int{firstMember}
	for i := 2; i <= 4; i++ {
		userId := 100 + i
		require.NoError(t, DB.Create(&User{Id: userId, Username: fmt.Sprintf("workload-%d", i), Password: "not-a-real-password",
			Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: fmt.Sprintf("aff-workload-%d", i)}).Error)
		require.NoError(t, DB.Create(&OrganizationMembership{OrganizationId: orgId, UserId: userId, Role: OrganizationRoleMember,
			Status: OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1}).Error)
		members = append(members, userId)
	}
	_, err := GrantOrganizationQuota(orgId, 5000, 1, "grant", "grant-workload", 1)
	require.NoError(t, err)
	for _, member := range members {
		require.NoError(t, SetOrganizationMemberAllocation(orgId, member, 1200, false, 2))
	}

	// Forty requests, reserved concurrently. Limits decide which succeed.
	var mu sync.Mutex
	var wg sync.WaitGroup
	var succeeded []string
	for i := range 40 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			requestId := fmt.Sprintf("req-%02d", i)
			if err := ConsumeOrganizationQuota(orgId, members[i%len(members)], requestId, 1, 100, 3); err == nil {
				mu.Lock()
				succeeded = append(succeeded, requestId)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	require.NotEmpty(t, succeeded)

	// Settle every other request up or down, then refund part of a few requests manually.
	for index, requestId := range succeeded {
		member := members[index%len(members)]
		delta := int64(50)
		if index%2 == 1 {
			delta = -20
		}
		_, _, err := SettleOrganizationQuota(orgId, member, requestId, 2, delta, 4)
		require.NoError(t, err)
	}
	for _, requestId := range succeeded[:3] {
		applied, err := RefundOrganizationRequest(orgId, requestId, 10, 1, "refund-"+requestId, 5)
		require.NoError(t, err)
		assert.True(t, applied)
	}
	disabled := OrganizationMembershipDisabled
	require.NoError(t, UpdateOrganizationMember(orgId, members[1], nil, &disabled, 6))

	reconciliation := reconciliationOf(t, orgId)
	require.True(t, reconciliation.Balanced, "organization usage must equal the sum of member usage")
	assert.LessOrEqual(t, reconciliation.ConsumedQuota, reconciliation.GrantedQuota)

	// The ledger must reproduce the snapshot, both for the organization and per member.
	type net struct {
		Total int64
	}
	ledgerNet := func(where string, args ...any) int64 {
		var sum net
		require.NoError(t, DB.Raw("SELECT COALESCE(SUM(CASE WHEN event_type = 'refund' THEN -amount WHEN event_type IN ('preconsume','settle') THEN amount ELSE 0 END), 0) AS total FROM organization_quota_ledger_entries WHERE "+where, args...).Scan(&sum).Error)
		return sum.Total
	}
	assert.Equal(t, reconciliation.ConsumedQuota, ledgerNet("organization_id = ?", orgId), "organization usage must match the ledger")
	for _, member := range members {
		var row OrganizationMemberQuota
		require.NoError(t, DB.Where("organization_id = ? AND user_id = ?", orgId, member).First(&row).Error)
		assert.Equal(t, row.UsedQuota, ledgerNet("organization_id = ? AND target_user_id = ?", orgId, member), "member %d usage must match the ledger", member)
		assert.LessOrEqual(t, row.UsedQuota, row.AllocatedQuota, "member %d must not exceed the allocation", member)
	}
}

func TestOrganizationQuotaConcurrentConsumeNeverOverdrawsOrganization(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 100, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 1000, false, 2))

	const attempts = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := ConsumeOrganizationQuota(orgId, memberId, "req-"+string(rune('a'+i)), 1, 10, 3); err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 10, succeeded, "only the organization's remaining quota can be consumed")
	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(0), reconciliation.RemainingQuota)
	assert.True(t, reconciliation.Balanced)
}
