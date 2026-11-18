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

// Allocations are each member's share of the organization's remaining quota, so they may
// not add up to more than it. Otherwise the organization balance would go negative and a
// member's remaining allocation could not actually be spent.
func TestOrganizationQuotaAllocationCannotExceedOrganizationRemaining(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 1000, 1, "grant", "grant-1", 1)
	require.NoError(t, err)

	assert.ErrorIs(t, SetOrganizationMemberAllocation(orgId, memberId, 5000, 2), ErrOrganizationAllocationExceedsPool,
		"an allocation cannot exceed the organization's remaining quota")
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 1000, 3), "the allocation may equal the remaining quota")

	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(1000), reconciliation.GrantedQuota)
	assert.Equal(t, int64(1000), reconciliation.RemainingQuota)
	assert.Equal(t, int64(1000), reconciliation.Allocated)
	assert.Equal(t, int64(0), reconciliation.Unallocated)
	assert.True(t, reconciliation.Balanced)

	// A second member shares the same remaining quota, so the two allocations must fit together.
	secondMemberId := 11
	require.NoError(t, DB.Create(&User{Id: secondMemberId, Username: "quota-member-2", Password: "not-a-real-password",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "aff-quota-member-2"}).Error)
	require.NoError(t, DB.Create(&OrganizationMembership{OrganizationId: orgId, UserId: secondMemberId,
		Role: OrganizationRoleMember, Status: OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1}).Error)
	assert.ErrorIs(t, SetOrganizationMemberAllocation(orgId, secondMemberId, 1, 4), ErrOrganizationAllocationExceedsPool,
		"the first member already holds the whole remaining quota")

	// Lowering the first allocation frees room for the second.
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 400, 5))
	require.NoError(t, SetOrganizationMemberAllocation(orgId, secondMemberId, 600, 6))
	assert.Equal(t, int64(0), reconciliationOf(t, orgId).Unallocated)
}

// The unallocated balance is the granted quota minus the allocations, because a member
// spending its allocation shrinks the remaining quota and its held share alike. Only a
// grant raises the ceiling, and only lowering an allocation frees room for another member.
func TestOrganizationQuotaUnallocatedTracksGrantsAndAllocations(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 100, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 100, 2))
	require.NoError(t, ConsumeOrganizationQuota(orgId, memberId, "req-1", 1, 60, 3))

	assert.ErrorIs(t, SetOrganizationMemberAllocation(orgId, memberId, 101, 4), ErrOrganizationAllocationExceedsPool,
		"spending does not free allocation room while the remaining quota shrinks with it")
	assert.Equal(t, int64(0), reconciliationOf(t, orgId).Unallocated)

	grown, err := AdjustOrganizationUnallocatedQuota(orgId, OrganizationAdjustAdd, 50, 1, "top up", "adjust-grow", 5)
	require.NoError(t, err)
	assert.Equal(t, int64(50), grown.Unallocated, "a grant is what raises the allocation ceiling")
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 150, 6))

	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(90), reconciliation.RemainingQuota)
	assert.Equal(t, int64(150), reconciliation.Allocated)
	assert.Equal(t, int64(0), reconciliation.Unallocated)
	assert.True(t, reconciliation.Balanced)
}

// Allocations left over from before the cap was enforced may exceed the remaining quota.
// They stay spendable only up to what the organization actually has left, and lowering them
// is still allowed, so an operator can bring the organization back in range.
func TestOrganizationQuotaLegacyOverAllocationIsBoundedByOrganizationRemaining(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 100, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 100, 2))
	// A row written before the cap existed: the member holds 5000 against a 100 quota.
	require.NoError(t, DB.Model(&OrganizationMemberQuota{}).Where("organization_id = ? AND user_id = ?", orgId, memberId).
		Update("allocated_quota", 5000).Error)

	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(5000), reconciliation.Allocated)
	assert.Equal(t, int64(-4900), reconciliation.Unallocated)
	assert.False(t, reconciliation.Balanced, "an over-allocated organization is out of balance")

	err = ConsumeOrganizationQuota(orgId, memberId, "req-1", 1, 200, 3)
	assert.ErrorIs(t, err, ErrOrganizationQuotaInsufficient, "the organization's own remaining quota still bounds spending")
	require.NoError(t, ConsumeOrganizationQuota(orgId, memberId, "req-1", 1, 100, 4), "the organization has 100 left")

	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 100, 5), "an operator may bring it back in range")
	assert.Equal(t, int64(0), reconciliationOf(t, orgId).Unallocated)
}

