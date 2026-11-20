package service

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/limiter"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
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

// channelBudgetRetryAfter is the Retry-After hint for a refused request: the
// time needed to refill one request's worth of budget. It is exact when the
// budget is empty and an upper bound otherwise.
func channelBudgetRetryAfter(channel *model.Channel) int64 {
	requests, window, ok := channelBudget(channel)
	if !ok {
		return 0
	}
	return max(1, (window+requests-1)/requests)
}

// AcquireChannelBudget takes one request from the channel's budget. It returns
// false with a Retry-After hint when the budget is exhausted. A Redis error
// fails open: a Redis outage must not take healthy channels out of service.
func AcquireChannelBudget(c *gin.Context, channel *model.Channel) (bool, int64) {
	requests, window, ok := channelBudget(channel)
	if !ok {
		return true, 0
	}
	key := fmt.Sprintf("rateLimit:channel:%d", channel.Id)

	var allowed bool
	if common.RedisEnabled && common.RDB != nil {
		ctx := c.Request.Context()
		var err error
		// A token bucket where each request costs one window of tokens and the
		// bucket refills at `requests` per second: `requests` requests may burst,
		// and the sustained rate is `requests` per window.
		allowed, err = limiter.New(ctx, common.RDB).Allow(ctx, key,
			limiter.WithCapacity(requests*window),
			limiter.WithRate(requests),
			limiter.WithRequested(window),
		)
		if err != nil {
			logger.LogError(ctx, common.LogText("channel budget check failed (channel_id=%d): %v", channel.Id, err))
			return true, 0
		}
	} else {
		channelBudgetMemoryLimiter.Init(time.Duration(kitdto.MaxChannelRateLimitWindowSeconds) * time.Second)
		allowed = channelBudgetMemoryLimiter.Request(key, int(requests), window)
	}
	if allowed {
		return true, 0
	}
	return false, channelBudgetRetryAfter(channel)
}

// channelBudgetExhaustedError is the 429 returned when no channel can take the
// request within its budget.
func channelBudgetExhaustedError(retryAfterSeconds int64) *ChannelSelectError {
	return &ChannelSelectError{
		StatusCode:        http.StatusTooManyRequests,
		Code:              channelRateLimitErrorCode,
		MessageID:         i18n.MsgDistributorChannelRateLimited,
		FilterKind:        dto.FilterChannelRateLimited,
		RetryAfterSeconds: retryAfterSeconds,
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
	var retryAfter int64
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
				return nil, group, channelBudgetExhaustedError(retryAfter)
			}
			return nil, group, selectErr
		}

		if attempt == 0 {
			tierPriority = channel.GetPriority()
		} else if channel.GetPriority() != tierPriority {
			return nil, group, channelBudgetExhaustedError(retryAfter)
		}

		allowed, wait := AcquireChannelBudget(c, channel)
		if allowed {
			return channel, group, nil
		}
		retryAfter = wait
		if pinned || len(excluded)+1 >= maxChannelBudgetAttempts {
			return nil, group, channelBudgetExhaustedError(retryAfter)
		}
		excluded = append(excluded, channel.Id)
	}
}
