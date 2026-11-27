package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

var modelMemoryRateLimit common.InMemoryRateLimiter

// Each nonzero limit is checked before any admission is recorded. Completed
// successes and pending reservations use separate sets, so a long response
// stays reserved even after the request's total-count event leaves the window.
var admitModelRequest = redis.NewScript(`
redis.replicate_commands()
local now = tonumber(redis.call('TIME')[1])
local window, lease, id = tonumber(ARGV[1]), tonumber(ARGV[2]), ARGV[3]
-- A transport retry after a committed admission must not charge twice.
for i = 1, #KEYS, 2 do
  if redis.call('ZSCORE', KEYS[i], id) or redis.call('ZSCORE', KEYS[i+1], id) then
    return -1
  end
end
for i = 1, #KEYS, 2 do
  local limit = tonumber(ARGV[3 + math.floor((i+1)/2)])
  local used = redis.call('ZCOUNT', KEYS[i], '(' .. (now-window), '+inf')
  local pending = redis.call('ZCOUNT', KEYS[i+1], '(' .. now, '+inf')
  if used + pending >= limit then return math.floor((i-1)/2) end
end
for i = 1, #KEYS, 2 do
  redis.call('ZREMRANGEBYSCORE', KEYS[i], '-inf', now-window)
  if ARGV[3 + #KEYS/2 + math.floor((i+1)/2)] == '1' then
    redis.call('ZREMRANGEBYSCORE', KEYS[i+1], '-inf', now)
    redis.call('ZADD', KEYS[i+1], now+lease, id)
    redis.call('EXPIRE', KEYS[i+1], lease)
  else
    redis.call('ZADD', KEYS[i], now, id)
    redis.call('EXPIRE', KEYS[i], window)
  end
end
return -1
`)

// Completion and renewal only affect reservations still owned by this request.
// Failure/cancellation removes them without extending the success counter.
var updateModelRequestReservation = redis.NewScript(`
redis.replicate_commands()
local now = tonumber(redis.call('TIME')[1])
local window, lease, id, action = tonumber(ARGV[1]), tonumber(ARGV[2]), ARGV[3], ARGV[4]
for i = 1, #KEYS, 2 do
  local expiry = redis.call('ZSCORE', KEYS[i+1], id)
  if expiry and tonumber(expiry) > now then
    if action == 'renew' then
      redis.call('ZADD', KEYS[i+1], now+lease, id)
      redis.call('EXPIRE', KEYS[i+1], lease)
    else
      redis.call('ZREM', KEYS[i+1], id)
      if action == 'success' then
        redis.call('ZREMRANGEBYSCORE', KEYS[i], '-inf', now-window)
        redis.call('ZADD', KEYS[i], now, id)
        redis.call('EXPIRE', KEYS[i], window)
      end
    end
  end
end
return 0
`)

const (
	modelReservationTTL = 2 * time.Minute
	modelRedisTimeout   = 3 * time.Second
)

type modelLimit struct {
	common.RateLimitCheck
	code types.ErrorCode
}

func modelRequestSucceeded(c *gin.Context) bool {
	status, _ := common.GetContextKeyType[*relaycommon.StreamStatus](c, constant.ContextKeyResponseStreamStatus)
	return c.Request.Context().Err() == nil && c.Writer.Status() < 400 && !status.ResponseFailed()
}

// ModelRequestRateLimit limits each user across all API keys and also applies
// any extra per-user restriction for the requested group before channel routing.
func ModelRequestRateLimit() gin.HandlerFunc {
	return modelRequestRateLimit(model.GetUserModelRateLimit)
}

