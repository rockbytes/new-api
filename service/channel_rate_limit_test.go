package service

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// budgetTestChannel builds an in-memory channel. A zero requests value leaves
// the channel without a budget.
func budgetTestChannel(id int, priority int64, requests, windowSeconds int) *model.Channel {
	channel := &model.Channel{Id: id, Type: constant.ChannelTypeOpenAI, Priority: common.GetPointer(priority)}
	if requests > 0 {
		channel.SetSetting(kitdto.ChannelSettings{RateLimit: &kitdto.ChannelRateLimit{
			Enabled:         true,
			Requests:        requests,
			DurationSeconds: windowSeconds,
		}})
	}
	return channel
}

// budgetTestPicker returns the first candidate that satisfies the request's
// filters, in candidate order. It applies the real filter code, so the budget
// exclusion is honoured exactly as the channel cache honours it.
func budgetTestPicker(c *gin.Context, candidates ...*model.Channel) ChannelPicker {
	return func(*RetryParam) (*model.Channel, string, *ChannelSelectError) {
		filters := GetChannelConstraints(c).Filters
		for _, channel := range candidates {
			if ok, _ := model.ChannelSatisfiesFilters(channel, "budget-model", filters); ok {
				return channel, "default", nil
			}
		}
		return nil, "default", &ChannelSelectError{NoAvailableChannel: true}
	}
}

func newBudgetTestContext(t *testing.T) (*gin.Context, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previous := common.RedisEnabled
	common.RedisEnabled = false
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, func() { common.RedisEnabled = previous }
}

func TestSelectChannelWithBudgetReroutesWithinTier(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()

	first := budgetTestChannel(9101, 10, 1, 60)
	second := budgetTestChannel(9102, 10, 0, 0)
	pick := budgetTestPicker(c, first, second)
	retry := &RetryParam{Ctx: c, Retry: common.GetPointer(0)}
	baseFilters := len(GetChannelConstraints(c).Filters)

	selected, _, selectErr := SelectChannelWithBudget(c, retry, pick)
	require.Nil(t, selectErr)
	require.Equal(t, first.Id, selected.Id, "the first request takes the budget of the first channel")

	selected, _, selectErr = SelectChannelWithBudget(c, retry, pick)
	require.Nil(t, selectErr)
	assert.Equal(t, second.Id, selected.Id, "an over-budget channel is replaced by a same-tier channel")
	assert.Equal(t, 0, retry.GetRetry(), "rerouting must not consume the retry index")
	assert.Len(t, GetChannelConstraints(c).Filters, baseFilters, "the exclusion must not outlive the selection")
}

func TestSelectChannelWithBudgetDoesNotFallToLowerTier(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()

	upper := budgetTestChannel(9201, 10, 1, 60)
	lower := budgetTestChannel(9202, 5, 0, 0)
	pick := budgetTestPicker(c, upper, lower)
	retry := &RetryParam{Ctx: c, Retry: common.GetPointer(0)}

	_, _, selectErr := SelectChannelWithBudget(c, retry, pick)
	require.Nil(t, selectErr)

	selected, _, selectErr := SelectChannelWithBudget(c, retry, pick)
	require.Nil(t, selected)
	require.NotNil(t, selectErr)
	assert.Equal(t, http.StatusTooManyRequests, selectErr.StatusCode)
	assert.Equal(t, channelRateLimitErrorCode, selectErr.Code)
	assert.Equal(t, int64(60), selectErr.RetryAfterSeconds, "one request refills in window/requests seconds")
}

func TestSelectChannelWithBudgetNeverReplacesPinnedChannel(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()

	pinned := budgetTestChannel(9301, 10, 1, 60)
	other := budgetTestChannel(9302, 10, 0, 0)
	GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: pinned.Id, Source: dto.PinSourceToken, Rank: dto.PinRankToken})
	pick := budgetTestPicker(c, pinned, other)
	retry := &RetryParam{Ctx: c, Retry: common.GetPointer(0)}

	_, _, selectErr := SelectChannelWithBudget(c, retry, pick)
	require.Nil(t, selectErr)

	selected, _, selectErr := SelectChannelWithBudget(c, retry, pick)
	require.Nil(t, selected, "a pinned channel must not be swapped for another channel")
	require.NotNil(t, selectErr)
	assert.Equal(t, http.StatusTooManyRequests, selectErr.StatusCode)
}

