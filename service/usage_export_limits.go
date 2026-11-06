package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

// Errors returned when an export limit refuses a request. Controllers map them to HTTP status codes.
var (
	ErrUsageExportQueueRequired = errors.New("the export is too large to download directly")
	ErrUsageGenerateBusy        = errors.New("too many exports are being generated")
	ErrUsageDownloadBusy        = errors.New("too many downloads are running")
	ErrUsageExportUnavailable   = errors.New("export limits are unavailable")
)

// usageExportSettings are the export limits. They are read from the environment once, on first use.
type usageExportSettings struct {
	SyncRowLimit            int
	DownloadConcurrency     int
	UserDownloadConcurrency int
	GenerateConcurrency     int
	DownloadBytesPerSecond  int
}

var (
	usageExportSettingsOnce  sync.Once
	usageExportSettingsValue usageExportSettings
)

func usageExportLimits() *usageExportSettings {
	usageExportSettingsOnce.Do(func() {
		usageExportSettingsValue = usageExportSettings{
			SyncRowLimit:            common.GetEnvOrDefault("USAGE_EXPORT_SYNC_ROW_LIMIT", 5000),
			DownloadConcurrency:     common.GetEnvOrDefault("USAGE_EXPORT_DOWNLOAD_CONCURRENCY", 8),
			UserDownloadConcurrency: common.GetEnvOrDefault("USAGE_EXPORT_USER_DOWNLOAD_CONCURRENCY", 2),
			GenerateConcurrency:     common.GetEnvOrDefault("USAGE_EXPORT_GENERATE_CONCURRENCY", 2),
			DownloadBytesPerSecond:  common.GetEnvOrDefault("USAGE_EXPORT_DOWNLOAD_BYTES_PER_SECOND", 2*1024*1024),
		}
	})
	return &usageExportSettingsValue
}

const (
	usageLeaseTTL           = 60 * time.Second
	usageLeaseRenewInterval = 15 * time.Second
	usageLeaseKeyPrefix     = "usage_export:lease:"
	usageLeaseTimeout       = 5 * time.Second
)

// usageLeaseStore holds named pools of slots. A slot is a lease: it expires unless its holder
// renews it, so a process that stops without releasing does not hold its slot forever.
type usageLeaseStore interface {
	acquire(ctx context.Context, pool string, limit int, id string, now time.Time) (bool, error)
	renew(ctx context.Context, pool string, id string, now time.Time) error
	release(ctx context.Context, pool string, id string) error
}

// errUsageLeaseLost means the lease expired before it was renewed.
var errUsageLeaseLost = errors.New("usage export lease expired")

// memoryUsageLeaseStore counts slots in process. It is used when Redis is not enabled.
type memoryUsageLeaseStore struct {
	mu    sync.Mutex
	pools map[string]map[string]time.Time
}

func (s *memoryUsageLeaseStore) acquire(_ context.Context, pool string, limit int, id string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pools == nil {
		s.pools = map[string]map[string]time.Time{}
	}
	leases := s.pools[pool]
	if leases == nil {
		leases = map[string]time.Time{}
		s.pools[pool] = leases
	}
	for leaseId, expires := range leases {
		if !expires.After(now) {
			delete(leases, leaseId)
		}
	}
	if len(leases) >= limit {
		return false, nil
	}
	leases[id] = now.Add(usageLeaseTTL)
	return true, nil
}

func (s *memoryUsageLeaseStore) renew(_ context.Context, pool string, id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	expires, ok := s.pools[pool][id]
	if !ok || !expires.After(now) {
		return errUsageLeaseLost
	}
	s.pools[pool][id] = now.Add(usageLeaseTTL)
	return nil
}

func (s *memoryUsageLeaseStore) release(_ context.Context, pool string, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pools[pool], id)
	return nil
}

// usageLeaseAcquireScript removes expired leases, refuses when the pool is full, and otherwise
// records the lease with its expiry. Running it as one script keeps the check and the insert
// atomic across instances.
const usageLeaseAcquireScript = `
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[3]) then
  return 0
end
redis.call('ZADD', KEYS[1], ARGV[2], ARGV[4])
redis.call('PEXPIRE', KEYS[1], ARGV[5])
return 1
`

// usageLeaseRenewScript extends a lease only while it still exists.
const usageLeaseRenewScript = `
if redis.call('ZSCORE', KEYS[1], ARGV[2]) == false then
  return 0
end
redis.call('ZADD', KEYS[1], ARGV[1], ARGV[2])
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return 1
`

// redisUsageLeaseStore keeps the pools in Redis, so every instance counts the same slots.
// Times come from the caller, which keeps the script free of server-side clock reads.
type redisUsageLeaseStore struct {
	client *redis.Client
}

func usageLeaseKey(pool string) string {
	return usageLeaseKeyPrefix + pool
}

