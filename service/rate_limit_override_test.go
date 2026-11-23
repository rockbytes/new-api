package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A user's effective limits are its override, with each count the override leaves at 0
// taken from the global default, and that default is off while the global limit is off.
func TestEffectiveUserRateLimitsResolveOverridesAgainstTheGlobalDefault(t *testing.T) {
	previousEnabled := setting.ModelRequestRateLimitEnabled
	previousCount, previousSuccess := setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = previousEnabled
		setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = previousCount, previousSuccess
		ReplaceUserRateLimitOverrides(nil)
	})
	setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = 100, 20
	ReplaceUserRateLimitOverrides(map[int]UserRateLimitOverride{
		1: {TotalCount: 5},
		2: {SuccessCount: 3},
		3: {},
	})

	cases := []struct {
		name        string
		enabled     bool
		userId      int
		wantTotal   int
		wantSuccess int
	}{
		{"global off without an override is off", false, 9, 0, 0},
		{"global off with a total override sets only the total", false, 1, 5, 0},
		{"global off with a success override sets only the success count", false, 2, 0, 3},
		{"global on without an override uses the global counts", true, 9, 100, 20},
		{"global on with a total override keeps the global success count", true, 1, 5, 20},
		{"global on with a success override keeps the global total", true, 2, 100, 3},
		{"an override with no positive count is dropped", true, 3, 100, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setting.ModelRequestRateLimitEnabled = tc.enabled
			total, success := EffectiveUserRateLimits(tc.userId)
			assert.Equal(t, tc.wantTotal, total)
			assert.Equal(t, tc.wantSuccess, success)
		})
	}
}

// A refresh loads the user overrides with both counts and keeps the organization
// overrides separate from them.
func TestRefreshRateLimitOverridesLoadsUserAndOrganizationRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/overrides.db?_pragma=busy_timeout(30000)&_txlock=immediate"), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		ReplaceUserRateLimitOverrides(nil)
		ReplaceOrganizationTotalRateLimits(nil)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	require.NoError(t, db.AutoMigrate(&model.RateLimitOverride{}))

	require.NoError(t, model.UpsertRateLimitOverride(&model.RateLimitOverride{
		SubjectType: model.RateLimitOverrideSubjectUser, SubjectId: 4, TotalCount: 6, SuccessCount: 2,
	}))
	require.NoError(t, model.UpsertRateLimitOverride(&model.RateLimitOverride{
		SubjectType: model.RateLimitOverrideSubjectOrganization, SubjectId: 8, TotalCount: 50,
	}))
	require.NoError(t, RefreshRateLimitOverrides())

	override, ok := UserRateLimitOverrideOf(4)
	require.True(t, ok)
	assert.Equal(t, UserRateLimitOverride{TotalCount: 6, SuccessCount: 2}, override)
	assert.Equal(t, 50, OrganizationTotalRateLimit(8))
	_, ok = UserRateLimitOverrideOf(8)
	assert.False(t, ok, "an organization row must not appear among the user overrides")
}