func TestOrganizationQuotaConsumeChecksMemberAndOrganizationLimits(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 100, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 50, 2))

	require.NoError(t, ConsumeOrganizationQuota(orgId, memberId, "req-1", 1, 40, 3))
	err = ConsumeOrganizationQuota(orgId, memberId, "req-2", 1, 20, 4)
	assert.ErrorIs(t, err, ErrOrganizationMemberQuotaInsufficient, "the member's allocation has 10 left")

	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 100, 5), "the member may hold the rest of the 100 quota")
	err = ConsumeOrganizationQuota(orgId, memberId, "req-3", 1, 80, 6)
	assert.ErrorIs(t, err, ErrOrganizationMemberQuotaInsufficient, "the member has 60 of the allocation left")

	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(40), reconciliation.ConsumedQuota)
	assert.Equal(t, int64(40), reconciliation.MemberUsed)
	assert.True(t, reconciliation.Balanced)
}

func TestOrganizationQuotaSettleCapsAndRecordsShortfall(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 100, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 100, 2))
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
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 100, 2))
	require.NoError(t, ConsumeOrganizationQuota(orgId, memberId, "req-1", 1, 60, 3))

	assert.ErrorIs(t, SetOrganizationMemberAllocation(orgId, memberId, 50, 4), ErrOrganizationMemberQuotaBelowUsage)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 60, 5), "the allocation may equal the usage")
}

func TestOrganizationRequestSettlesToTargetThroughLedger(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	_, err := GrantOrganizationQuota(orgId, 1000, 1, "grant", "grant-task", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 300, 2))
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
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 300, 2))
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
		require.NoError(t, SetOrganizationMemberAllocation(orgId, member, 1200, 2))
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

	// Settle every other request up or down, then settle a few of them all the way back, so the
	// run still exercises refunds alongside charges.
	for index, requestId := range succeeded {
		member := members[index%len(members)]
		delta := int64(50)
		if index%2 == 1 {
			delta = -20
		}
		_, _, err := SettleOrganizationQuota(orgId, member, requestId, 2, delta, 4)
		require.NoError(t, err)
	}
	for index, requestId := range succeeded[:3] {
		member := members[index%len(members)]
		_, _, err := SettleOrganizationRequestTo(orgId, member, requestId, 0, 5)
		require.NoError(t, err)
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
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 100, 2))

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

