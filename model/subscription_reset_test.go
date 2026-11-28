package model

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func seedSubscriptionResetPlan(t *testing.T, plan *SubscriptionPlan) {
	t.Helper()
	require.NoError(t, DB.Create(plan).Error)
}

func seedSubscriptionResetSub(t *testing.T, sub *UserSubscription) {
	t.Helper()
	require.NoError(t, DB.Create(sub).Error)
}

func getSubscriptionResetSub(t *testing.T, id int) UserSubscription {
	t.Helper()
	var sub UserSubscription
	require.NoError(t, DB.Where("id = ?", id).First(&sub).Error)
	return sub
}

func TestAdminResetUserSubscriptionsByPlanResetsAllActiveMatchesAndAdvancesTime(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Id:               9101,
		Title:            "Pro",
		PriceAmount:      10,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      1000,
		QuotaResetPeriod: SubscriptionResetDaily,
	}
	otherPlan := &SubscriptionPlan{
		Id:               9102,
		Title:            "Basic",
		PriceAmount:      1,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      100,
		QuotaResetPeriod: SubscriptionResetDaily,
	}
	seedSubscriptionResetPlan(t, plan)
	seedSubscriptionResetPlan(t, otherPlan)

	activeEnd := now + 30*24*3600
	expiredEnd := now - 1
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9201, UserId: 101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 300, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 120})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9202, UserId: 101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 500, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 120})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9203, UserId: 101, PlanId: otherPlan.Id, AmountTotal: 100, AmountUsed: 60, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 120})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9204, UserId: 101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 700, StartTime: now - 7200, EndTime: expiredEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now - 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9205, UserId: 102, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 800, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 120})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9206, UserId: 101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 900, StartTime: now - 3600, EndTime: activeEnd, Status: "cancelled", LastResetTime: now - 3600, NextResetTime: now + 120})

	beforeReset := GetDBTimestamp()
	result, err := AdminResetUserSubscriptionsByPlan(101, plan.Id, true)
	afterReset := GetDBTimestamp()

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, plan.Id, result.PlanId)
	assert.Equal(t, 2, result.MatchedCount)
	assert.Equal(t, 2, result.ResetCount)
	assert.Equal(t, 1, result.UserCount)
	assert.Equal(t, []int{101}, result.AffectedUserIds)
	assert.True(t, result.AdvanceResetTime)

	for _, id := range []int{9201, 9202} {
		sub := getSubscriptionResetSub(t, id)
		assert.Zero(t, sub.AmountUsed)
		assert.GreaterOrEqual(t, sub.LastResetTime, beforeReset)
		assert.LessOrEqual(t, sub.LastResetTime, afterReset)
		assert.Equal(t, calcNextResetTime(time.Unix(sub.LastResetTime, 0), plan, sub.EndTime), sub.NextResetTime)
	}
	assert.EqualValues(t, 60, getSubscriptionResetSub(t, 9203).AmountUsed)
	assert.EqualValues(t, 700, getSubscriptionResetSub(t, 9204).AmountUsed)
	assert.EqualValues(t, 800, getSubscriptionResetSub(t, 9205).AmountUsed)
	assert.EqualValues(t, 900, getSubscriptionResetSub(t, 9206).AmountUsed)
}

func TestAdminResetUserSubscriptionsByPlanKeepsResetTimes(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Id:               9301,
		Title:            "Team",
		PriceAmount:      20,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      2000,
		QuotaResetPeriod: SubscriptionResetMonthly,
	}
	seedSubscriptionResetPlan(t, plan)

	lastReset := now - 86400
	nextReset := now + 86400
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9302, UserId: 201, PlanId: plan.Id, AmountTotal: 2000, AmountUsed: 1200, StartTime: now - 172800, EndTime: now + 30*24*3600, Status: "active", LastResetTime: lastReset, NextResetTime: nextReset})

	result, err := AdminResetUserSubscriptionsByPlan(201, plan.Id, false)

	require.NoError(t, err)
	assert.False(t, result.AdvanceResetTime)
	sub := getSubscriptionResetSub(t, 9302)
	assert.Zero(t, sub.AmountUsed)
	assert.Equal(t, lastReset, sub.LastResetTime)
	assert.Equal(t, nextReset, sub.NextResetTime)
}