func modelRequestRateLimit(lookupOverride func(context.Context, int) (*model.UserModelRateLimit, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		config := setting.CurrentModelRequestRateLimit()
		if !config.Enabled {
			c.Next()
			return
		}
		userID := c.GetInt("id")
		ctx := c.Request.Context()
		override, err := lookupOverride(ctx, userID)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("model rate limit user override lookup: %v", err))
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
			return
		}
		if override != nil {
			config.Total, config.Success = override.Total, override.Success
		}
		group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
		if group == "" {
			group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		}
		windowMinutes := max(1, min(config.DurationMinutes, setting.MaxModelRateLimitMinutes))
		window := time.Duration(windowMinutes) * time.Minute
		// Keep user and group keys in the same Redis hash slot and keep old
		// counter formats isolated during rolling upgrades.
		prefix := "rateLimit:model:v2:{" + strconv.Itoa(userID) + "}:" + strconv.Itoa(windowMinutes) + ":"
		limits := []modelLimit{
			{common.RateLimitCheck{Key: prefix + "user:total", Limit: config.Total}, "user_rate_limit_total"},
			{common.RateLimitCheck{Key: prefix + "user:success", Limit: config.Success, Reserve: true}, "user_rate_limit_success"},
		}
		if counts, ok := config.Groups[group]; ok {
			limits = append(limits,
				modelLimit{common.RateLimitCheck{Key: prefix + "group:total:" + group, Limit: counts[0]}, "group_rate_limit_total"},
				modelLimit{common.RateLimitCheck{Key: prefix + "group:success:" + group, Limit: counts[1], Reserve: true}, "group_rate_limit_success"},
			)
		}
		checks := make([]common.RateLimitCheck, 0, len(limits))
		codes := make([]types.ErrorCode, 0, len(limits))
		for _, limit := range limits {
			if limit.Limit > 0 {
				checks = append(checks, limit.RateLimitCheck)
				codes = append(codes, limit.code)
			}
		}
		if len(checks) == 0 {
			c.Next()
			return
		}

		var rejected int
		var complete func(bool)
		if common.RedisEnabled {
			rejected, complete, err = admitModelRequestRedis(ctx, common.RDB, checks, window)
		} else {
			modelMemoryRateLimit.Init(setting.MaxModelRateLimitMinutes * time.Minute)
			var reservation *common.RateLimitBatchReservation
			rejected, reservation = modelMemoryRateLimit.Admit(checks, window)
			if reservation != nil {
				complete = reservation.Complete
			}
		}
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("model rate limit admission: %v", err))
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
			return
		}
		if rejected >= 0 {
			messageKey := i18n.MsgRateLimitTotalReached
			if checks[rejected].Reserve {
				messageKey = i18n.MsgRateLimitReached
			}
			message := i18n.T(c, messageKey, map[string]any{"Minutes": windowMinutes, "Max": checks[rejected].Limit})
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, message, codes[rejected])
			return
		}
		success := false
		if complete != nil {
			defer func() { complete(success) }()
		}
		c.Next()
		success = modelRequestSucceeded(c)
	}
}

func admitModelRequestRedis(ctx context.Context, client *redis.Client, checks []common.RateLimitCheck, window time.Duration) (int, func(bool), error) {
	if client == nil {
		return -1, nil, fmt.Errorf("Redis is enabled without a client")
	}
	keys := make([]string, 0, 2*len(checks))
	pending := make([]string, 0, 2*len(checks))
	base := []any{int64(window / time.Second), int64(modelReservationTTL / time.Second), common.GetUUID()}
	args := make([]any, 3+2*len(checks))
	copy(args, base)
	for i, check := range checks {
		// Separate domains even if a user-controlled group includes punctuation.
		pair := []string{check.Key + ":recorded", check.Key + ":pending"}
		keys = append(keys, pair...)
		args[3+i] = check.Limit
		args[3+len(checks)+i] = "0"
		if check.Reserve {
			pending = append(pending, pair...)
			args[3+len(checks)+i] = "1"
		}
	}
	admissionCtx, cancel := context.WithTimeout(ctx, modelRedisTimeout)
	rejected, err := admitModelRequest.Run(admissionCtx, client, keys, args...).Int()
	cancel()
	if err != nil || rejected >= 0 || len(pending) == 0 {
		return rejected, nil, err
	}
	// Release even on client cancellation or panic. If the process goes away
	// without completing, Redis reclaims abandoned reservations after the TTL.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(modelReservationTTL / 3)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				updateModelRedisReservation(client, pending, base, "renew")
			}
		}
	}()
	return -1, func(success bool) {
		close(done)
		action := "release"
		if success {
			action = "success"
		}
		updateModelRedisReservation(client, pending, base, action)
	}, nil
}

func updateModelRedisReservation(client *redis.Client, keys []string, base []any, action string) {
	ctx, cancel := context.WithTimeout(context.Background(), modelRedisTimeout)
	defer cancel()
	if err := updateModelRequestReservation.Run(ctx, client, keys, append(base, action)...).Err(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("model rate limit reservation %s: %v", action, err))
	}
}