// Suspending an organization freezes it: its members, their allocations and its quota stay as
// they were until it is resumed. Two groups of writes deliberately survive the freeze, and
// both are load-bearing: a request that already holds quota must still be able to settle and
// give it back, and a member must still be removable, because deleting an organization
// requires a suspended one with no ordinary members left.
func TestSuspendedOrganizationRefusesChangesButStillSettlesAndWindsDown(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	useLogDatabaseForDeletion(t)

	adminId := 30
	require.NoError(t, DB.Create(&User{Id: adminId, Username: "quota-admin", Password: "not-a-real-password",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "aff-quota-admin"}).Error)
	require.NoError(t, DB.Create(&OrganizationMembership{OrganizationId: orgId, UserId: adminId,
		Role: OrganizationRoleAdmin, Status: OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1}).Error)

	_, err := GrantOrganizationQuota(orgId, 1000, 1, "grant", "grant-suspend", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 500, 2))
	require.NoError(t, ConsumeOrganizationQuota(orgId, memberId, "req-1", 1, 200, 3))

	require.NoError(t, ChangeOrganizationStatus(orgId, OrganizationStatusSuspended, 4))

	_, err = GrantOrganizationQuota(orgId, 100, 1, "top up", "grant-while-suspended", 5)
	assert.ErrorIs(t, err, ErrOrganizationNotActive)
	_, err = AdjustOrganizationUnallocatedQuota(orgId, OrganizationAdjustAdd, 100, 1, "top up", "adjust-while-suspended", 5)
	assert.ErrorIs(t, err, ErrOrganizationNotActive)
	assert.ErrorIs(t, SetOrganizationMemberAllocation(orgId, memberId, 600, 5), ErrOrganizationNotActive)
	assert.ErrorIs(t, CreateOrganizationMemberAccount(orgId, "suspended-hire", "not-a-real-password", "", OrganizationRoleMember, 1, 5),
		ErrOrganizationNotActive)
	disabled := OrganizationMembershipDisabled
	assert.ErrorIs(t, UpdateOrganizationMember(orgId, memberId, nil, &disabled, 5), ErrOrganizationNotActive)
	assert.ErrorIs(t, ResetOrganizationMemberPassword(orgId, memberId, "another-not-real-password", 5), ErrOrganizationNotActive)
	assert.ErrorIs(t, ConsumeOrganizationQuota(orgId, memberId, "req-2", 1, 10, 5), ErrOrganizationNotActive)

	// None of the refused calls moved a number.
	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(1000), reconciliation.GrantedQuota)
	assert.Equal(t, int64(500), reconciliation.Allocated)
	assert.Equal(t, int64(200), reconciliation.ConsumedQuota)

	// Quota the request already holds still settles and still comes back.
	charged, shortfall, err := SettleOrganizationQuota(orgId, memberId, "req-1", 2, 50, 6)
	require.NoError(t, err)
	assert.Equal(t, int64(50), charged)
	assert.Equal(t, int64(0), shortfall)
	require.NoError(t, RefundOrganizationPreConsume(orgId, memberId, "req-1", 250, 7))
	assert.Equal(t, int64(0), reconciliationOf(t, orgId).ConsumedQuota)

	// Resuming lifts the freeze in one step, without touching any member row.
	require.NoError(t, ChangeOrganizationStatus(orgId, OrganizationStatusActive, 8))
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 600, 9))
	assert.Equal(t, int64(600), reconciliationOf(t, orgId).Allocated)

	// Winding the organization down stays possible while it is suspended.
	require.NoError(t, ChangeOrganizationStatus(orgId, OrganizationStatusSuspended, 10))
	require.NoError(t, DeleteOrganizationMember(orgId, memberId))
	impact, err := GetOrganizationDeletionImpact(orgId)
	require.NoError(t, err)
	assert.Empty(t, impact.Blockers, "an admin may stay behind, so only ordinary members block deletion")
	require.NoError(t, DeleteOrganization(orgId, 11))
}

func TestOrganizationDeletionReportsBlockersUntilDependenciesAreCleared(t *testing.T) {
	organizationId, memberId := setupOrganizationQuotaTestDB(t)
	useLogDatabaseForDeletion(t)

	_, err := GrantOrganizationQuota(organizationId, 1000, 1, "initial", "grant-delete-blocked", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(organizationId, memberId, 300, 1))

	assert.ErrorIs(t, DeleteOrganization(organizationId, 2), ErrOrganizationDeleteNotSuspended, "an active organization cannot be deleted")
	require.NoError(t, ChangeOrganizationStatus(organizationId, OrganizationStatusSuspended, 2))

	assert.ErrorIs(t, DeleteOrganization(organizationId, 3), ErrOrganizationDeleteBlocked)
	impact, err := GetOrganizationDeletionImpact(organizationId)
	require.NoError(t, err)
	assert.Equal(t, int64(1), impact.MemberCount)
	assert.Equal(t, int64(300), impact.AllocatedQuota)
	assert.Equal(t, int64(700), impact.UnallocatedQuota)
	assert.ElementsMatch(t, []string{"members_remaining"}, impact.Blockers, "a leftover balance does not block deletion")
}