func TestChannelBudgetAdmitsConfiguredRequestsPerWindow(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()

	channel := budgetTestChannel(9401, 1, 2, 60)

	allowed := AcquireChannelBudget(c, channel) == nil
	assert.True(t, allowed)
	allowed = AcquireChannelBudget(c, channel) == nil
	assert.True(t, allowed)

	refusal := AcquireChannelBudget(c, channel)
	require.NotNil(t, refusal)
	assert.Equal(t, int64(30), refusal.retryAfter, "two requests per 60s refill one request in 30s")
	assert.Equal(t, RateLimitScopeChannel, refusal.scope)

	unlimited := budgetTestChannel(9402, 1, 0, 0)
	for range 5 {
		allowed := AcquireChannelBudget(c, unlimited) == nil
		require.True(t, allowed, "a channel without a budget is never refused")
	}
}

// ReleaseChannelBudget returns the admission of a request that never reached the
// upstream, once. A second release has nothing left to return.
func TestReleaseChannelBudgetReturnsAdmissionOnce(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()

	channel := budgetTestChannel(9403, 1, 1, 60)
	allowed := AcquireChannelBudget(c, channel) == nil
	require.True(t, allowed)
	allowed = AcquireChannelBudget(c, channel) == nil
	require.False(t, allowed)

	ReleaseChannelBudget(c)
	ReleaseChannelBudget(c)
	allowed = AcquireChannelBudget(c, channel) == nil
	assert.True(t, allowed, "the released admission is available to the next request")
	allowed = AcquireChannelBudget(c, channel) == nil
	assert.False(t, allowed, "the second release must not return a second admission")
}

// With Redis enabled but unreachable, the budget admits every request instead of
// failing it: a Redis outage must not take a healthy channel out of service. The
// release must also return without panicking when its script cannot reach Redis.
func TestChannelBudgetFailsOpenWhenRedisIsUnreachable(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()
	client := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
		MaxRetries:   0,
	})
	t.Cleanup(func() { _ = client.Close() })
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() { common.RDB = previousRDB })

	channel := budgetTestChannel(9404, 1, 1, 60)
	for range 3 {
		require.Nil(t, AcquireChannelBudget(c, channel), "an unreachable Redis must not refuse the request")
	}
	ReleaseChannelBudget(c)
}

// A refused channel budget reports its request count as the limit and the Retry-After
// hint as the reset, so the 429 headers describe the layer that refused.
func TestSelectChannelWithBudgetRefusalReportsItsLimit(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()
	channel := budgetTestChannel(9405, 1, 2, 60)
	picker := budgetTestPicker(c, channel)
	retry := &RetryParam{Ctx: c, ModelName: "budget-model", Retry: common.GetPointer(0)}

	for range 2 {
		_, _, selectErr := SelectChannelWithBudget(c, retry, picker)
		require.Nil(t, selectErr)
	}
	_, _, selectErr := SelectChannelWithBudget(c, retry, picker)
	require.NotNil(t, selectErr)
	assert.Equal(t, int64(2), selectErr.RateLimitRequests)
	assert.Equal(t, int64(30), selectErr.RetryAfterSeconds)
}

// An admitted channel request reports the channel's limit and the requests it has left.
func TestChannelBudgetAdmissionReportsTheRemainingRequests(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()
	channel := budgetTestChannel(9406, 1, 3, 60)

	allowed := AcquireChannelBudget(c, channel) == nil
	require.True(t, allowed)
	assert.Equal(t, "3", c.Writer.Header().Get("x-ratelimit-limit"))
	assert.Equal(t, "2", c.Writer.Header().Get("x-ratelimit-remaining"))
}

// TestChannelBudgetRedisPathAgainstRealRedis runs the token-bucket script on a
// real Redis server. It is skipped unless NEW_API_TEST_REDIS_ADDR names one.
// The test uses database 15 and flushes only that database.
func TestChannelBudgetRedisPathAgainstRealRedis(t *testing.T) {
	addr := os.Getenv("NEW_API_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set NEW_API_TEST_REDIS_ADDR (for example 127.0.0.1:6379) to run the Redis budget path")
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.FlushDB(ctx).Err())

	c, restore := newBudgetTestContext(t)
	defer restore()
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() { common.RDB = previousRDB })

	channel := budgetTestChannel(9701, 1, 2, 60)
	for i := range 2 {
		allowed := AcquireChannelBudget(c, channel) == nil
		require.True(t, allowed, "request %d is within the two-per-window budget", i+1)
	}
	refusal := AcquireChannelBudget(c, channel)
	require.NotNil(t, refusal, "the third request in the window must be refused")
	assert.Equal(t, int64(30), refusal.retryAfter)

	ttl, err := client.TTL(ctx, "rateLimit:channel:9701").Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0), "the bucket key must carry an expiry")
	assert.LessOrEqual(t, ttl, 120*time.Second, "expiry is the refill time plus the idle margin")
}