func TestAdminResetUserSubscriptionsByPlanNoActiveMatchReturnsError(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Id:            9401,
		Title:         "Expired",
		PriceAmount:   10,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   1000,
	}
	seedSubscriptionResetPlan(t, plan)
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9402, UserId: 301, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 500, StartTime: now - 7200, EndTime: now - 1, Status: "active"})

	result, err := AdminResetUserSubscriptionsByPlan(301, plan.Id, true)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, strings.Contains(err.Error(), "This user has no active subscription to this plan"))
}

func TestAdminResetPlanSubscriptionsResetsAllActiveUsers(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Id:               9501,
		Title:            "Business",
		PriceAmount:      30,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      3000,
		QuotaResetPeriod: SubscriptionResetNever,
	}
	seedSubscriptionResetPlan(t, plan)

	activeEnd := now + 30*24*3600
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9502, UserId: 401, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1000, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9503, UserId: 401, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1100, StartTime: now - 3500, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9504, UserId: 402, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1200, StartTime: now - 3400, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9505, UserId: 403, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1300, StartTime: now - 7200, EndTime: now - 1, Status: "active", LastResetTime: now - 3600, NextResetTime: now - 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9506, UserId: 404, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1400, StartTime: now - 3600, EndTime: activeEnd, Status: "cancelled", LastResetTime: now - 3600, NextResetTime: now + 10})

	result, err := AdminResetPlanSubscriptions(plan.Id, true)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 3, result.MatchedCount)
	assert.Equal(t, 3, result.ResetCount)
	assert.Equal(t, 2, result.UserCount)
	assert.Equal(t, []int{401, 402}, result.AffectedUserIds)
	for _, id := range []int{9502, 9503, 9504} {
		sub := getSubscriptionResetSub(t, id)
		assert.Zero(t, sub.AmountUsed)
		assert.Zero(t, sub.LastResetTime)
		assert.Zero(t, sub.NextResetTime)
	}
	assert.EqualValues(t, 1300, getSubscriptionResetSub(t, 9505).AmountUsed)
	assert.EqualValues(t, 1400, getSubscriptionResetSub(t, 9506).AmountUsed)
}

func TestSubscriptionRefundAcrossDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, upgrade := range []bool{false, true} {
				t.Run(fmt.Sprintf("upgrade=%v", upgrade), func(t *testing.T) {
					db := openSubscriptionRefundTestDB(t, dialect)
					previous, previousType := DB, common.MainDatabaseType()
					DB = db
					common.SetMainDatabaseType(common.DatabaseType(dialect))
					t.Cleanup(func() { DB = previous; common.SetMainDatabaseType(previousType) })
					verifySubscriptionRefund(t, db, upgrade)
				})
			}
		})
	}
}

func openSubscriptionRefundTestDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	switch dialect {
	case "mysql":
		if os.Getenv("TEST_MYSQL_DSN") == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		t.Setenv("TEST_ORGANIZATION_QUOTA_DIALECT", dialect)
		db, _ := openOrganizationQuotaTestDatabase(t)
		return db
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		uri, err := url.Parse(dsn)
		require.NoError(t, err)
		require.True(t, uri.Hostname() == "127.0.0.1" && uri.Port() == "5432" && uri.Path == "/newapi_e2e")
		admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		adminSQL, err := admin.DB()
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, adminSQL.Close()) })
		schema := fmt.Sprintf("subscription_refund_test_%d", time.Now().UnixNano())
		name := clause.Table{Name: schema}
		require.NoError(t, admin.Exec("CREATE SCHEMA ?", name).Error)
		t.Cleanup(func() { assert.NoError(t, admin.Exec("DROP SCHEMA ? CASCADE", name).Error) })
		query := uri.Query()
		query.Set("search_path", schema)
		uri.RawQuery = query.Encode()
		db, err := gorm.Open(postgres.Open(uri.String()), &gorm.Config{})
		require.NoError(t, err)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
		return db
	default:
		db, err := gorm.Open(sqlite.Open(t.TempDir()+"/refund.db?_pragma=busy_timeout(100)&_txlock=immediate"), &gorm.Config{})
		require.NoError(t, err)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(2)
		t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
		return db
	}
}

