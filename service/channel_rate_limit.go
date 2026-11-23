package service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/limiter"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// channelRateLimitErrorCode is the error code clients see when a channel
// request budget refused the request.
const channelRateLimitErrorCode types.ErrorCode = "channel_rate_limited"

// maxChannelBudgetAttempts bounds how many channels one selection may check
// before it gives up with 429.
const maxChannelBudgetAttempts = 8

// channelBudgetMemoryLimiter holds channel budgets when Redis is disabled. It
// is separate from the middleware limiter because Init keeps the first
// configuration it receives. Its idle-key expiry is the longest allowed
// window, so a budget is not reset by a quiet period shorter than its window.
var channelBudgetMemoryLimiter common.InMemoryRateLimiter

// channelBudget returns the enabled request budget of a channel.
func channelBudget(channel *model.Channel) (requests int64, windowSeconds int64, ok bool) {
	budget := channel.GetSetting().RateLimit
	if budget == nil || !budget.Enabled || budget.Requests <= 0 || budget.DurationSeconds <= 0 {
		return 0, 0, false
	}
	return int64(budget.Requests), int64(budget.DurationSeconds), true
}

// budgetRetryAfter is the Retry-After hint for a refused request: the time needed to
// refill one request's worth of a budget of requests per window. It is exact when the
// bucket is empty and an upper bound otherwise.
func budgetRetryAfter(requests, window int64) int64 {
	return max(1, (window+requests-1)/requests)
}

// channelBudgetRefusal is the limit that refused a request on one channel.
type channelBudgetRefusal struct {
	retryAfter int64
	limit      int64
	scope      string
}

// channelBudgetHeldKey and channelBudgetRefusedKey are the gin context keys of the
// admissions a request holds and of the refusals it received, per channel.
const (
	channelBudgetHeldKey    = "channel_budget_hold"
	channelBudgetRefusedKey = "channel_budget_refusals"
)

// channelBudgetHold is one admission taken from a token bucket, so that it can be
// returned if the request never reaches the upstream.
type channelBudgetHold struct {
	key       string
	redis     bool
	capacity  int64
	requested int64
}

// channelBudgetRefundScript gives one admission back to a token bucket, capped at
// its capacity. It leaves the bucket untouched when the bucket is absent.
const channelBudgetRefundScript = `
local bucket = redis.call('HMGET', KEYS[1], 'tokens')
if bucket[1] then
  redis.call('HSET', KEYS[1], 'tokens', math.min(tonumber(ARGV[1]), tonumber(bucket[1]) + tonumber(ARGV[2])))
end
return 1
`

// channelBudgetRefund returns the admission with EVALSHA, falling back to EVAL only
// when Redis has not cached the script yet.
var channelBudgetRefund = redis.NewScript(channelBudgetRefundScript)

// channelOrgShare returns the paying organization of the request and its share of a
// channel budget of requests per window. The share is a percentage of the budget and
// is at least one request, so rounding never locks a configured organization out. It
// reports false when the request has no paying organization or the channel does not
// cap that organization.
func channelOrgShare(c *gin.Context, channel *model.Channel, requests int64) (organizationId int, shareRequests int64, capped bool) {
	organizationId = common.GetContextKeyInt(c, constant.ContextKeyTokenBillingOrganizationId)
	if organizationId <= 0 {
		return 0, 0, false
	}
	budget := channel.GetSetting().RateLimit
	if budget == nil {
		return 0, 0, false
	}
	percent, ok := budget.OrgShares[organizationId]
	if !ok {
		return 0, 0, false
	}
	return organizationId, max(1, requests*int64(percent)/100), true
}

// takeChannelBudgetToken takes one request from the token bucket of key. The bucket
// holds requests per window of tokens, refills at requests per second, and each
// request costs one window of tokens. The error is returned so the caller can fail open.
func takeChannelBudgetToken(c *gin.Context, key string, requests, window int64) (channelBudgetHold, bool, int64, error) {
	hold := channelBudgetHold{key: key, capacity: requests * window, requested: window}
	if !common.RedisEnabled || common.RDB == nil {
		channelBudgetMemoryLimiter.Init(time.Duration(kitdto.MaxChannelRateLimitWindowSeconds) * time.Second)
		allowed, remaining := channelBudgetMemoryLimiter.RequestRemaining(key, int(requests), window)
		return hold, allowed, remaining, nil
	}
	hold.redis = true
	ctx := c.Request.Context()
	allowed, remaining, err := limiter.New(ctx, common.RDB).AllowWithRemaining(ctx, key,
		limiter.WithCapacity(requests*window),
		limiter.WithRate(requests),
		limiter.WithRequested(window),
	)
	return hold, allowed, remaining, err
}