func TestChannelBudgetFilterExcludesOnlyListedChannels(t *testing.T) {
	excluded := budgetTestChannel(9501, 1, 1, 60)
	kept := budgetTestChannel(9502, 1, 0, 0)
	filters := []dto.ChannelFilter{{Kind: dto.FilterChannelRateLimited, ExcludeChannelIDs: []int{excluded.Id}}}

	ok, kind := model.ChannelSatisfiesFilters(excluded, "budget-model", filters)
	assert.False(t, ok)
	assert.Equal(t, dto.FilterChannelRateLimited, kind)

	ok, _ = model.ChannelSatisfiesFilters(kept, "budget-model", filters)
	assert.True(t, ok)
}

func TestChannelRateLimitSettingValidation(t *testing.T) {
	tests := []struct {
		name    string
		budget  *kitdto.ChannelRateLimit
		wantErr bool
	}{
		{name: "absent budget is unlimited", budget: nil},
		{name: "disabled budget ignores zero values", budget: &kitdto.ChannelRateLimit{Enabled: false}},
		{name: "valid budget", budget: &kitdto.ChannelRateLimit{Enabled: true, Requests: 60, DurationSeconds: 60}},
		{name: "zero requests", budget: &kitdto.ChannelRateLimit{Enabled: true, Requests: 0, DurationSeconds: 60}, wantErr: true},
		{name: "requests overflow the window product", budget: &kitdto.ChannelRateLimit{Enabled: true, Requests: math.MaxInt64/kitdto.MaxChannelRateLimitWindowSeconds + 1, DurationSeconds: 60}, wantErr: true},
		{name: "zero window", budget: &kitdto.ChannelRateLimit{Enabled: true, Requests: 1, DurationSeconds: 0}, wantErr: true},
		{name: "window longer than one day", budget: &kitdto.ChannelRateLimit{Enabled: true, Requests: 1, DurationSeconds: kitdto.MaxChannelRateLimitWindowSeconds + 1}, wantErr: true},
		{name: "organization share within range", budget: &kitdto.ChannelRateLimit{Enabled: true, Requests: 60, DurationSeconds: 60, OrgShares: map[int]int{7: 50, 8: 100}}},
		{name: "organization share above 100 percent", budget: &kitdto.ChannelRateLimit{Enabled: true, Requests: 60, DurationSeconds: 60, OrgShares: map[int]int{7: 101}}, wantErr: true},
		{name: "zero organization share", budget: &kitdto.ChannelRateLimit{Enabled: true, Requests: 60, DurationSeconds: 60, OrgShares: map[int]int{7: 0}}, wantErr: true},
		{name: "organization share without an organization id", budget: &kitdto.ChannelRateLimit{Enabled: true, Requests: 60, DurationSeconds: 60, OrgShares: map[int]int{0: 50}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &model.Channel{Id: 9601}
			channel.SetSetting(kitdto.ChannelSettings{RateLimit: tt.budget})
			err := channel.ValidateSettings()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// budgetTestChannelWithShares builds an in-memory channel whose budget caps the given
// organizations at percentages of it. The setting is stored as JSON, so the tests also
// cover the round trip the server performs.
func budgetTestChannelWithShares(id int, requests, windowSeconds int, shares map[int]int) *model.Channel {
	channel := &model.Channel{Id: id, Type: constant.ChannelTypeOpenAI, Priority: common.GetPointer(int64(1))}
	channel.SetSetting(kitdto.ChannelSettings{RateLimit: &kitdto.ChannelRateLimit{
		Enabled:         true,
		Requests:        requests,
		DurationSeconds: windowSeconds,
		OrgShares:       shares,
	}})
	return channel
}

// An organization capped at a share of a channel budget is refused once its share is
// used, with the share named as the refusing scope. The refusal does not use the channel
// budget, so other organizations keep the rest of it.
func TestChannelOrgShareCapsOneOrganizationWithoutUsingTheChannelBudget(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()
	channel := budgetTestChannelWithShares(9801, 4, 60, map[int]int{7: 50})

	admit := func(organizationId int) *channelBudgetRefusal {
		common.SetContextKey(c, constant.ContextKeyTokenBillingOrganizationId, organizationId)
		return AcquireChannelBudget(c, channel)
	}
	require.Nil(t, admit(7), "the first request of the capped organization is within its share")
	require.Nil(t, admit(7), "the second request uses the whole share of two requests")

	refusal := admit(7)
	require.NotNil(t, refusal)
	assert.Equal(t, RateLimitScopeChannelOrgShare, refusal.scope)
	assert.Equal(t, int64(2), refusal.limit, "the limit is the organization's share of the budget")
	assert.Equal(t, int64(30), refusal.retryAfter)

	require.Nil(t, admit(8), "an organization without a share is limited only by the channel budget")
	require.Nil(t, admit(8), "the refused request must not have taken a channel admission")
	refusal = admit(9)
	require.NotNil(t, refusal)
	assert.Equal(t, RateLimitScopeChannel, refusal.scope, "once the channel budget is spent, the channel is named")
}

// A request the channel budget refuses gives back the share it took first, so the
// organization can use that share once the channel has room again.
func TestChannelRefusalGivesBackTheOrganizationShare(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()
	channel := budgetTestChannelWithShares(9802, 1, 60, map[int]int{7: 100})

	otherContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	otherContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	require.Nil(t, AcquireChannelBudget(otherContext, channel), "an unshared request takes the only channel admission")

	common.SetContextKey(c, constant.ContextKeyTokenBillingOrganizationId, 7)
	refusal := AcquireChannelBudget(c, channel)
	require.NotNil(t, refusal)
	assert.Equal(t, RateLimitScopeChannel, refusal.scope, "the channel, not the share, refused this request")

	ReleaseChannelBudget(otherContext)
	require.Nil(t, AcquireChannelBudget(c, channel), "the share given back by the refusal is available again")
}

// Releasing an admission returns both the organization share and the channel admission.
func TestReleaseReturnsTheOrganizationShareAndChannelAdmission(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()
	channel := budgetTestChannelWithShares(9803, 4, 60, map[int]int{7: 25})
	common.SetContextKey(c, constant.ContextKeyTokenBillingOrganizationId, 7)

	require.Nil(t, AcquireChannelBudget(c, channel))
	require.NotNil(t, AcquireChannelBudget(c, channel), "a share of one request is used up")
	ReleaseChannelBudget(c)
	require.Nil(t, AcquireChannelBudget(c, channel), "the released admission returns the share")
}

// Selection names the share as the scope of a 429 when the only candidate is over the
// organization's share.
func TestSelectChannelWithBudgetNamesTheOrganizationShare(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()
	channel := budgetTestChannelWithShares(9804, 2, 60, map[int]int{7: 50})
	common.SetContextKey(c, constant.ContextKeyTokenBillingOrganizationId, 7)
	picker := budgetTestPicker(c, channel)
	retry := &RetryParam{Ctx: c, ModelName: "budget-model", Retry: common.GetPointer(0)}

	_, _, selectErr := SelectChannelWithBudget(c, retry, picker)
	require.Nil(t, selectErr)
	_, _, selectErr = SelectChannelWithBudget(c, retry, picker)
	require.NotNil(t, selectErr, "the candidate is over the organization's share")
	assert.Equal(t, RateLimitScopeChannelOrgShare, selectErr.RateLimitedScope)
	assert.Equal(t, int64(1), selectErr.RateLimitRequests)
	assert.Equal(t, int64(60), selectErr.RetryAfterSeconds)
}

// TestChannelOrgShareRedisPathAgainstRealRedis admits concurrent requests of one
// organization on a real Redis server. The share bounds the admissions exactly. It is
// skipped unless NEW_API_TEST_REDIS_ADDR names one, and it uses database 15.
func TestChannelOrgShareRedisPathAgainstRealRedis(t *testing.T) {
	addr := os.Getenv("NEW_API_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set NEW_API_TEST_REDIS_ADDR (for example 127.0.0.1:6379) to run the Redis organization share path")
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.FlushDB(ctx).Err())

	const requests = 12
	const share = 25 // three of twelve requests
	channel := budgetTestChannelWithShares(9805, 12, 60, map[int]int{7: share})
	previousRDB := common.RDB
	t.Cleanup(func() { common.RDB = previousRDB })
	common.RDB = client
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = true
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })

	contexts := make([]*gin.Context, requests)
	for i := range contexts {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		common.SetContextKey(c, constant.ContextKeyTokenBillingOrganizationId, 7)
		contexts[i] = c
	}
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for _, c := range contexts {
		wg.Go(func() {
			if AcquireChannelBudget(c, channel) == nil {
				admitted.Add(1)
			}
		})
	}
	wg.Wait()
	assert.Equal(t, int64(3), admitted.Load(), "the organization share of three requests admits exactly three")
}
