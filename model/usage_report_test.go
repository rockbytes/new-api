package model

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/clickhouse"
	"gorm.io/gorm"
)

// usageReportBase is the UTC midnight of a fixed day, so every test row has a known day.
const usageReportBase int64 = 1799971200

// setupUsageReportTest opens an isolated database and makes it the log database. The
// fixtures cover two organizations, personal billing, and a non-usage log type.
func setupUsageReportTest(t *testing.T) {
	t.Helper()
	db, dialectType := openUsageReportLogDatabase(t)
	previousDB, previousLogDB := DB, LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(dialectType, dialectType)
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	rows := []Log{
		// Organization 1, user 10: two consume rows on the first day.
		{UserId: 10, Username: "alice", Type: LogTypeConsume, ModelName: "m1", Quota: 100, PromptTokens: 10, CompletionTokens: 5, CreatedAt: usageReportBase + 3600, BillingOrganizationId: 1, RequestId: "r1", ChannelId: 7, Ip: "10.0.0.1"},
		{UserId: 10, Username: "alice", Type: LogTypeConsume, ModelName: "m2", Quota: 50, CreatedAt: usageReportBase + 7200, BillingOrganizationId: 1, RequestId: "r2"},
		// Organization 1, user 11 on the second day.
		{UserId: 11, Username: "bob", Type: LogTypeConsume, ModelName: "m1", Quota: 30, CreatedAt: usageReportBase + 86400 + 3600, BillingOrganizationId: 1, RequestId: "r3"},
		// Organization 1, user 10 late on the previous day, which local time moves across midnight.
		{UserId: 10, Username: "alice", Type: LogTypeConsume, ModelName: "m1", Quota: 5, CreatedAt: usageReportBase - 3600, BillingOrganizationId: 1, RequestId: "r8"},
		// Personal billing for user 10, and organization 2 for user 12.
		{UserId: 10, Username: "alice", Type: LogTypeConsume, ModelName: "m1", Quota: 70, CreatedAt: usageReportBase + 3600, BillingOrganizationId: 0, RequestId: "r4"},
		{UserId: 12, Username: "carol", Type: LogTypeConsume, ModelName: "m3", Quota: 40, CreatedAt: usageReportBase + 3600, BillingOrganizationId: 2, RequestId: "r7"},
		// A login log is not usage and must not be counted.
		{UserId: 10, Username: "alice", Type: LogTypeLogin, Quota: 999, CreatedAt: usageReportBase + 3600, BillingOrganizationId: 1, RequestId: "r9"},
	}
	require.NoError(t, db.Create(&rows).Error)
}

func usageReportWindow(tzOffsetSeconds int64) UsageWindow {
	return UsageWindow{Start: usageReportBase - 86400, End: usageReportBase + 2*86400, TzOffsetSeconds: tzOffsetSeconds}
}

