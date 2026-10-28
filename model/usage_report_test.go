package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// usageReportBase is the UTC midnight of a fixed day, so every test row has a known day.
const usageReportBase int64 = 1799971200

// setupUsageReportTest opens an isolated database and makes it the log database. The
// fixtures cover two organizations, personal billing, refunds, and a non-usage log type.
func setupUsageReportTest(t *testing.T) {
	t.Helper()
	db, dialectType := openOrganizationQuotaTestDatabase(t)
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
	require.NoError(t, db.AutoMigrate(&Log{}))

	rows := []Log{
		// Organization 1, user 10: two consume rows and one refund on the first day.
		{UserId: 10, Username: "alice", Type: LogTypeConsume, ModelName: "m1", Quota: 100, PromptTokens: 10, CompletionTokens: 5, CreatedAt: usageReportBase + 3600, BillingOrganizationId: 1, RequestId: "r1", ChannelId: 7, Ip: "10.0.0.1"},
		{UserId: 10, Username: "alice", Type: LogTypeConsume, ModelName: "m2", Quota: 50, CreatedAt: usageReportBase + 7200, BillingOrganizationId: 1, RequestId: "r2"},
		{UserId: 10, Username: "alice", Type: LogTypeRefund, ModelName: "m1", Quota: 20, CreatedAt: usageReportBase + 3600, BillingOrganizationId: 1, RequestId: "r1"},
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

func TestUsageSummaryUsesSettledNetBasisForEachScope(t *testing.T) {
	setupUsageReportTest(t)
	window := usageReportWindow(0)

	// Consume 100+50+30+5 minus the 20 refund for organization 1.
	organization, err := GetUsageSummary(UsageScope{OrganizationId: 1}, window)
	require.NoError(t, err)
	assert.Equal(t, int64(165), organization.NetQuota)
	assert.Equal(t, int64(185), organization.ConsumeQuota)
	assert.Equal(t, int64(20), organization.RefundQuota)
	assert.Equal(t, int64(4), organization.Requests)
	assert.Equal(t, int64(10), organization.PromptTokens)

	member, err := GetUsageSummary(UsageScope{OrganizationId: 1, UserId: 10}, window)
	require.NoError(t, err)
	assert.Equal(t, int64(135), member.NetQuota)
	assert.Equal(t, int64(3), member.Requests, "user 10 has three consume rows in organization 1")

	platform, err := GetUsageSummary(UsageScope{}, window)
	require.NoError(t, err)
	assert.Equal(t, int64(275), platform.NetQuota, "all payers: 295 consumed minus the 20 refund, excluding the login log")
	assert.Equal(t, int64(6), platform.Requests)
}

func TestUsageTrendBucketsByLocalDay(t *testing.T) {
	setupUsageReportTest(t)

	utc, err := GetUsageTrend(UsageScope{OrganizationId: 1}, usageReportWindow(0))
	require.NoError(t, err)
	require.Len(t, utc, 3)
	assert.Equal(t, usageReportBase-86400, utc[0].DayStart)
	assert.Equal(t, int64(5), utc[0].NetQuota)
	assert.Equal(t, usageReportBase, utc[1].DayStart)
	assert.Equal(t, int64(130), utc[1].NetQuota)
	assert.Equal(t, int64(2), utc[1].Requests)
	assert.Equal(t, usageReportBase+86400, utc[2].DayStart)
	assert.Equal(t, int64(30), utc[2].NetQuota)

	// At UTC+8 the 23:00 UTC row belongs to the local day that starts 8 hours before the
	// UTC midnight, so the first two buckets move.
	local, err := GetUsageTrend(UsageScope{OrganizationId: 1}, usageReportWindow(8*3600))
	require.NoError(t, err)
	require.Len(t, local, 2)
	assert.Equal(t, usageReportBase-8*3600, local[0].DayStart)
	assert.Equal(t, int64(135), local[0].NetQuota, "the 23:00 UTC row joins the first local day")
	assert.Equal(t, usageReportBase+16*3600, local[1].DayStart)
	assert.Equal(t, int64(30), local[1].NetQuota)
}

func TestUsageModelsAndRankingsRespectScope(t *testing.T) {
	setupUsageReportTest(t)
	window := usageReportWindow(0)

	models, err := GetUsageModels(UsageScope{OrganizationId: 1}, window)
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "m1", models[0].ModelName)
	assert.Equal(t, int64(115), models[0].NetQuota)
	assert.Equal(t, "m2", models[1].ModelName)
	assert.Equal(t, int64(50), models[1].NetQuota)

	byUser, err := GetUsageRanking(UsageScope{OrganizationId: 1}, window, UsageRankByUser)
	require.NoError(t, err)
	require.Len(t, byUser, 2)
	assert.Equal(t, int64(10), byUser[0].Key)
	assert.Equal(t, int64(135), byUser[0].NetQuota)
	assert.Equal(t, int64(11), byUser[1].Key)

	// Personal billing ranks as organization 0, next to the real organizations.
	byOrganization, err := GetUsageRanking(UsageScope{}, window, UsageRankByOrganization)
	require.NoError(t, err)
	require.Len(t, byOrganization, 3)
	assert.Equal(t, int64(1), byOrganization[0].Key)
	assert.Equal(t, int64(165), byOrganization[0].NetQuota)
	assert.Equal(t, int64(0), byOrganization[1].Key)
	assert.Equal(t, int64(70), byOrganization[1].NetQuota)
}

func TestUsageRecordsPageNewestFirstWithinScope(t *testing.T) {
	setupUsageReportTest(t)
	window := usageReportWindow(0)

	first, total, err := GetUsageRecords(UsageScope{OrganizationId: 1}, window, 1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total, "consume and refund rows of organization 1")
	require.Len(t, first, 2)
	assert.Equal(t, "r3", first[0].RequestId)
	assert.Equal(t, "r2", first[1].RequestId)

	second, _, err := GetUsageRecords(UsageScope{OrganizationId: 1}, window, 2, 2)
	require.NoError(t, err)
	require.Len(t, second, 2)
	assert.Equal(t, "r1", second[1].RequestId, "the refund and the consume share a timestamp; ties break by id")
}