func (s redisUsageLeaseStore) acquire(ctx context.Context, pool string, limit int, id string, now time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, usageLeaseTimeout)
	defer cancel()
	nowMillis := now.UnixMilli()
	result, err := s.client.Eval(ctx, usageLeaseAcquireScript, []string{usageLeaseKey(pool)},
		nowMillis, now.Add(usageLeaseTTL).UnixMilli(), limit, id, usageLeaseTTL.Milliseconds()).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (s redisUsageLeaseStore) renew(ctx context.Context, pool string, id string, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, usageLeaseTimeout)
	defer cancel()
	result, err := s.client.Eval(ctx, usageLeaseRenewScript, []string{usageLeaseKey(pool)},
		now.Add(usageLeaseTTL).UnixMilli(), id, usageLeaseTTL.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return errUsageLeaseLost
	}
	return nil
}

func (s redisUsageLeaseStore) release(ctx context.Context, pool string, id string) error {
	ctx, cancel := context.WithTimeout(ctx, usageLeaseTimeout)
	defer cancel()
	return s.client.ZRem(ctx, usageLeaseKey(pool), id).Err()
}

var usageMemoryLeases = &memoryUsageLeaseStore{}

// usageLeaseStoreOverride replaces the store in tests.
var usageLeaseStoreOverride usageLeaseStore

// SetUsageLeaseStore replaces the lease store and returns the previous override, so tests can
// inject a failing store or a Redis store on a test server.
func SetUsageLeaseStore(store usageLeaseStore) usageLeaseStore {
	previous := usageLeaseStoreOverride
	usageLeaseStoreOverride = store
	return previous
}

// currentUsageLeaseStore chooses the store the way the rate limiters do: Redis when it is enabled,
// otherwise the in-process store.
func currentUsageLeaseStore() usageLeaseStore {
	if usageLeaseStoreOverride != nil {
		return usageLeaseStoreOverride
	}
	if common.RedisEnabled && common.RDB != nil {
		return redisUsageLeaseStore{client: common.RDB}
	}
	return usageMemoryLeases
}

// usageLeasePool names one pool of slots and its size.
type usageLeasePool struct {
	name  string
	limit int
}

// usageLease holds one slot in each of its pools. A heartbeat renews the slots until Release.
type usageLease struct {
	store   usageLeaseStore
	pools   []string
	id      string
	stop    chan struct{}
	release sync.Once
}

// acquireUsageLease takes a slot in every pool, or none. It returns false when any pool is full.
func acquireUsageLease(ctx context.Context, pools ...usageLeasePool) (*usageLease, bool, error) {
	store := currentUsageLeaseStore()
	id := common.GetUUID()
	now := time.Now()
	lease := &usageLease{store: store, id: id, stop: make(chan struct{})}
	for _, pool := range pools {
		acquired, err := store.acquire(ctx, pool.name, pool.limit, id, now)
		if err != nil {
			lease.release.Do(func() { lease.releaseSlots() })
			return nil, false, fmt.Errorf("%w: %v", ErrUsageExportUnavailable, err)
		}
		if !acquired {
			lease.release.Do(func() { lease.releaseSlots() })
			return nil, false, nil
		}
		lease.pools = append(lease.pools, pool.name)
	}
	go lease.heartbeat()
	return lease, true, nil
}

func (l *usageLease) heartbeat() {
	ticker := time.NewTicker(usageLeaseRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			for _, pool := range l.pools {
				ctx, cancel := context.WithTimeout(context.Background(), usageLeaseTimeout)
				err := l.store.renew(ctx, pool, l.id, time.Now())
				cancel()
				if err != nil {
					common.SysError(fmt.Sprintf("usage export lease %s in pool %s: %v", l.id, pool, err))
				}
			}
		}
	}
}

// Release stops the heartbeat and frees every slot. It is safe to call more than once.
func (l *usageLease) Release() {
	l.release.Do(func() {
		close(l.stop)
		l.releaseSlots()
	})
}

func (l *usageLease) releaseSlots() {
	for _, pool := range l.pools {
		ctx, cancel := context.WithTimeout(context.Background(), usageLeaseTimeout)
		if err := l.store.release(ctx, pool, l.id); err != nil {
			common.SysError(fmt.Sprintf("usage export lease %s release from pool %s: %v", l.id, pool, err))
		}
		cancel()
	}
}

// UsageDownloadLease is a held download slot, released when the response finishes.
type UsageDownloadLease struct {
	lease *usageLease
}

// Release frees the download slots.
func (l *UsageDownloadLease) Release() {
	if l != nil && l.lease != nil {
		l.lease.Release()
	}
}

// AcquireUsageDownload takes a global download slot and a slot for the user. A user who already
// holds their share is refused even when global slots are free.
func AcquireUsageDownload(ctx context.Context, userId int) (*UsageDownloadLease, error) {
	settings := usageExportLimits()
	lease, ok, err := acquireUsageLease(ctx,
		usageLeasePool{name: "download", limit: settings.DownloadConcurrency},
		usageLeasePool{name: fmt.Sprintf("download:user:%d", userId), limit: settings.UserDownloadConcurrency},
	)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrUsageDownloadBusy
	}
	return &UsageDownloadLease{lease: lease}, nil
}

// usageGenerationPool is the pool shared by direct exports and queued jobs.
const usageGenerationPool = "generate"

// acquireUsageGeneration takes one generation slot. It returns false when the pool is full.
func acquireUsageGeneration(ctx context.Context) (*usageLease, bool, error) {
	return acquireUsageLease(ctx, usageLeasePool{name: usageGenerationPool, limit: usageExportLimits().GenerateConcurrency})
}
