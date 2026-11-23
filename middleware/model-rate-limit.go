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
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	ModelRequestRateLimitCountMark        = "MRRL"
	ModelRequestRateLimitSuccessCountMark = "MRRLS"
	modelRateLimitOrganizationCountMark   = "MRRLO"

	// modelRateLimitUpstreamReachedKey marks a request that passed channel
	// selection. Its admission counts against the limits even when the upstream
	// fails. A request rejected before this point releases its slots.
	modelRateLimitUpstreamReachedKey = "model_rate_limit_upstream_reached"

	// modelRateLimitInflightTTLSeconds bounds how long an in-flight reservation
	// holds a slot. A process that dies mid-request therefore cannot block the
	// user past this time.
	modelRateLimitInflightTTLSeconds = 15 * 60
)

// modelRateLimitReserveScript admits one request atomically. It checks every
// limit first and writes nothing until all of them pass, so a refusal leaves
// every counter as it was.
//
// KEYS[1] success records (sorted set, score = completion time in seconds)
// KEYS[2] in-flight reservations (sorted set, score = reservation time)
// KEYS[3] user total admission bucket (hash: tokens, last_time)
// KEYS[4] organization total admission bucket (hash: tokens, last_time)
// ARGV[1] now            ARGV[2] window           ARGV[3] success max (0 = off)
// ARGV[4] total max      ARGV[5] total capacity   ARGV[6] total rate
// ARGV[7] requested      ARGV[8] reservation id   ARGV[9] in-flight TTL
// ARGV[10] organization max  ARGV[11] organization capacity  ARGV[12] organization rate
// Returns {allowed, scope, retryAfter}: scope 1 = success, 2 = user total, 3 = organization
// total. retryAfter is in seconds: for a token bucket, the time until it holds the tokens the
// request needs; for the success limit, the window (its in-flight requests are not predictable).
const modelRateLimitReserveScript = `
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local successMax = tonumber(ARGV[3])
local totalMax = tonumber(ARGV[4])
local totalCapacity = tonumber(ARGV[5])
local totalRate = tonumber(ARGV[6])
local requested = tonumber(ARGV[7])
local id = ARGV[8]
local inflightTTL = tonumber(ARGV[9])
local orgMax = tonumber(ARGV[10])
local orgCapacity = tonumber(ARGV[11])
local orgRate = tonumber(ARGV[12])

local function refill(key, capacity, rate)
  local tokens = capacity
  local bucket = redis.call('HMGET', key, 'tokens', 'last_time')
  if bucket[1] and bucket[2] then
    tokens = math.min(capacity, tonumber(bucket[1]) + (now - tonumber(bucket[2])) * rate)
  end
  return tokens
end

redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now - inflightTTL)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - window)

if successMax > 0 and redis.call('ZCARD', KEYS[1]) + redis.call('ZCARD', KEYS[2]) >= successMax then
  return {0, 1, window}
end

local totalTokens, orgTokens = 0, 0
if totalMax > 0 then
  totalTokens = refill(KEYS[3], totalCapacity, totalRate)
  if totalTokens < requested then
    return {0, 2, math.max(1, math.ceil((requested - totalTokens) / totalRate))}
  end
end
if orgMax > 0 then
  orgTokens = refill(KEYS[4], orgCapacity, orgRate)
  if orgTokens < requested then
    return {0, 3, math.max(1, math.ceil((requested - orgTokens) / orgRate))}
  end
end

if totalMax > 0 then
  redis.call('HMSET', KEYS[3], 'tokens', totalTokens - requested, 'last_time', now)
  redis.call('EXPIRE', KEYS[3], math.ceil(totalCapacity / totalRate) + 60)
end
if orgMax > 0 then
  redis.call('HMSET', KEYS[4], 'tokens', orgTokens - requested, 'last_time', now)
  redis.call('EXPIRE', KEYS[4], math.ceil(orgCapacity / orgRate) + 60)
end
if successMax > 0 then
  redis.call('ZADD', KEYS[2], now, id)
  redis.call('EXPIRE', KEYS[2], inflightTTL)
end
local remTotal, remOrg, remSuccess = -1, -1, -1
if totalMax > 0 then remTotal = math.floor((totalTokens - requested) / requested) end
if orgMax > 0 then remOrg = math.floor((orgTokens - requested) / requested) end
if successMax > 0 then remSuccess = successMax - (redis.call('ZCARD', KEYS[1]) + redis.call('ZCARD', KEYS[2])) end
return {1, 0, remTotal, remOrg, remSuccess}
`