func TestUsagePayerQueryConsistency(t *testing.T) {
	setupUsageReportTest(t)
	cases := []struct {
		name   string
		scope  UsageScope
		window UsageWindow
		quota  int64
		ids    []string
		models []string
		names  []string
		users  []int64
		orgs   []int64
	}{
		{"personal", UsageScope{PayerType: "personal"}, usageReportWindow(0), 70, []string{"r4"}, []string{"m1"}, []string{"m1"}, []int64{10}, []int64{0}},
		{"organization", UsageScope{PayerType: "organization"}, usageReportWindow(0), 225, []string{"r1", "r2", "r3", "r7", "r8"}, []string{"m1", "m2", "m3"}, []string{"m1", "m2", "m3"}, []int64{10, 11, 12}, []int64{1, 2}},
		{"personal user mismatch", UsageScope{PayerType: "personal", UserId: 11}, usageReportWindow(0), 0, nil, nil, nil, nil, nil},
		{"historical organization user", UsageScope{PayerType: "organization", UserId: 10}, usageReportWindow(0), 155, []string{"r1", "r2", "r8"}, []string{"m1", "m2"}, []string{"m1", "m2"}, []int64{10}, []int64{1}},
		{"organization model", UsageScope{PayerType: "organization", OrganizationId: 1, ModelName: "m2"}, usageReportWindow(0), 50, []string{"r2"}, []string{"m2"}, []string{"m1", "m2"}, []int64{10}, []int64{1}},
		{"personal model exclusion", UsageScope{PayerType: "personal", ModelName: "m2"}, usageReportWindow(0), 0, nil, nil, []string{"m1"}, nil, nil},
		{"inclusive time boundary", UsageScope{PayerType: "organization"}, UsageWindow{Start: usageReportBase + 3600, End: usageReportBase + 3600}, 140, []string{"r1", "r7"}, []string{"m1", "m3"}, []string{"m1", "m3"}, []int64{10, 12}, []int64{1, 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary, err := GetUsageSummary(tc.scope, tc.window)
			require.NoError(t, err)
			assert.Equal(t, tc.quota, summary.ConsumeQuota)
			assert.Equal(t, int64(len(tc.ids)), summary.Requests)
			rows, total, err := GetUsageRecords(tc.scope, tc.window, 1, 100)
			require.NoError(t, err)
			assert.Equal(t, int64(len(tc.ids)), total)
			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				ids = append(ids, row.RequestId)
			}
			assert.ElementsMatch(t, tc.ids, ids)
			models, err := GetUsageModels(tc.scope, tc.window)
			require.NoError(t, err)
			exportModels, err := GetUsageExportModels(tc.scope, tc.window)
			require.NoError(t, err)
			assert.ElementsMatch(t, models, exportModels)
			modelNames := make([]string, 0, len(models))
			for _, row := range models {
				modelNames = append(modelNames, row.ModelName)
			}
			assert.ElementsMatch(t, tc.models, modelNames)
			names, err := GetUsageModelNames(tc.scope, tc.window)
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.names, names)
			for _, group := range []struct {
				by   UsageRankBy
				keys []int64
			}{{UsageRankByUser, tc.users}, {UsageRankByOrganization, tc.orgs}} {
				groups, err := GetUsageExportGroups(tc.scope, tc.window, group.by)
				require.NoError(t, err)
				keys := make([]int64, 0, len(groups))
				var quota, count int64
				for _, row := range groups {
					keys = append(keys, row.Key)
					quota += row.ConsumeQuota
					count += row.Requests
				}
				assert.ElementsMatch(t, group.keys, keys)
				assert.Equal(t, tc.quota, quota)
				assert.Equal(t, int64(len(tc.ids)), count)
			}
		})
	}
}

func TestUsageSummaryCountsSettledConsumptionForEachScope(t *testing.T) {
	setupUsageReportTest(t)
	window := usageReportWindow(0)

	// Consume 100+50+30+5 for organization 1; the login log is not usage.
	organization, err := GetUsageSummary(UsageScope{OrganizationId: 1}, window)
	require.NoError(t, err)
	assert.Equal(t, int64(185), organization.ConsumeQuota)
	assert.Equal(t, int64(4), organization.Requests)
	assert.Equal(t, int64(10), organization.PromptTokens)

	member, err := GetUsageSummary(UsageScope{OrganizationId: 1, UserId: 10}, window)
	require.NoError(t, err)
	assert.Equal(t, int64(155), member.ConsumeQuota)
	assert.Equal(t, int64(3), member.Requests, "user 10 has three consume rows in organization 1")

	platform, err := GetUsageSummary(UsageScope{}, window)
	require.NoError(t, err)
	assert.Equal(t, int64(295), platform.ConsumeQuota, "all payers: 100+50+30+5+70+40, excluding the login log")
	assert.Equal(t, int64(6), platform.Requests)
}

func TestUsageTrendBucketsByLocalDay(t *testing.T) {
	setupUsageReportTest(t)

	utc, err := GetUsageTrend(UsageScope{OrganizationId: 1}, usageReportWindow(0))
	require.NoError(t, err)
	require.Len(t, utc, 3)
	assert.Equal(t, usageReportBase-86400, utc[0].DayStart)
	assert.Equal(t, int64(5), utc[0].ConsumeQuota)
	assert.Equal(t, usageReportBase, utc[1].DayStart)
	assert.Equal(t, int64(150), utc[1].ConsumeQuota)
	assert.Equal(t, int64(2), utc[1].Requests)
	assert.Equal(t, usageReportBase+86400, utc[2].DayStart)
	assert.Equal(t, int64(30), utc[2].ConsumeQuota)

	// At UTC+8 the 23:00 UTC row belongs to the local day that starts 8 hours before the
	// UTC midnight, so the first two buckets move.
	local, err := GetUsageTrend(UsageScope{OrganizationId: 1}, usageReportWindow(8*3600))
	require.NoError(t, err)
	require.Len(t, local, 2)
	assert.Equal(t, usageReportBase-8*3600, local[0].DayStart)
	assert.Equal(t, int64(155), local[0].ConsumeQuota, "the 23:00 UTC row joins the first local day")
	assert.Equal(t, usageReportBase+16*3600, local[1].DayStart)
	assert.Equal(t, int64(30), local[1].ConsumeQuota)
}

