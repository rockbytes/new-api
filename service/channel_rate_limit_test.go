package service

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
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

	allowed, _ := AcquireChannelBudget(c, channel)
	assert.True(t, allowed)
	allowed, _ = AcquireChannelBudget(c, channel)
	assert.True(t, allowed)

	allowed, retryAfter := AcquireChannelBudget(c, channel)
	assert.False(t, allowed)
	assert.Equal(t, int64(30), retryAfter, "two requests per 60s refill one request in 30s")

	unlimited := budgetTestChannel(9402, 1, 0, 0)
	for range 5 {
		allowed, _ := AcquireChannelBudget(c, unlimited)
		require.True(t, allowed, "a channel without a budget is never refused")
	}
}

// ReleaseChannelBudget returns the admission of a request that never reached the
// upstream, once. A second release has nothing left to return.
func TestReleaseChannelBudgetReturnsAdmissionOnce(t *testing.T) {
	c, restore := newBudgetTestContext(t)
	defer restore()

	channel := budgetTestChannel(9403, 1, 1, 60)
	allowed, _ := AcquireChannelBudget(c, channel)
	require.True(t, allowed)
	allowed, _ = AcquireChannelBudget(c, channel)
	require.False(t, allowed)

	ReleaseChannelBudget(c)
	ReleaseChannelBudget(c)
	allowed, _ = AcquireChannelBudget(c, channel)
	assert.True(t, allowed, "the released admission is available to the next request")
	allowed, _ = AcquireChannelBudget(c, channel)
	assert.False(t, allowed, "the second release must not return a second admission")
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
		allowed, _ := AcquireChannelBudget(c, channel)
		require.True(t, allowed, "request %d is within the two-per-window budget", i+1)
	}
	allowed, retryAfter := AcquireChannelBudget(c, channel)
	assert.False(t, allowed, "the third request in the window must be refused")
	assert.Equal(t, int64(30), retryAfter)

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