func verifySubscriptionRefund(t *testing.T, db *gorm.DB, upgrade bool) {
	t.Helper()
	versionSQL := "SELECT version()"
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		versionSQL = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
	t.Logf("database version: %s", version)
	migrate := func() {
		require.NoError(t, db.AutoMigrate(&UserSubscription{}, &SubscriptionPreConsumeRecord{}, &SubscriptionPlan{}))
	}
	migrate()
	now := GetDBTimestamp()
	plan := &SubscriptionPlan{Id: 9901, Title: "Refund matrix", DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1000, QuotaResetPeriod: SubscriptionResetDaily}
	sub := &UserSubscription{UserId: 9902, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 100, Status: "active", StartTime: now - 3600, EndTime: now + 3600, LastResetTime: now - 3600, NextResetTime: now + 60}
	require.NoError(t, db.Create(plan).Error)
	if upgrade {
		require.NoError(t, db.Migrator().DropColumn(&UserSubscription{}, "usage_reset_epoch"))
		require.NoError(t, db.Migrator().DropColumn(&SubscriptionPreConsumeRecord{}, "usage_reset_epoch"))
	}
	require.NoError(t, db.Omit("UsageResetEpoch").Create(sub).Error)
	old := SubscriptionPreConsumeRecord{RequestId: "refund-before-upgrade", UserId: sub.UserId, UserSubscriptionId: sub.Id, PreConsumed: 100, Status: "consumed"}
	require.NoError(t, db.Omit("UsageResetEpoch").Create(&old).Error)
	migrate()
	migrate()
	assert.Error(t, db.Create(&SubscriptionPreConsumeRecord{RequestId: old.RequestId}).Error, "the request id remains unique after migration")
	require.NoError(t, RefundSubscriptionPreConsume(old.RequestId))
	require.NoError(t, RefundSubscriptionPreConsume(old.RequestId))
	assert.Zero(t, getSubscriptionResetSub(t, sub.Id).AmountUsed)

	res, err := PreConsumeUserSubscription("refund-original-period", sub.UserId, "matrix", 0, 100)
	require.NoError(t, err)
	require.NotNil(t, res.UsageResetEpoch)
	require.NoError(t, ReserveSubscriptionQuotaForPeriod("refund-original-period", 20))
	require.NoError(t, RefundSubscriptionPreConsume("refund-original-period"))
	assert.Zero(t, getSubscriptionResetSub(t, sub.Id).AmountUsed, "release the top-up and original amount together")

	other, err := PreConsumeUserSubscription("refund-after-reset", sub.UserId, "matrix", 0, 100)
	require.NoError(t, err)
	_, err = AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, false)
	require.NoError(t, err)
	_, err = PreConsumeUserSubscription("current-period-usage", sub.UserId, "matrix", 0, 60)
	require.NoError(t, err)
	require.ErrorIs(t, ReserveSubscriptionQuotaForPeriod("refund-after-reset", 1), ErrSubscriptionPeriodChanged)
	require.NoError(t, RefundSubscriptionPreConsume("refund-after-reset"))
	require.NoError(t, SettleTaskSubscriptionUsage(&Task{PrivateData: TaskPrivateData{SubscriptionId: sub.Id, SubscriptionPeriodStart: other.UsageResetEpoch}}, -40))
	assert.EqualValues(t, 60, getSubscriptionResetSub(t, sub.Id).AmountUsed, "a previous-period refund must not erase current-period usage")
	require.NoError(t, SettleTaskSubscriptionUsage(&Task{PrivateData: TaskPrivateData{SubscriptionId: sub.Id, SubscriptionPeriodStart: other.UsageResetEpoch}}, 15))
	assert.EqualValues(t, 75, getSubscriptionResetSub(t, sub.Id).AmountUsed, "positive settlement preserves the existing charging policy")
}

func TestAdminResetPlanSubscriptionsNoMatchSucceeds(t *testing.T) {
	truncateTables(t)

	plan := &SubscriptionPlan{
		Id:            9601,
		Title:         "Empty",
		PriceAmount:   10,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   1000,
	}
	seedSubscriptionResetPlan(t, plan)

	result, err := AdminResetPlanSubscriptions(plan.Id, true)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Zero(t, result.MatchedCount)
	assert.Zero(t, result.ResetCount)
	assert.Zero(t, result.UserCount)
	assert.Empty(t, result.AffectedUserIds)
}