// Removing a member returns the allocation it had not spent to the organization, so the
// quota can be allocated to someone else. The usage it already recorded stays attributed.
func TestOrganizationMemberRemovalReleasesUnspentAllocation(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	useLogDatabaseForDeletion(t)

	_, err := GrantOrganizationQuota(orgId, 1000, 1, "grant", "grant-removal", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 700, 2))
	require.NoError(t, ConsumeOrganizationQuota(orgId, memberId, "req-1", 1, 200, 3))
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 600, 4))
	assert.Equal(t, int64(400), reconciliationOf(t, orgId).Unallocated, "400 of the 1000 quota is not allocated")

	require.NoError(t, DeleteOrganizationMember(orgId, memberId))

	reconciliation := reconciliationOf(t, orgId)
	assert.Equal(t, int64(200), reconciliation.Allocated, "the departing member keeps only what it spent")
	assert.Equal(t, int64(200), reconciliation.ConsumedQuota)
	assert.Equal(t, int64(800), reconciliation.Unallocated, "the unspent 400 returns to the pool")
	assert.True(t, reconciliation.Balanced)

	// The freed quota can now be allocated to a remaining member.
	remaining := &User{Id: 21, Username: "quota-remaining", Password: "not-a-real-password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, AffCode: "aff-quota-remaining"}
	require.NoError(t, DB.Create(remaining).Error)
	require.NoError(t, DB.Create(&OrganizationMembership{OrganizationId: orgId, UserId: remaining.Id,
		Role: OrganizationRoleMember, Status: OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1}).Error)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, remaining.Id, 800, 5))
	assert.Equal(t, int64(0), reconciliationOf(t, orgId).Unallocated)
}

func TestOrganizationDeletionSoftDeletesAClearedOrganization(t *testing.T) {
	organizationId, memberId := setupOrganizationQuotaTestDB(t)
	useLogDatabaseForDeletion(t)

	require.NoError(t, ChangeOrganizationStatus(organizationId, OrganizationStatusSuspended, 2))
	require.NoError(t, DeleteOrganizationMember(organizationId, memberId))

	impact, err := GetOrganizationDeletionImpact(organizationId)
	require.NoError(t, err)
	assert.Empty(t, impact.Blockers, "a suspended organization without members or balances has nothing left to clear")

	require.NoError(t, DeleteOrganization(organizationId, 3))
	organization, err := GetOrganizationById(organizationId)
	require.NoError(t, err)
	assert.Equal(t, OrganizationStatusDeleted, organization.Status)

	listed, total, err := ListOrganizations(0, 20, "")
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, listed, "deleted organizations are hidden from the default list")

	assert.ErrorIs(t, ChangeOrganizationStatus(organizationId, OrganizationStatusActive, 4), ErrOrganizationStatusTransition,
		"a deleted organization cannot be resumed")
}

// useLogDatabaseForDeletion points the log database at the test database, because the
// deletion impact counts consume logs there. It also creates the tables the impact
// counts and member removal touch.
func useLogDatabaseForDeletion(t *testing.T) {
	t.Helper()
	previousLogDB := LOG_DB
	LOG_DB = DB
	t.Cleanup(func() { LOG_DB = previousLogDB })
	require.NoError(t, DB.AutoMigrate(&Log{}, &Task{}, &UserSession{}, &AuthFlow{}, &ExternalIdentityClaim{},
		&PasskeyCredential{}, &TwoFA{}, &TwoFABackupCode{}, &UserOAuthBinding{}, &UserAccessToken{}))
}

