package middleware

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	ModelRequestRateLimitCountMark        = "MRRL"
	ModelRequestRateLimitSuccessCountMark = "MRRLS"

	// modelRateLimitUpstreamReachedKey marks a request that passed channel
	// selection. Its admission counts against the total limit even when the
	// upstream fails. A request rejected before this point releases its slots.
	modelRateLimitUpstreamReachedKey = "model_rate_limit_upstream_reached"

	// modelRateLimitInflightTTLSeconds bounds how long an in-flight reservation
	// holds a slot. A process that dies mid-request therefore cannot block the
	// user past this time.
	modelRateLimitInflightTTLSeconds = 15 * 60
)

// modelRateLimitReserveScript admits one request atomically. It checks the
// success limit (recorded successes plus in-flight reservations) and the total
// token bucket, and reserves both only when both pass. A refusal writes nothing.
//
// KEYS[1] success records (sorted set, score = completion time in seconds)
// KEYS[2] in-flight reservations (sorted set, score = reservation time)
// KEYS[3] total admission bucket (hash: tokens, last_time)
// ARGV[1] now  ARGV[2] window  ARGV[3] success max (0 = off)
// ARGV[4] total max (0 = off)  ARGV[5] bucket capacity  ARGV[6] bucket rate
// ARGV[7] requested tokens     ARGV[8] reservation id   ARGV[9] in-flight TTL
// Returns {allowed, scope}: scope 1 = success limit, 2 = total limit.
const modelRateLimitReserveScript = `
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local successMax = tonumber(ARGV[3])
local totalMax = tonumber(ARGV[4])
local capacity = tonumber(ARGV[5])
local rate = tonumber(ARGV[6])
local requested = tonumber(ARGV[7])
local id = ARGV[8]
local inflightTTL = tonumber(ARGV[9])

redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now - inflightTTL)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - window)

if successMax > 0 and redis.call('ZCARD', KEYS[1]) + redis.call('ZCARD', KEYS[2]) >= successMax then
  return {0, 1}
end

if totalMax > 0 then
  local tokens = capacity
  local bucket = redis.call('HMGET', KEYS[3], 'tokens', 'last_time')
  if bucket[1] and bucket[2] then
    tokens = math.min(capacity, tonumber(bucket[1]) + (now - tonumber(bucket[2])) * rate)
  end
  if tokens < requested then
    return {0, 2}
  end
  redis.call('HMSET', KEYS[3], 'tokens', tokens - requested, 'last_time', now)
  redis.call('EXPIRE', KEYS[3], math.ceil(capacity / rate) + 60)
end

redis.call('ZADD', KEYS[2], now, id)
redis.call('EXPIRE', KEYS[2], inflightTTL)
return {1, 0}
`

// modelRateLimitSettleScript ends a reservation. It always removes the
// in-flight slot. A success is recorded only when requested. A release
// returns the admission token that the reservation took.
//
// KEYS as in the reserve script.
// ARGV[1] now  ARGV[2] window  ARGV[3] reservation id
// ARGV[4] record success ('1'/'0')  ARGV[5] refund admission ('1'/'0')
// ARGV[6] bucket capacity  ARGV[7] requested tokens
const modelRateLimitSettleScript = `
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])

redis.call('ZREM', KEYS[2], ARGV[3])
if ARGV[4] == '1' then
  redis.call('ZADD', KEYS[1], now, ARGV[3])
  redis.call('EXPIRE', KEYS[1], math.ceil(window) + 60)
end
if ARGV[5] == '1' then
  local bucket = redis.call('HMGET', KEYS[3], 'tokens')
  if bucket[1] then
    redis.call('HSET', KEYS[3], 'tokens', math.min(tonumber(ARGV[6]), tonumber(bucket[1]) + tonumber(ARGV[7])))
  end
end
return 1
`

// modelRateLimitScope names the limit that refused a request.
type modelRateLimitScope int

const (
	modelRateLimitAllowed modelRateLimitScope = iota
	modelRateLimitSuccessDenied
	modelRateLimitTotalDenied
)

// modelRateLimitReservation holds the slots of one admitted request until its
// outcome is known.
type modelRateLimitReservation interface {
	// commit ends the request after it reached the upstream or completed.
	// The admission stays counted. succeeded adds the request to the success count.
	commit(succeeded bool)
	// release ends a request that never reached the upstream. Every slot it took is returned.
	release()
}

// settleModelRateLimitReservation decides the outcome from the response. It is
// deferred around the downstream handlers, so it runs for every admitted request.
func settleModelRateLimitReservation(c *gin.Context, reservation modelRateLimitReservation) {
	switch {
	case c.Writer.Status() < http.StatusBadRequest:
		reservation.commit(modelRequestSucceeded(c))
	case c.GetBool(modelRateLimitUpstreamReachedKey):
		reservation.commit(false)
	default:
		reservation.release()
	}
}

