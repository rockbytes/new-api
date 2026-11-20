package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The override table must migrate twice without change, keep one row per
// subject through upserts, and delete one subject without touching the others.
// Run it on each supported database with TEST_ORGANIZATION_QUOTA_DIALECT and
// the matching TEST_<DIALECT>_DSN, the same way as the organization quota tests.
func TestRateLimitOverrideMigrationUpsertAndDelete(t *testing.T) {
	db, _ := openOrganizationQuotaTestDatabase(t)
	t.Cleanup(func() {
		// Close the connection before the test directory is removed; Windows
		// refuses to delete an open SQLite file.
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	require.NoError(t, DB.AutoMigrate(&RateLimitOverride{}))
	require.NoError(t, DB.AutoMigrate(&RateLimitOverride{}), "a second migration must change nothing")

	require.NoError(t, UpsertRateLimitOverride(&RateLimitOverride{
		SubjectType: RateLimitOverrideSubjectOrganization, SubjectId: 5, TotalCount: 10, UpdatedBy: 1, UpdatedTime: common.GetTimestamp(),
	}))
	require.NoError(t, UpsertRateLimitOverride(&RateLimitOverride{
		SubjectType: RateLimitOverrideSubjectOrganization, SubjectId: 5, TotalCount: 20, UpdatedBy: 2, UpdatedTime: common.GetTimestamp(),
	}))
	require.NoError(t, UpsertRateLimitOverride(&RateLimitOverride{
		SubjectType: RateLimitOverrideSubjectOrganization, SubjectId: 6, TotalCount: 30, UpdatedBy: 2, UpdatedTime: common.GetTimestamp(),
	}))

	overrides, err := ListRateLimitOverrides(RateLimitOverrideSubjectOrganization)
	require.NoError(t, err)
	require.Len(t, overrides, 2, "an upsert must replace the row, not add one")
	assert.Equal(t, 5, overrides[0].SubjectId)
	assert.Equal(t, 20, overrides[0].TotalCount)
	assert.Equal(t, 2, overrides[0].UpdatedBy)
	assert.Equal(t, 6, overrides[1].SubjectId)

	require.NoError(t, DeleteRateLimitOverride(RateLimitOverrideSubjectOrganization, 5))
	overrides, err = ListRateLimitOverrides(RateLimitOverrideSubjectOrganization)
	require.NoError(t, err)
	require.Len(t, overrides, 1)
	assert.Equal(t, 6, overrides[0].SubjectId)
}