func TestUsageModelsAndRankingsRespectScope(t *testing.T) {
	setupUsageReportTest(t)
	window := usageReportWindow(0)

	models, err := GetUsageModels(UsageScope{OrganizationId: 1}, window)
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "m1", models[0].ModelName)
	assert.Equal(t, int64(135), models[0].ConsumeQuota)
	assert.Equal(t, "m2", models[1].ModelName)
	assert.Equal(t, int64(50), models[1].ConsumeQuota)

	byUser, err := GetUsageRanking(UsageScope{OrganizationId: 1}, window, UsageRankByUser)
	require.NoError(t, err)
	require.Len(t, byUser, 2)
	assert.Equal(t, int64(10), byUser[0].Key)
	assert.Equal(t, int64(155), byUser[0].ConsumeQuota)
	assert.Equal(t, int64(11), byUser[1].Key)

	// Personal billing ranks as organization 0, next to the real organizations.
	byOrganization, err := GetUsageRanking(UsageScope{}, window, UsageRankByOrganization)
	require.NoError(t, err)
	require.Len(t, byOrganization, 3)
	assert.Equal(t, int64(1), byOrganization[0].Key)
	assert.Equal(t, int64(185), byOrganization[0].ConsumeQuota)
	assert.Equal(t, int64(0), byOrganization[1].Key)
	assert.Equal(t, int64(70), byOrganization[1].ConsumeQuota)
}

func TestUsageExportGroupsCompleteAndScoped(t *testing.T) {
	setupUsageReportTest(t)
	window := usageReportWindow(0)
	rows := make([]Log, 51)
	for i := range rows {
		rows[i] = Log{UserId: 100 + i, Username: "shared", Type: LogTypeConsume,
			ModelName: fmt.Sprintf("extra-%02d", i), Quota: 1, PromptTokens: 2, CompletionTokens: 3,
			CreatedAt: usageReportBase, BillingOrganizationId: 100 + i}
	}
	rows = append(rows, Log{UserId: 100, Username: "z-renamed", Type: LogTypeConsume,
		ModelName: "extra-00", Quota: 4, PromptTokens: 5, CompletionTokens: 6,
		CreatedAt: usageReportBase, BillingOrganizationId: 100})
	require.NoError(t, LOG_DB.Create(&rows).Error)

	models, err := GetUsageExportModels(UsageScope{}, window)
	require.NoError(t, err)
	assert.Len(t, models, 54)
	users, err := GetUsageExportGroups(UsageScope{}, window, UsageRankByUser)
	require.NoError(t, err)
	assert.Len(t, users, 54)
	organizations, err := GetUsageExportGroups(UsageScope{}, window, UsageRankByOrganization)
	require.NoError(t, err)
	assert.Len(t, organizations, 54)

	filtered := UsageScope{OrganizationId: 100, ModelName: "extra-00"}
	users, err = GetUsageExportGroups(filtered, window, UsageRankByUser)
	require.NoError(t, err)
	require.Equal(t, []UsageExportGroupRow{{Key: 100, Name: "z-renamed", ConsumeQuota: 5, Requests: 2, PromptTokens: 7, CompletionTokens: 9}}, users)
	organizations, err = GetUsageExportGroups(UsageScope{UserId: 100}, window, UsageRankByOrganization)
	require.NoError(t, err)
	require.Equal(t, []UsageExportGroupRow{{Key: 100, ConsumeQuota: 5, Requests: 2, PromptTokens: 7, CompletionTokens: 9}}, organizations)
	models, err = GetUsageExportModels(filtered, window)
	require.NoError(t, err)
	require.Equal(t, []UsageModelRow{{ModelName: "extra-00", ConsumeQuota: 5, Requests: 2, PromptTokens: 7, CompletionTokens: 9}}, models)

	rankedModels, err := GetUsageModels(UsageScope{}, window)
	require.NoError(t, err)
	assert.Len(t, rankedModels, 50)
	rankedUsers, err := GetUsageRanking(UsageScope{}, window, UsageRankByUser)
	require.NoError(t, err)
	assert.Len(t, rankedUsers, 50)
}