// MarkModelRateLimitUpstreamReached records that the request passed channel
// selection. Call it after the channel is set up for the request.
func MarkModelRateLimitUpstreamReached(c *gin.Context) {
	c.Set(modelRateLimitUpstreamReachedKey, true)
}

type redisModelRateLimitReservation struct {
	rdb          *redis.Client
	successKey   string
	inflightKey  string
	totalKey     string
	id           string
	window       int64
	capacity     int64
	requested    int64
	totalEnabled bool
}

func (r *redisModelRateLimitReservation) settle(record bool, refund bool) {
	err := r.rdb.Eval(context.Background(), modelRateLimitSettleScript,
		[]string{r.successKey, r.inflightKey, r.totalKey},
		float64(time.Now().UnixNano())/1e9,
		r.window, r.id, boolFlag(record), boolFlag(refund && r.totalEnabled), r.capacity, r.requested,
	).Err()
	if err != nil {
		// The in-flight slot expires on its own after modelRateLimitInflightTTLSeconds.
		fmt.Println(common.LogText("model rate limit settle failed: %s", err.Error()))
	}
}

func (r *redisModelRateLimitReservation) commit(succeeded bool) {
	r.settle(succeeded, false)
}

func (r *redisModelRateLimitReservation) release() {
	r.settle(false, true)
}