func TestOrganizationUnallocatedAdjustmentAddsSubtractsAndOverridesThroughTheLedger(t *testing.T) {
	organizationId, memberId := setupOrganizationQuotaTestDB(t)

	added, err := AdjustOrganizationUnallocatedQuota(organizationId, OrganizationAdjustAdd, 1000, 1, "top up", "adjust-add-1", 1)
	require.NoError(t, err)
	assert.True(t, added.Applied)
	assert.Equal(t, int64(1000), added.Unallocated)

	subtracted, err := AdjustOrganizationUnallocatedQuota(organizationId, OrganizationAdjustSubtract, 400, 1, "correction", "adjust-subtract-1", 2)
	require.NoError(t, err)
	assert.True(t, subtracted.Applied)
	assert.Equal(t, int64(600), subtracted.Unallocated)

	_, err = AdjustOrganizationUnallocatedQuota(organizationId, OrganizationAdjustSubtract, 700, 1, "too much", "adjust-subtract-2", 3)
	assert.ErrorIs(t, err, ErrOrganizationUnallocatedInsufficient, "the pool cannot go below zero")

	require.NoError(t, SetOrganizationMemberAllocation(organizationId, memberId, 300, 3))
	unchanged, err := AdjustOrganizationUnallocatedQuota(organizationId, OrganizationAdjustOverride, 300, 1, "already there", "adjust-override-same", 4)
	require.NoError(t, err)
	assert.False(t, unchanged.Applied, "overriding to the current value changes nothing")
	assert.Equal(t, int64(300), unchanged.Unallocated, "quota held by the member is not part of the pool")

	cleared, err := AdjustOrganizationUnallocatedQuota(organizationId, OrganizationAdjustOverride, 0, 1, "clear before deletion", "adjust-override-0", 5)
	require.NoError(t, err)
	assert.True(t, cleared.Applied)
	assert.Equal(t, int64(0), cleared.Unallocated)

	repeated, err := AdjustOrganizationUnallocatedQuota(organizationId, OrganizationAdjustAdd, 1000, 1, "top up", "adjust-add-1", 6)
	require.NoError(t, err)
	assert.False(t, repeated.Applied, "a reference applies once")

	_, err = AdjustOrganizationUnallocatedQuota(organizationId, "double", 1, 1, "bad mode", "adjust-bad-mode", 7)
	assert.ErrorIs(t, err, ErrOrganizationAdjustModeInvalid)

	var revokes, grants int64
	require.NoError(t, DB.Model(&OrganizationQuotaLedgerEntry{}).Where("event_type = ?", OrganizationLedgerRevoke).Count(&revokes).Error)
	require.NoError(t, DB.Model(&OrganizationQuotaLedgerEntry{}).Where("event_type = ?", OrganizationLedgerGrant).Count(&grants).Error)
	assert.Equal(t, int64(2), revokes, "subtract and the clearing override are recorded as revokes")
	assert.Equal(t, int64(1), grants, "only the add is recorded as a grant")
}

// The list figures agree with the reconciliation for the same organization: only active members count,
// and an organization without a quota account shows zeros.
func TestOrganizationSummariesCountActiveMembersAndMatchReconciliation(t *testing.T) {
	orgId, memberId := setupOrganizationQuotaTestDB(t)
	disabled := &User{Id: 11, Username: "disabled-member", Password: "not-a-real-password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, AffCode: "aff-disabled-member"}
	require.NoError(t, DB.Create(disabled).Error)
	require.NoError(t, DB.Create(&OrganizationMembership{OrganizationId: orgId, UserId: disabled.Id,
		Role: OrganizationRoleMember, Status: "disabled", CreatedTime: 1, UpdatedTime: 1}).Error)
	empty := &Organization{Name: "No Account Co", Status: OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, DB.Create(empty).Error)

	_, err := GrantOrganizationQuota(orgId, 1000, 1, "grant", "grant-1", 1)
	require.NoError(t, err)
	require.NoError(t, SetOrganizationMemberAllocation(orgId, memberId, 300, 2))

	summaries, total, err := ListOrganizationSummaries(0, 10, "")
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	byId := map[int]OrganizationSummary{}
	for _, summary := range summaries {
		byId[summary.Id] = summary
	}

	reconciliation := reconciliationOf(t, orgId)
	quota := byId[orgId]
	assert.Equal(t, int64(1), quota.MemberCount, "the disabled membership is not counted")
	assert.Equal(t, reconciliation.GrantedQuota, quota.GrantedQuota)
	assert.Equal(t, reconciliation.Allocated, quota.AllocatedQuota)
	assert.Equal(t, reconciliation.Unallocated, quota.UnallocatedQuota)
	assert.Equal(t, int64(700), quota.UnallocatedQuota)

	bare := byId[empty.Id]
	assert.Equal(t, int64(0), bare.MemberCount)
	assert.Equal(t, int64(0), bare.GrantedQuota)
	assert.Equal(t, int64(0), bare.UnallocatedQuota)
}