// The scripts are loaded once as go-redis Scripts. Run sends EVALSHA and falls back
// to EVAL only when Redis has not cached the script yet (NOSCRIPT).
var (
	modelRateLimitReserve = redis.NewScript(modelRateLimitReserveScript)
	modelRateLimitSettle  = redis.NewScript(modelRateLimitSettleScript)
)

// modelRateLimitSettleScript ends a reservation. It always removes the in-flight
// slot. A success is recorded only when requested. A release returns the
// admission tokens the reservation took from the user and organization buckets.
//
// KEYS as in the reserve script.
// ARGV[1] now  ARGV[2] window  ARGV[3] reservation id
// ARGV[4] record success ('1'/'0')  ARGV[5] refund user total ('1'/'0')
// ARGV[6] user capacity  ARGV[7] requested  ARGV[8] refund organization ('1'/'0')
// ARGV[9] organization capacity
const modelRateLimitSettleScript = `
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local requested = tonumber(ARGV[7])

local function refund(key, capacity)
  local bucket = redis.call('HMGET', key, 'tokens')
  if bucket[1] then
    redis.call('HSET', key, 'tokens', math.min(capacity, tonumber(bucket[1]) + requested))
  end
end

redis.call('ZREM', KEYS[2], ARGV[3])
if ARGV[4] == '1' then
  redis.call('ZADD', KEYS[1], now, ARGV[3])
  redis.call('EXPIRE', KEYS[1], math.ceil(window) + 60)
end
if ARGV[5] == '1' then
  refund(KEYS[3], tonumber(ARGV[6]))
end
if ARGV[8] == '1' then
  refund(KEYS[4], tonumber(ARGV[9]))
end
return 1
`

// modelRateLimitScope names the limit that refused a request.
type modelRateLimitScope int