func boolFlag(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

// reserveRedisModelRequest admits one request against the Redis counters.
func reserveRedisModelRequest(rdb *redis.Client, subject string, duration int64, totalMaxCount, successMaxCount int, now time.Time) (*redisModelRateLimitReservation, modelRateLimitScope, error) {
	reservation := &redisModelRateLimitReservation{
		rdb:          rdb,
		successKey:   fmt.Sprintf("rateLimit:v3:model:success:%s", subject),
		inflightKey:  fmt.Sprintf("rateLimit:v3:model:inflight:%s", subject),
		totalKey:     fmt.Sprintf("rateLimit:v3:model:total:%s", subject),
		id:           common.GetUUID(),
		window:       duration,
		requested:    duration,
		totalEnabled: totalMaxCount > 0,
	}
	reservation.capacity = rateLimitCapacity(totalMaxCount, duration)
	rate := int64(totalMaxCount)
	result, err := rdb.Eval(context.Background(), modelRateLimitReserveScript,
		[]string{reservation.successKey, reservation.inflightKey, reservation.totalKey},
		float64(now.UnixNano())/1e9,
		duration, successMaxCount, totalMaxCount, reservation.capacity, rate, reservation.requested,
		reservation.id, modelRateLimitInflightTTLSeconds,
	).Slice()
	if err != nil {
		return nil, modelRateLimitAllowed, err
	}
	if len(result) != 2 {
		return nil, modelRateLimitAllowed, fmt.Errorf("unexpected model rate limit reply length %d", len(result))
	}
	allowed, err := redisReplyInteger(result[0])
	if err != nil {
		return nil, modelRateLimitAllowed, err
	}
	if allowed == 1 {
		return reservation, modelRateLimitAllowed, nil
	}
	scope, err := redisReplyInteger(result[1])
	if err != nil {
		return nil, modelRateLimitAllowed, err
	}
	if scope == 2 {
		return nil, modelRateLimitTotalDenied, nil
	}
	return nil, modelRateLimitSuccessDenied, nil
}

type memoryModelRateLimitReservation struct {
	success *common.RateLimitReservation
	total   *common.RateLimitReservation
}

func (r *memoryModelRateLimitReservation) commit(succeeded bool) {
	// The admission is recorded now. Until this call, the reservation counted as in flight.
	r.total.Complete(true)
	r.success.Complete(succeeded)
}

func (r *memoryModelRateLimitReservation) release() {
	r.total.Complete(false)
	r.success.Complete(false)
}

// reserveMemoryModelRequest admits one request against the in-process limiter.
// Both reservations count toward their limits while the request runs. If the
// total is refused, the success reservation is released in the same call.
func reserveMemoryModelRequest(subject string, duration int64, totalMaxCount, successMaxCount int) (*memoryModelRateLimitReservation, modelRateLimitScope) {
	totalKey := ModelRequestRateLimitCountMark + subject
	successKey := ModelRequestRateLimitSuccessCountMark + subject

	var success *common.RateLimitReservation
	if successMaxCount > 0 {
		success = inMemoryRateLimiter.Reserve(successKey, successMaxCount, duration)
		if success == nil {
			return nil, modelRateLimitSuccessDenied
		}
	}
	var total *common.RateLimitReservation
	if totalMaxCount > 0 {
		total = inMemoryRateLimiter.Reserve(totalKey, totalMaxCount, duration)
		if total == nil {
			success.Complete(false)
			return nil, modelRateLimitTotalDenied
		}
	}
	return &memoryModelRateLimitReservation{success: success, total: total}, modelRateLimitAllowed
}

func abortModelRateLimited(c *gin.Context, scope modelRateLimitScope, totalMaxCount, successMaxCount int) {
	if scope == modelRateLimitTotalDenied {
		abortWithOpenAiMessage(c, http.StatusTooManyRequests, i18n.T(c, i18n.MsgRateLimitTotalReached, map[string]any{"Minutes": setting.ModelRequestRateLimitDurationMinutes, "Max": totalMaxCount}))
		return
	}
	abortWithOpenAiMessage(c, http.StatusTooManyRequests, i18n.T(c, i18n.MsgRateLimitReached, map[string]any{"Minutes": setting.ModelRequestRateLimitDurationMinutes, "Max": successMaxCount}))
}

// Redis限流处理器
func redisRateLimitHandler(subject string, duration int64, totalMaxCount, successMaxCount int) gin.HandlerFunc {
	return func(c *gin.Context) {
		reservation, scope, err := reserveRedisModelRequest(common.RDB, subject, duration, totalMaxCount, successMaxCount, time.Now())
		if err != nil {
			fmt.Println(common.LogText("failed to reserve the model request limit: %s", err.Error()))
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
			return
		}
		if scope != modelRateLimitAllowed {
			abortModelRateLimited(c, scope, totalMaxCount, successMaxCount)
			return
		}
		defer settleModelRateLimitReservation(c, reservation)
		c.Next()
	}
}

// 内存限流处理器
func memoryRateLimitHandler(subject string, duration int64, totalMaxCount, successMaxCount int) gin.HandlerFunc {
	inMemoryRateLimiter.Init(time.Duration(setting.ModelRequestRateLimitDurationMinutes) * time.Minute)

	return func(c *gin.Context) {
		reservation, scope := reserveMemoryModelRequest(subject, duration, totalMaxCount, successMaxCount)
		if scope != modelRateLimitAllowed {
			abortModelRateLimited(c, scope, totalMaxCount, successMaxCount)
			return
		}
		defer settleModelRateLimitReservation(c, reservation)
		c.Next()
	}
}

func modelRequestSucceeded(c *gin.Context) bool {
	status, _ := common.GetContextKeyType[*relaycommon.StreamStatus](c, constant.ContextKeyResponseStreamStatus)
	return c.Writer.Status() < 400 && !status.ResponseFailed()
}

// ModelRequestRateLimit 模型请求限流中间件
func ModelRequestRateLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		// 在每个请求时检查是否启用限流
		if !setting.ModelRequestRateLimitEnabled {
			c.Next()
			return
		}

		// 计算限流参数
		duration := rateLimitDurationSeconds(setting.ModelRequestRateLimitDurationMinutes)
		totalMaxCount := setting.ModelRequestRateLimitCount
		successMaxCount := setting.ModelRequestRateLimitSuccessCount

		// 获取分组
		group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
		if group == "" {
			group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		}

		// 获取分组的限流配置。配置了独立限流的分组按用户和分组单独计数，
		// 其余分组共用该用户的全局计数
		subject := strconv.Itoa(c.GetInt("id"))
		groupTotalCount, groupSuccessCount, found := setting.GetGroupRateLimit(group)
		if found {
			totalMaxCount = groupTotalCount
			successMaxCount = groupSuccessCount
			subject += ":" + group
		}

		// 根据存储类型选择并执行限流处理器
		if common.RedisEnabled {
			redisRateLimitHandler(subject, duration, totalMaxCount, successMaxCount)(c)
		} else {
			memoryRateLimitHandler(subject, duration, totalMaxCount, successMaxCount)(c)
		}
	}
}

func rateLimitDurationSeconds(durationMinutes int) int64 {
	if durationMinutes <= 0 {
		return 0
	}
	minutes := int64(durationMinutes)
	if minutes > math.MaxInt64/60 {
		return math.MaxInt64
	}
	return minutes * 60
}

func rateLimitCapacity(count int, durationSeconds int64) int64 {
	if count <= 0 || durationSeconds <= 0 {
		return 0
	}
	c := int64(count)
	if c > math.MaxInt64/durationSeconds {
		return math.MaxInt64
	}
	return c * durationSeconds
}