// AcquireChannelBudget takes one request from the channel's budget, and from the paying
// organization's share of it when the channel caps that organization. The share is taken
// first, so a request over its share never uses up the channel budget. When the channel
// budget then refuses, the share is given back. It returns nil when the request is
// admitted. A Redis error fails open: a Redis outage must not take healthy channels out
// of service.
func AcquireChannelBudget(c *gin.Context, channel *model.Channel) *channelBudgetRefusal {
	requests, window, ok := channelBudget(channel)
	if !ok {
		return nil
	}
	var holds []channelBudgetHold
	var shareHeld bool
	var shareRemaining int64
	organizationId, shareRequests, shareCapped := channelOrgShare(c, channel, requests)
	if shareCapped {
		hold, allowed, remaining, err := takeChannelBudgetToken(c, channelOrgShareKey(channel.Id, organizationId), shareRequests, window)
		switch {
		case err != nil:
			logChannelBudgetError(c, channel.Id, err)
		case !allowed:
			return recordChannelBudgetRefusal(c, channel.Id, channelBudgetRefusal{
				retryAfter: budgetRetryAfter(shareRequests, window),
				limit:      shareRequests,
				scope:      RateLimitScopeChannelOrgShare,
			})
		default:
			holds = append(holds, hold)
			shareHeld, shareRemaining = true, remaining
		}
	}

	hold, allowed, remaining, err := takeChannelBudgetToken(c, channelBudgetKey(channel.Id), requests, window)
	switch {
	case err != nil:
		// Fail open on the channel budget, but keep the share this request already took.
		logChannelBudgetError(c, channel.Id, err)
		holdChannelBudget(c, holds)
		return nil
	case !allowed:
		releaseChannelBudgetHolds(c.Request.Context(), holds)
		return recordChannelBudgetRefusal(c, channel.Id, channelBudgetRefusal{
			retryAfter: budgetRetryAfter(requests, window),
			limit:      requests,
			scope:      RateLimitScopeChannel,
		})
	}
	holds = append(holds, hold)
	holdChannelBudget(c, holds)

	// The response reports the tighter of the two limits, the one the client is about to hit.
	limit, left := requests, remaining
	if shareHeld && shareRemaining < remaining {
		limit, left = shareRequests, shareRemaining
	}
	SetRateLimitRemaining(c, limit, left)
	return nil
}

func channelBudgetKey(channelId int) string {
	return fmt.Sprintf("rateLimit:channel:%d", channelId)
}

func channelOrgShareKey(channelId int, organizationId int) string {
	return fmt.Sprintf("rateLimit:channel:%d:org:%d", channelId, organizationId)
}

func logChannelBudgetError(c *gin.Context, channelId int, err error) {
	logger.LogError(c.Request.Context(), common.LogText("channel budget check failed (channel_id=%d): %v", channelId, err))
}

// holdChannelBudget records the admissions a request holds, so that a release returns them.
func holdChannelBudget(c *gin.Context, holds []channelBudgetHold) {
	if len(holds) == 0 {
		return
	}
	existing, _ := c.Get(channelBudgetHeldKey)
	held, _ := existing.([]channelBudgetHold)
	c.Set(channelBudgetHeldKey, append(held, holds...))
}

// releaseChannelBudgetHolds gives the admissions back to their token buckets.
func releaseChannelBudgetHolds(ctx context.Context, holds []channelBudgetHold) {
	for _, hold := range holds {
		if !hold.redis {
			channelBudgetMemoryLimiter.Refund(hold.key)
			continue
		}
		if err := channelBudgetRefund.Run(ctx, common.RDB, []string{hold.key}, hold.capacity, hold.requested).Err(); err != nil {
			logger.LogError(ctx, common.LogText("channel budget release failed (key=%s): %v", hold.key, err))
		}
	}
}

