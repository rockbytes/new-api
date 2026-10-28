package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUsageExportJobMigratesAndTransitions runs on the dialect selected by
// TEST_ORGANIZATION_QUOTA_DIALECT. The migration runs twice, the way two startups would.
func TestUsageExportJobMigratesAndTransitions(t *testing.T) {
	db, dbType := openOrganizationQuotaTestDatabase(t)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	previousDB, previousLogDB := DB, LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(dbType, dbType)
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
	})

	require.NoError(t, db.AutoMigrate(&UsageExportJob{}))
	require.NoError(t, db.AutoMigrate(&UsageExportJob{}), "a second startup finds the table and changes nothing")

	const now int64 = 1_800_000_000
	job := &UsageExportJob{
		JobId: "migration-job", UserId: 7, View: "organization", OrganizationId: 3, TargetUserId: 0,
		StartTimestamp: now - 3600, EndTimestamp: now, TzOffsetMinutes: -480, Format: "xlsx", Kind: "workbook",
		WithBom: false, Status: UsageExportStatusQueued, CreatedTime: now,
	}
	require.NoError(t, CreateUsageExportJob(job))

	claimed, err := ClaimUsageExportJob(job.JobId, now+1)
	require.NoError(t, err)
	assert.True(t, claimed)
	claimedAgain, err := ClaimUsageExportJob(job.JobId, now+2)
	require.NoError(t, err)
	assert.False(t, claimedAgain, "a job is claimed once")

	require.NoError(t, FinishUsageExportJob(job.JobId, UsageExportStatusSucceeded, 12, "usage-workbook-migration-job.xlsx",
		"migration-job.xlsx", "", now+5, now+100))
	got, err := GetUsageExportJob(job.JobId)
	require.NoError(t, err)
	assert.Equal(t, UsageExportStatusSucceeded, got.Status)
	assert.Equal(t, int64(12), got.RowCount)
	assert.Equal(t, "migration-job.xlsx", got.StorageKey)
	assert.Equal(t, "xlsx", got.Format)
	assert.Equal(t, -480, got.TzOffsetMinutes)

	before, err := ListExpiredUsageExportJobs(now+99, 10)
	require.NoError(t, err)
	assert.Empty(t, before, "the file is not due for cleanup yet")
	due, err := ListExpiredUsageExportJobs(now+100, 10)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.NoError(t, MarkUsageExportJobExpired(job.JobId))
	got, err = GetUsageExportJob(job.JobId)
	require.NoError(t, err)
	assert.Equal(t, UsageExportStatusExpired, got.Status)

	stale := &UsageExportJob{JobId: "stale-job", UserId: 7, View: "self", Format: "csv", Kind: "records",
		Status: UsageExportStatusRunning, CreatedTime: now, StartedTime: now}
	require.NoError(t, CreateUsageExportJob(stale))
	failed, err := FailStaleUsageExportJobs(now+1, now+10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), failed)

	_, err = GetUsageExportJob("no-such-job")
	assert.ErrorIs(t, err, ErrUsageExportNotFound)

	listed, err := ListUsageExportJobs(7, 10)
	require.NoError(t, err)
	assert.Len(t, listed, 2)
}
