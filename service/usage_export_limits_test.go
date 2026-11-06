package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withUsageExportLimits sets the export limits for one test and restores them afterwards.
func withUsageExportLimits(t *testing.T, change func(*usageExportSettings)) {
	t.Helper()
	settings := usageExportLimits()
	previous := *settings
	change(settings)
	t.Cleanup(func() { *settings = previous })
}

// leaseStoreCases runs the same lease rules against every store implementation.
func leaseStoreCases(t *testing.T) map[string]usageLeaseStore {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return map[string]usageLeaseStore{
		"memory": &memoryUsageLeaseStore{},
		"redis":  redisUsageLeaseStore{client: client},
	}
}

func TestUsageLeasesCapEachPoolAndFreeOnRelease(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	for name, store := range leaseStoreCases(t) {
		t.Run(name, func(t *testing.T) {
			for _, id := range []string{"a", "b"} {
				ok, err := store.acquire(ctx, "pool", 2, id, now)
				require.NoError(t, err)
				assert.True(t, ok)
			}
			ok, err := store.acquire(ctx, "pool", 2, "c", now)
			require.NoError(t, err)
			assert.False(t, ok, "a full pool refuses the third slot")

			require.NoError(t, store.release(ctx, "pool", "a"))
			ok, err = store.acquire(ctx, "pool", 2, "c", now)
			require.NoError(t, err)
			assert.True(t, ok, "a released slot is available again")

			other, err := store.acquire(ctx, "other-pool", 1, "d", now)
			require.NoError(t, err)
			assert.True(t, other, "pools are counted separately")
		})
	}
}

func TestUsageLeasesExpireWithoutRenewal(t *testing.T) {
	ctx := context.Background()
	start := time.Unix(1_800_000_000, 0)
	afterTTL := start.Add(usageLeaseTTL + time.Second)
	for name, store := range leaseStoreCases(t) {
		t.Run(name, func(t *testing.T) {
			ok, err := store.acquire(ctx, "pool", 1, "crashed", start)
			require.NoError(t, err)
			require.True(t, ok)

			// A holder that stops renewing loses its slot once the lease expires.
			ok, err = store.acquire(ctx, "pool", 1, "next", start.Add(usageLeaseTTL/2))
			require.NoError(t, err)
			assert.False(t, ok, "the slot is still held before the lease expires")
			ok, err = store.acquire(ctx, "pool", 1, "next", afterTTL)
			require.NoError(t, err)
			assert.True(t, ok, "the expired lease no longer holds the slot")

			// Renewal keeps a live holder's lease.
			renewedAt := afterTTL.Add(usageLeaseTTL / 2)
			require.NoError(t, store.renew(ctx, "pool", "next", renewedAt))
			ok, err = store.acquire(ctx, "pool", 1, "third", renewedAt.Add(usageLeaseTTL/2))
			require.NoError(t, err)
			assert.False(t, ok, "a renewed lease holds the slot past its original expiry")

			require.ErrorIs(t, store.renew(ctx, "pool", "gone", renewedAt), errUsageLeaseLost)
		})
	}
}

func TestUsageDownloadSlotsRefuseGlobalAndPerUserOverflow(t *testing.T) {
	store := &memoryUsageLeaseStore{}
	previous := SetUsageLeaseStore(store)
	t.Cleanup(func() { SetUsageLeaseStore(previous) })
	withUsageExportLimits(t, func(s *usageExportSettings) {
		s.DownloadConcurrency = 3
		s.UserDownloadConcurrency = 1
	})
	ctx := context.Background()

	first, err := AcquireUsageDownload(ctx, 1)
	require.NoError(t, err)
	_, err = AcquireUsageDownload(ctx, 1)
	assert.ErrorIs(t, err, ErrUsageDownloadBusy, "a user over their own share is refused while global slots are free")

	second, err := AcquireUsageDownload(ctx, 2)
	require.NoError(t, err)
	_, err = AcquireUsageDownload(ctx, 3)
	require.NoError(t, err)
	_, err = AcquireUsageDownload(ctx, 4)
	assert.ErrorIs(t, err, ErrUsageDownloadBusy, "the global limit refuses a fourth download")

	first.Release()
	_, err = AcquireUsageDownload(ctx, 1)
	assert.NoError(t, err, "releasing a download frees its slot for the same user")
	second.Release()
}