// recordChannelBudgetRefusal remembers the refusal of a channel for the rest of the
// request, and returns it.
func recordChannelBudgetRefusal(c *gin.Context, channelId int, refusal channelBudgetRefusal) *channelBudgetRefusal {
	refusals, _ := c.Get(channelBudgetRefusedKey)
	byChannel, _ := refusals.(map[int]channelBudgetRefusal)
	if byChannel == nil {
		byChannel = make(map[int]channelBudgetRefusal)
		c.Set(channelBudgetRefusedKey, byChannel)
	}
	byChannel[channelId] = refusal
	return &refusal
}

// channelBudgetRefusalAt returns the refusal a channel gave this request, if any.
func channelBudgetRefusalAt(c *gin.Context, channelId int) channelBudgetRefusal {
	refusals, _ := c.Get(channelBudgetRefusedKey)
	byChannel, _ := refusals.(map[int]channelBudgetRefusal)
	return byChannel[channelId]
}

// ReleaseChannelBudget returns the channel admissions the request holds. Call it
// when the request fails before it reaches the upstream, such as when the quota
// pre-consume refuses it, so a refused request does not use up the channel's budget.
func ReleaseChannelBudget(c *gin.Context) {
	existing, _ := c.Get(channelBudgetHeldKey)
	holds, _ := existing.([]channelBudgetHold)
	if len(holds) == 0 {
		return
	}
	c.Set(channelBudgetHeldKey, nil)
	releaseChannelBudgetHolds(context.Background(), holds)
}

// channelBudgetExhaustedError is the 429 returned when no channel can take the
// request within its budget. The refusal names the limit that refused it last.
func channelBudgetExhaustedError(refusal channelBudgetRefusal) *ChannelSelectError {
	scope := refusal.scope
	if scope == "" {
		scope = RateLimitScopeChannel
	}
	return &ChannelSelectError{
		StatusCode:        http.StatusTooManyRequests,
		Code:              channelRateLimitErrorCode,
		MessageID:         i18n.MsgDistributorChannelRateLimited,
		FilterKind:        dto.FilterChannelRateLimited,
		RetryAfterSeconds: refusal.retryAfter,
		RateLimitRequests: refusal.limit,
		RateLimitedScope:  scope,
	}
}

// ChannelPicker chooses one channel for an attempt. The shared selection and
// the relay retry selection both satisfy it.
type ChannelPicker func(retry *RetryParam) (*model.Channel, string, *ChannelSelectError)

// SelectChannelWithBudget runs pick and takes one request from the chosen
// channel's budget. An over-budget channel is excluded and pick runs again at
// the same retry index, so the request moves to another channel of the same
// priority tier without consuming a retry. A selection that would drop to a
// lower tier is refused instead, because routing to a lower tier is a decision
// the operator has not asked for. A pinned channel is never replaced.
func SelectChannelWithBudget(c *gin.Context, retry *RetryParam, pick ChannelPicker) (*model.Channel, string, *ChannelSelectError) {
	constraints := GetChannelConstraints(c)
	baseFilters := len(constraints.Filters)
	defer func() { constraints.Filters = constraints.Filters[:baseFilters] }()
	_, pinned, _ := constraints.ResolvedPin()

	var excluded []int
	var tierPriority int64
	var refusal channelBudgetRefusal
	for attempt := 0; ; attempt++ {
		// The exclusion lasts for this selection only, so a later request
		// starts with every channel eligible again.
		constraints.Filters = constraints.Filters[:baseFilters]
		if len(excluded) > 0 {
			constraints.AddFilter(dto.ChannelFilter{Kind: dto.FilterChannelRateLimited, ExcludeChannelIDs: excluded})
		}

		channel, group, selectErr := pick(retry)
		if selectErr != nil {
			if len(excluded) > 0 && selectErr.NoAvailableChannel {
				// Every remaining candidate is over budget.
				return nil, group, channelBudgetExhaustedError(refusal)
			}
			return nil, group, selectErr
		}

		if attempt == 0 {
			tierPriority = channel.GetPriority()
		} else if channel.GetPriority() != tierPriority {
			return nil, group, channelBudgetExhaustedError(refusal)
		}

		refused := AcquireChannelBudget(c, channel)
		if refused == nil {
			return channel, group, nil
		}
		refusal = *refused
		if pinned || len(excluded)+1 >= maxChannelBudgetAttempts {
			return nil, group, channelBudgetExhaustedError(refusal)
		}
		excluded = append(excluded, channel.Id)
	}
}