func TestUsageRecordsPageNewestFirstWithinScope(t *testing.T) {
	setupUsageReportTest(t)
	window := usageReportWindow(0)

	first, total, err := GetUsageRecords(UsageScope{OrganizationId: 1}, window, 1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(4), total, "consume rows of organization 1")
	require.Len(t, first, 2)
	assert.Equal(t, "r3", first[0].RequestId)
	assert.Equal(t, "r2", first[1].RequestId)

	second, _, err := GetUsageRecords(UsageScope{OrganizationId: 1}, window, 2, 2)
	require.NoError(t, err)
	require.Len(t, second, 2)
	assert.Equal(t, "r1", second[0].RequestId)
	assert.Equal(t, "r8", second[1].RequestId)
}

func TestUsageModelFilterNarrowsReportsAndKeepsModelChoicesComplete(t *testing.T) {
	setupUsageReportTest(t)
	window := usageReportWindow(0)
	filtered := UsageScope{OrganizationId: 1, ModelName: "m1"}

	// m1 in organization 1: 100+30+5. The m2 rows and the other organizations are excluded.
	summary, err := GetUsageSummary(filtered, window)
	require.NoError(t, err)
	assert.Equal(t, int64(135), summary.ConsumeQuota)
	assert.Equal(t, int64(3), summary.Requests)

	records, total, err := GetUsageRecords(filtered, window, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	for _, record := range records {
		assert.Equal(t, "m1", record.ModelName)
	}

	models, err := GetUsageModels(filtered, window)
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "m1", models[0].ModelName)

	// The choices list every model in the scope and window, whichever model is selected.
	names, err := GetUsageModelNames(filtered, window)
	require.NoError(t, err)
	assert.Equal(t, []string{"m1", "m2"}, names)

	platformNames, err := GetUsageModelNames(UsageScope{}, window)
	require.NoError(t, err)
	assert.Equal(t, []string{"m1", "m2", "m3"}, platformNames)

	unknown, err := GetUsageSummary(UsageScope{ModelName: "missing"}, window)
	require.NoError(t, err)
	assert.Equal(t, int64(0), unknown.Requests)
}

// openUsageReportLogDatabase opens the fixture database. With TEST_CLICKHOUSE_LOG_DSN set, the
// fixtures run on a ClickHouse log database, created and dropped by the test. Use a disposable
// loopback instance. Without it, the same fixtures run on the SQL test engines.
func openUsageReportLogDatabase(t *testing.T) (*gorm.DB, common.DatabaseType) {
	t.Helper()
	dsn := os.Getenv("TEST_CLICKHOUSE_LOG_DSN")
	if dsn == "" {
		db, dialectType := openOrganizationQuotaTestDatabase(t)
		require.NoError(t, db.AutoMigrate(&Log{}))
		return db, dialectType
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.True(t, net.ParseIP(parsed.Hostname()).IsLoopback(), "database tests only permit loopback instances")
	name := fmt.Sprintf("newapi_usage_%d", time.Now().UnixNano())
	admin, err := gorm.Open(clickhouse.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, admin.Exec("CREATE DATABASE "+name).Error)
	t.Cleanup(func() {
		_ = admin.Exec("DROP DATABASE IF EXISTS " + name).Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	parsed.Path = "/" + name
	t.Setenv("LOG_SQL_DSN", parsed.String())
	db, dialectType, err := chooseDB("LOG_SQL_DSN", true)
	require.NoError(t, err)
	previousLogDB, previousLog := LOG_DB, common.LogDatabaseType()
	LOG_DB = db
	common.SetLogDatabaseType(dialectType)
	t.Cleanup(func() {
		LOG_DB = previousLogDB
		common.SetLogDatabaseType(previousLog)
	})
	require.NoError(t, migrateLOGDB())
	return db, dialectType
}