const (
	modelRateLimitAllowed modelRateLimitScope = iota
	modelRateLimitSuccessDenied
	modelRateLimitTotalDenied
	modelRateLimitOrganizationDenied
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

// ClearModelRateLimitUpstreamReached undoes MarkModelRateLimitUpstreamReached for
// a request refused before its upstream was reached, such as by quota pre-consume.
// Its settlement then releases the slots instead of counting the request.
func ClearModelRateLimitUpstreamReached(c *gin.Context) {
	c.Set(modelRateLimitUpstreamReachedKey, false)
}

type redisModelRateLimitReservation struct {
	rdb             *redis.Client
	successKey      string
	inflightKey     string
	totalKey        string
	orgKey          string
	id              string
	window          int64
	userCapacity    int64
	orgCapacity     int64
	requested       int64
	recordSucc      bool
	totalEnabled    bool
	orgEnabled      bool
	headerLimit     int64
	headerRemaining int64
}

func (r *redisModelRateLimitReservation) settle(record bool, refund bool) {
	err := modelRateLimitSettle.Run(context.Background(), r.rdb,
		[]string{r.successKey, r.inflightKey, r.totalKey, r.orgKey},
		float64(time.Now().UnixNano())/1e9,
		r.window, r.id,
		boolFlag(record && r.recordSucc),
		boolFlag(refund && r.totalEnabled), r.userCapacity, r.requested,
		boolFlag(refund && r.orgEnabled), r.orgCapacity,
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

// layerLeft is one enabled layer of an admission: its limit and the requests it still allows.
type layerLeft struct {
	limit     int64
	remaining int64
}

// tightestLayer returns the limit and remaining count of the layer with the fewest requests
// left, which is the layer a client is about to hit. It returns zeros when no layer is enabled.
func tightestLayer(layers ...layerLeft) (int64, int64) {
	var limit, remaining int64
	found := false
	for _, layer := range layers {
		if layer.limit <= 0 || layer.remaining < 0 {
			continue
		}
		if !found || layer.remaining < remaining {
			limit, remaining, found = layer.limit, layer.remaining, true
		}
	}
	return limit, remaining
}

// redisAdmissionHeaders reads the remaining counts that the reservation script returns with an
// admission: user total at index 2, organization total at 3 and success at 4, -1 when disabled.
func redisAdmissionHeaders(result []any, totalMax, orgMax, successMax int) (int64, int64) {
	layers := make([]layerLeft, 0, 3)
	for _, layer := range []struct{ limit, index int }{{totalMax, 2}, {orgMax, 3}, {successMax, 4}} {
		if len(result) <= layer.index {
			continue
		}
		left, err := redisReplyInteger(result[layer.index])
		if err != nil {
			continue
		}
		layers = append(layers, layerLeft{limit: int64(layer.limit), remaining: left})
	}
	return tightestLayer(layers...)
}

// memoryAdmissionHeaders takes the remaining counts of the in-process reservations.
func memoryAdmissionHeaders(success, total, org *common.RateLimitReservation, successMax, totalMax, orgMax int) (int64, int64) {
	var layers []layerLeft
	if success != nil {
		layers = append(layers, layerLeft{limit: int64(successMax), remaining: int64(success.Remaining)})
	}
	if total != nil {
		layers = append(layers, layerLeft{limit: int64(totalMax), remaining: int64(total.Remaining)})
	}
	if org != nil {
		layers = append(layers, layerLeft{limit: int64(orgMax), remaining: int64(org.Remaining)})
	}
	return tightestLayer(layers...)
}

// reserveRedisModelRequest admits one request against the user limits only.
func reserveRedisModelRequest(rdb *redis.Client, subject string, duration int64, totalMaxCount, successMaxCount int, now time.Time) (*redisModelRateLimitReservation, modelRateLimitScope, int64, error) {
	return reserveRedisModelRequestWithOrganization(rdb, subject, duration, totalMaxCount, successMaxCount, 0, 0, now)
}

// reserveRedisModelRequestWithOrganization admits one request against the user
// limits and, when organizationMax is positive, the organization's total limit.
// All of them are checked and reserved by one script.
func reserveRedisModelRequestWithOrganization(rdb *redis.Client, subject string, duration int64, totalMaxCount, successMaxCount int, organizationId int, organizationMax int, now time.Time) (*redisModelRateLimitReservation, modelRateLimitScope, int64, error) {
	reservation := &redisModelRateLimitReservation{
		rdb:          rdb,
		successKey:   fmt.Sprintf("rateLimit:v3:model:success:%s", subject),
		inflightKey:  fmt.Sprintf("rateLimit:v3:model:inflight:%s", subject),
		totalKey:     fmt.Sprintf("rateLimit:v3:model:total:%s", subject),
		orgKey:       fmt.Sprintf("rateLimit:v3:model:org:%d", organizationId),
		id:           common.GetUUID(),
		window:       duration,
		requested:    duration,
		recordSucc:   successMaxCount > 0,
		totalEnabled: totalMaxCount > 0,
		orgEnabled:   organizationMax > 0,
	}
	reservation.userCapacity = rateLimitCapacity(totalMaxCount, duration)
	reservation.orgCapacity = rateLimitCapacity(organizationMax, duration)
	result, err := modelRateLimitReserve.Run(context.Background(), rdb,
		[]string{reservation.successKey, reservation.inflightKey, reservation.totalKey, reservation.orgKey},
		float64(now.UnixNano())/1e9,
		duration, successMaxCount, totalMaxCount, reservation.userCapacity, int64(totalMaxCount), reservation.requested,
		reservation.id, modelRateLimitInflightTTLSeconds,
		organizationMax, reservation.orgCapacity, int64(organizationMax),
	).Slice()
	if err != nil {
		return nil, modelRateLimitAllowed, 0, err
	}
	if len(result) != 3 && len(result) != 5 {
		return nil, modelRateLimitAllowed, 0, fmt.Errorf("unexpected model rate limit reply length %d", len(result))
	}
	allowed, err := redisReplyInteger(result[0])
	if err != nil {
		return nil, modelRateLimitAllowed, 0, err
	}
	if allowed == 1 {
		reservation.headerLimit, reservation.headerRemaining = redisAdmissionHeaders(result, totalMaxCount, organizationMax, successMaxCount)
		return reservation, modelRateLimitAllowed, 0, nil
	}
	retryAfter := int64(0)
	if len(result) == 3 {
		retryAfter, err = redisReplyInteger(result[2])
		if err != nil {
			return nil, modelRateLimitAllowed, 0, err
		}
	}
	scope, err := redisReplyInteger(result[1])
	if err != nil {
		return nil, modelRateLimitAllowed, 0, err
	}
	switch scope {
	case 2:
		return nil, modelRateLimitTotalDenied, retryAfter, nil
	case 3:
		return nil, modelRateLimitOrganizationDenied, retryAfter, nil
	default:
		return nil, modelRateLimitSuccessDenied, retryAfter, nil
	}
}

type memoryModelRateLimitReservation struct {
	success         *common.RateLimitReservation
	total           *common.RateLimitReservation
	org             *common.RateLimitReservation
	headerLimit     int64
	headerRemaining int64
}

func (r *memoryModelRateLimitReservation) commit(succeeded bool) {
	// The admission is recorded now. Until this call, the reservation counted as in flight.
	r.total.Complete(true)
	r.org.Complete(true)
	r.success.Complete(succeeded)
}

func (r *memoryModelRateLimitReservation) release() {
	r.total.Complete(false)
	r.org.Complete(false)
	r.success.Complete(false)
}

// reserveMemoryModelRequest admits one request against the user limits only.
func reserveMemoryModelRequest(subject string, duration int64, totalMaxCount, successMaxCount int) (*memoryModelRateLimitReservation, modelRateLimitScope, int64) {
	return reserveMemoryModelRequestWithOrganization(subject, duration, totalMaxCount, successMaxCount, 0, 0)
}

// reserveMemoryModelRequestWithOrganization admits one request against the
// in-process limiter. The reservations count toward their limits while the
// request runs. A refusal releases the reservations already taken.
func reserveMemoryModelRequestWithOrganization(subject string, duration int64, totalMaxCount, successMaxCount int, organizationId int, organizationMax int) (*memoryModelRateLimitReservation, modelRateLimitScope, int64) {
	totalKey := ModelRequestRateLimitCountMark + subject
	successKey := ModelRequestRateLimitSuccessCountMark + subject
	orgKey := modelRateLimitOrganizationCountMark + strconv.Itoa(organizationId)

	var success, total, org *common.RateLimitReservation
	if successMaxCount > 0 {
		success = inMemoryRateLimiter.Reserve(successKey, successMaxCount, duration)
		if success == nil {
			return nil, modelRateLimitSuccessDenied, duration
		}
	}
	if totalMaxCount > 0 {
		total = inMemoryRateLimiter.Reserve(totalKey, totalMaxCount, duration)
		if total == nil {
			success.Complete(false)
			return nil, modelRateLimitTotalDenied, inMemoryRateLimiter.RetryAfter(totalKey, duration)
		}
	}
	if organizationMax > 0 {
		org = inMemoryRateLimiter.Reserve(orgKey, organizationMax, duration)
		if org == nil {
			total.Complete(false)
			success.Complete(false)
			return nil, modelRateLimitOrganizationDenied, inMemoryRateLimiter.RetryAfter(orgKey, duration)
		}
	}
	limit, remaining := memoryAdmissionHeaders(success, total, org, successMaxCount, totalMaxCount, organizationMax)
	return &memoryModelRateLimitReservation{success: success, total: total, org: org, headerLimit: limit, headerRemaining: remaining}, modelRateLimitAllowed, 0
}

// abortModelRateLimited rejects a request refused by a limit. retryAfter is the Retry-After hint in seconds:
// a token bucket gives the exact time to refill; the success limit gives the window, an upper bound.
func abortModelRateLimited(c *gin.Context, scope modelRateLimitScope, retryAfter int64, totalMaxCount, successMaxCount, organizationMax int) {
	c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
	layer := service.RateLimitScopeUser
	limit := int64(successMaxCount)
	var message string
	switch scope {
	case modelRateLimitTotalDenied:
		limit = int64(totalMaxCount)
		message = i18n.T(c, i18n.MsgRateLimitTotalReached, map[string]any{"Minutes": setting.ModelRequestRateLimitDurationMinutes, "Max": totalMaxCount})
	case modelRateLimitOrganizationDenied:
		layer = service.RateLimitScopeOrganization
		limit = int64(organizationMax)
		message = i18n.T(c, i18n.MsgRateLimitOrganizationReached, map[string]any{"Minutes": setting.ModelRequestRateLimitDurationMinutes, "Max": organizationMax})
	default:
		message = i18n.T(c, i18n.MsgRateLimitReached, map[string]any{"Minutes": setting.ModelRequestRateLimitDurationMinutes, "Max": successMaxCount})
	}
	setRateLimitHeaders(c, limit)
	service.RecordRateLimitRefusal(c, layer, "rate_limited", message)
	abortWithRateLimitedScope(c, http.StatusTooManyRequests, message, "", layer)
}

// Redis限流处理器
func redisRateLimitHandler(subject string, duration int64, totalMaxCount, successMaxCount int) gin.HandlerFunc {
	return redisModelRateLimitHandler(subject, duration, totalMaxCount, successMaxCount, 0, 0)
}

func redisModelRateLimitHandler(subject string, duration int64, totalMaxCount, successMaxCount int, organizationId int, organizationMax int) gin.HandlerFunc {
	return func(c *gin.Context) {
		reservation, scope, retryAfter, err := reserveRedisModelRequestWithOrganization(common.RDB, subject, duration, totalMaxCount, successMaxCount, organizationId, organizationMax, time.Now())
		if err != nil {
			fmt.Println(common.LogText("failed to reserve the model request limit: %s", err.Error()))
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
			return
		}
		if scope != modelRateLimitAllowed {
			abortModelRateLimited(c, scope, retryAfter, totalMaxCount, successMaxCount, organizationMax)
			return
		}
		if reservation.headerLimit > 0 {
			service.SetRateLimitRemaining(c, reservation.headerLimit, reservation.headerRemaining)
		}
		defer settleModelRateLimitReservation(c, reservation)
		c.Next()
	}
}

// 内存限流处理器
func memoryRateLimitHandler(subject string, duration int64, totalMaxCount, successMaxCount int) gin.HandlerFunc {
	return memoryModelRateLimitHandler(subject, duration, totalMaxCount, successMaxCount, 0, 0)
}

func memoryModelRateLimitHandler(subject string, duration int64, totalMaxCount, successMaxCount int, organizationId int, organizationMax int) gin.HandlerFunc {
	inMemoryRateLimiter.Init(time.Duration(setting.ModelRequestRateLimitDurationMinutes) * time.Minute)

	return func(c *gin.Context) {
		reservation, scope, retryAfter := reserveMemoryModelRequestWithOrganization(subject, duration, totalMaxCount, successMaxCount, organizationId, organizationMax)
		if scope != modelRateLimitAllowed {
			abortModelRateLimited(c, scope, retryAfter, totalMaxCount, successMaxCount, organizationMax)
			return
		}
		if reservation.headerLimit > 0 {
			service.SetRateLimitRemaining(c, reservation.headerLimit, reservation.headerRemaining)
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
//
// It applies the user limits, which are the global defaults unless an administrator
// overrides them for this user, and the total limit of the organization that pays for
// the token when that organization has one. An override or organization limit
// applies even when the global user limit is off.
func ModelRequestRateLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		totalMaxCount, successMaxCount := service.EffectiveUserRateLimits(userId)
		organizationId := common.GetContextKeyInt(c, constant.ContextKeyTokenBillingOrganizationId)
		organizationMax := 0
		if organizationId > 0 {
			organizationMax = service.OrganizationTotalRateLimit(organizationId)
		}
		if totalMaxCount == 0 && successMaxCount == 0 && organizationMax == 0 {
			c.Next()
			return
		}

		duration := rateLimitDurationSeconds(setting.ModelRequestRateLimitDurationMinutes)
		subject := strconv.Itoa(userId)

		// 根据存储类型选择并执行限流处理器
		if common.RedisEnabled {
			redisModelRateLimitHandler(subject, duration, totalMaxCount, successMaxCount, organizationId, organizationMax)(c)
		} else {
			memoryModelRateLimitHandler(subject, duration, totalMaxCount, successMaxCount, organizationId, organizationMax)(c)
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