func TestUsageDirectExportWaitsForAFreeGenerationSlot(t *testing.T) {
	setupUsageExportServiceTest(t)
	store := &memoryUsageLeaseStore{}
	previous := SetUsageLeaseStore(store)
	t.Cleanup(func() { SetUsageLeaseStore(previous) })
	withUsageExportLimits(t, func(s *usageExportSettings) { s.GenerateConcurrency = 1 })
	start, end := usageExportTestWindow()
	request := UsageExportRequest{View: UsageViewPlatform, StartTimestamp: start, EndTimestamp: end, Format: UsageExportFormatCSV, Kind: UsageExportKindRecords}

	held, ok, err := acquireUsageGeneration(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	_, err = PrepareDirectUsageExport(usageExportRoot, common.RoleRootUser, request)
	assert.ErrorIs(t, err, ErrUsageGenerateBusy)

	held.Release()
	direct, err := PrepareDirectUsageExport(usageExportRoot, common.RoleRootUser, request)
	require.NoError(t, err)
	direct.Close()
}

func TestUsageQueuedJobFailsWhenNoSlotPicksItUpInTime(t *testing.T) {
	setupUsageExportServiceTest(t)
	store := &memoryUsageLeaseStore{}
	previous := SetUsageLeaseStore(store)
	t.Cleanup(func() { SetUsageLeaseStore(previous) })
	withUsageExportLimits(t, func(s *usageExportSettings) { s.GenerateConcurrency = 1 })
	held, ok, err := acquireUsageGeneration(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	defer held.Release()

	// The job was created before the queue timeout, so the first poll already gives up on it.
	job := &model.UsageExportJob{JobId: common.GetUUID(), UserId: usageExportRoot, View: string(UsageViewPlatform),
		Format: UsageExportFormatCSV, Kind: UsageExportKindRecords,
		Status: model.UsageExportStatusQueued, CreatedTime: common.GetTimestamp() - usageExportQueueTimeoutSeconds}
	require.NoError(t, model.CreateUsageExportJob(job))

	err = RunUsageExportJob(job.JobId)
	assert.ErrorIs(t, err, ErrUsageGenerateBusy)
	stored, err := model.GetUsageExportJob(job.JobId)
	require.NoError(t, err)
	assert.Equal(t, model.UsageExportStatusFailed, stored.Status)
	assert.Equal(t, usageExportErrorQueueTimeout, stored.ErrorCode)
}

func TestUsageLimitsRefuseWhenRedisIsUnavailable(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	require.NoError(t, client.Close(), "the client is closed, so every command fails")
	previous := SetUsageLeaseStore(redisUsageLeaseStore{client: client})
	t.Cleanup(func() { SetUsageLeaseStore(previous) })

	_, err := AcquireUsageDownload(context.Background(), 1)
	assert.ErrorIs(t, err, ErrUsageExportUnavailable)
	_, ok, err := acquireUsageGeneration(context.Background())
	assert.ErrorIs(t, err, ErrUsageExportUnavailable)
	assert.False(t, ok)
}

// The throttle is checked with a fake clock, so the sleeps it asks for are exact.
func TestUsageThrottleSleepsOnlyWhileTheBudgetIsSpent(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	var slept []time.Duration
	sleep := func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	var out bytes.Buffer
	// One second of burst at 1000 bytes per second: the first 1000 bytes are free, the next 2000
	// bytes must wait two seconds.
	throttle := newUsageThrottle(context.Background(), &out, 1000, func() time.Time { return now }, sleep)
	payload := bytes.Repeat([]byte("x"), 3000)
	n, err := throttle.Write(payload)
	require.NoError(t, err)
	assert.Equal(t, len(payload), n)
	assert.Equal(t, payload, out.Bytes(), "the bytes pass through unchanged")
	var total time.Duration
	for _, d := range slept {
		total += d
	}
	assert.Equal(t, 2*time.Second, total)
}

func TestUsageThrottleStopsWhenTheDownloadIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	throttle := newUsageThrottle(ctx, io.Discard, 10, time.Now, sleepUsageThrottle)
	_, err := throttle.Write(make([]byte, 100))
	assert.True(t, errors.Is(err, context.Canceled))
}

// A export larger than the sync row limit is queued: the estimate says so, and a direct download
// is refused.
func TestUsageLargeExportIsQueuedNotDirect(t *testing.T) {
	setupUsageExportServiceTest(t)
	withUsageExportLimits(t, func(s *usageExportSettings) { s.SyncRowLimit = 1 })
	start, end := usageExportTestWindow()
	request := UsageExportRequest{View: UsageViewPlatform, StartTimestamp: start, EndTimestamp: end, Format: UsageExportFormatCSV, Kind: UsageExportKindRecords}

	estimate, err := EstimateUsageExport(usageExportRoot, common.RoleRootUser, request)
	require.NoError(t, err)
	assert.Equal(t, UsageExportDeliveryQueued, estimate.Delivery)
	assert.Greater(t, estimate.RowCount, int64(1))

	_, err = PrepareDirectUsageExport(usageExportRoot, common.RoleRootUser, request)
	assert.ErrorIs(t, err, ErrUsageExportQueueRequired)
}

// Finished records are purged a week after they were created. Succeeded jobs are kept until
// their file is removed, and queued jobs that nothing picked up are failed.
func TestUsageExportRecordsArePurgedAfterRetention(t *testing.T) {
	setupUsageExportServiceTest(t)
	now := common.GetTimestamp()
	old := now - usageExportRecordRetentionSeconds - 60
	jobs := []model.UsageExportJob{
		{JobId: "old-expired", UserId: usageExportRoot, Status: model.UsageExportStatusExpired, CreatedTime: old},
		{JobId: "old-failed", UserId: usageExportRoot, Status: model.UsageExportStatusFailed, CreatedTime: old},
		{JobId: "old-succeeded", UserId: usageExportRoot, Status: model.UsageExportStatusSucceeded, CreatedTime: old, ExpiresTime: now + 60},
		{JobId: "recent-expired", UserId: usageExportRoot, Status: model.UsageExportStatusExpired, CreatedTime: now},
		{JobId: "abandoned", UserId: usageExportRoot, Status: model.UsageExportStatusQueued, CreatedTime: now - usageExportQueueTimeoutSeconds - 1},
	}
	require.NoError(t, model.DB.Create(&jobs).Error)

	failed, err := model.FailAbandonedQueuedUsageExportJobs(now-usageExportQueueTimeoutSeconds, now, usageExportErrorQueueTimeout)
	require.NoError(t, err)
	assert.Equal(t, int64(1), failed)

	purged, err := model.DeleteUsageExportJobsCreatedBefore(now - usageExportRecordRetentionSeconds)
	require.NoError(t, err)
	assert.Equal(t, int64(2), purged, "only the old failed and expired records are removed")

	var remaining []string
	require.NoError(t, model.DB.Model(&model.UsageExportJob{}).Order("job_id").Pluck("job_id", &remaining).Error)
	assert.Equal(t, []string{"abandoned", "old-succeeded", "recent-expired"}, remaining)
}
