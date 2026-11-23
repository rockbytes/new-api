package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelRedisReservationIgnoresLocalTimezone(t *testing.T) {
	_, redisClient := useRateLimitMiniRedis(t)
	previousLocation := time.Local
	time.Local = time.FixedZone("test-utc-plus-eight", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocation })

	first, scope, _, err := reserveRedisModelRequest(redisClient, "tz-subject", 60, 0, 1, time.Now())
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope)
	first.commit(true)

	_, scope, _, err = reserveRedisModelRequest(redisClient, "tz-subject", 60, 0, 1, time.Now())
	require.NoError(t, err)
	assert.Equal(t, modelRateLimitSuccessDenied, scope, "a recorded success inside the window must count on a non-UTC host")
}

// A refused reservation must leave no trace: the in-flight set and the admission
// bucket are unchanged by the refusal itself.
func TestModelRedisReservationRefusalWritesNothing(t *testing.T) {
	_, redisClient := useRateLimitMiniRedis(t)
	ctx := context.Background()
	now := time.Now()

	held, scope, _, err := reserveRedisModelRequest(redisClient, "refusal-subject", 60, 1, 5, now)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope)
	assert.Equal(t, int64(1), redisClient.ZCard(ctx, held.inflightKey).Val())

	_, scope, _, err = reserveRedisModelRequest(redisClient, "refusal-subject", 60, 1, 5, now)
	require.NoError(t, err)
	assert.Equal(t, modelRateLimitTotalDenied, scope)
	assert.Equal(t, int64(1), redisClient.ZCard(ctx, held.inflightKey).Val(), "the refused request must not hold a success slot")

	// Releasing the held request returns its admission token.
	held.release()
	assert.Equal(t, int64(0), redisClient.ZCard(ctx, held.inflightKey).Val())
	again, scope, _, err := reserveRedisModelRequest(redisClient, "refusal-subject", 60, 1, 5, now)
	require.NoError(t, err)
	assert.Equal(t, modelRateLimitAllowed, scope, "a released admission must be available again")
	again.commit(false)
}

// A request that reached the upstream keeps its admission even when it fails,
// but a failure does not use up the success limit.
func TestModelRedisReservationCommitFailureCountsAdmissionOnly(t *testing.T) {
	_, redisClient := useRateLimitMiniRedis(t)
	now := time.Now()

	first, scope, _, err := reserveRedisModelRequest(redisClient, "failure-subject", 60, 2, 1, now)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope)
	first.commit(false)

	second, scope, _, err := reserveRedisModelRequest(redisClient, "failure-subject", 60, 2, 1, now)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope, "a failed request must not use the success limit")
	second.commit(false)

	_, scope, _, err = reserveRedisModelRequest(redisClient, "failure-subject", 60, 2, 1, now)
	require.NoError(t, err)
	assert.Equal(t, modelRateLimitTotalDenied, scope, "both failed requests keep their admission in the total")
}

// An in-flight reservation whose process died is released after the in-flight TTL.
func TestModelRedisReservationPrunesAbandonedInflight(t *testing.T) {
	_, redisClient := useRateLimitMiniRedis(t)
	start := time.Now()

	_, scope, _, err := reserveRedisModelRequest(redisClient, "abandon-subject", 60, 0, 1, start)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope)

	_, scope, _, err = reserveRedisModelRequest(redisClient, "abandon-subject", 60, 0, 1, start)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitSuccessDenied, scope, "an in-flight reservation holds the success slot")

	later := start.Add(time.Duration(modelRateLimitInflightTTLSeconds+1) * time.Second)
	_, scope, _, err = reserveRedisModelRequest(redisClient, "abandon-subject", 60, 0, 1, later)
	require.NoError(t, err)
	assert.Equal(t, modelRateLimitAllowed, scope, "an abandoned reservation must expire")
}

// Many concurrent requests must admit exactly the success limit, counting the
// reservations that are still running, not only the recorded successes.
func TestModelRedisReservationAdmitsExactlyTheLimitUnderConcurrency(t *testing.T) {
	_, redisClient := useRateLimitMiniRedis(t)
	runConcurrentRedisReservation(t, redisClient, "concurrent-subject")
}

// The same concurrency check against a real Redis server. It is skipped unless
// NEW_API_TEST_REDIS_ADDR names one. The test uses database 15 and flushes only that database.
func TestModelRedisReservationAdmitsExactlyTheLimitAgainstRealRedis(t *testing.T) {
	addr := os.Getenv("NEW_API_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set NEW_API_TEST_REDIS_ADDR (for example 127.0.0.1:6379) to run the reservation check against a real Redis")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.FlushDB(context.Background()).Err())
	previousRDB := common.RDB
	common.RDB = client
	t.Cleanup(func() { common.RDB = previousRDB })
	runConcurrentRedisReservation(t, client, "real-redis-subject")
}

func runConcurrentRedisReservation(t *testing.T, client *redis.Client, subject string) {
	t.Helper()
	const successLimit = 50
	const attempts = 300
	var admitted atomic.Int64
	var wg sync.WaitGroup
	reservations := make(chan *redisModelRateLimitReservation, attempts)
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reservation, scope, _, err := reserveRedisModelRequest(client, subject, 60, 0, successLimit, time.Now())
			if err != nil {
				t.Errorf("reserve failed: %v", err)
				return
			}
			if scope == modelRateLimitAllowed {
				admitted.Add(1)
				reservations <- reservation
			}
		}()
	}
	wg.Wait()
	close(reservations)
	assert.Equal(t, int64(successLimit), admitted.Load(), "admitted requests must equal the success limit exactly")
	for reservation := range reservations {
		reservation.commit(true)
	}
	_, scope, _, err := reserveRedisModelRequest(client, subject, 60, 0, successLimit, time.Now())
	require.NoError(t, err)
	assert.Equal(t, modelRateLimitSuccessDenied, scope, "the recorded successes keep the limit closed")
}

// A memory reservation that is released gives back its admission, and one
// that is committed as a failure keeps it.
func TestModelMemoryReservationReleaseAndCommitAdmission(t *testing.T) {
	require.NoError(t, i18n.Init())
	subject := strconv.Itoa(7200000 + int(modelRateLimitTestUsers.Add(1)))
	inMemoryRateLimiter.Init(time.Duration(setting.ModelRequestRateLimitDurationMinutes) * time.Minute)

	released, scope, _ := reserveMemoryModelRequest(subject, 60, 1, 0)
	require.Equal(t, modelRateLimitAllowed, scope)
	released.release()
	again, scope, _ := reserveMemoryModelRequest(subject, 60, 1, 0)
	require.Equal(t, modelRateLimitAllowed, scope, "a released admission must be available again")
	again.commit(false)

	_, scope, _ = reserveMemoryModelRequest(subject, 60, 1, 0)
	assert.Equal(t, modelRateLimitTotalDenied, scope, "a committed failure keeps its admission")
}

var modelRateLimitTestUsers atomic.Int64

func TestModelRateLimitStreamFailuresDoNotConsumeSuccessLimit(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, backend := range []string{"memory", "redis"} {
		for _, totalLimit := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/total=%d", backend, totalLimit), func(t *testing.T) {
				subject := strconv.Itoa(7200000 + int(modelRateLimitTestUsers.Add(1)))
				handler := memoryRateLimitHandler(subject, 60, totalLimit, 1)
				if backend == "redis" {
					useRateLimitMiniRedis(t)
					handler = redisRateLimitHandler(subject, 60, totalLimit, 1)
				}
				router := gin.New()
				router.GET("/:outcome", handler, func(c *gin.Context) {
					status := relaycommon.NewStreamStatus()
					if c.Param("outcome") == "failed" {
						status.MarkFailed("server_error", "", 0)
					} else {
						status.MarkCompleted()
					}
					common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
					c.Status(http.StatusOK)
				})
				assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
				if totalLimit > 0 {
					assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
				} else {
					assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
				}
				assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
			})
		}
	}
}

func TestModelMemoryRateLimitReservesConcurrentSuccessAdmission(t *testing.T) {
	require.NoError(t, i18n.Init())
	subject := strconv.Itoa(7200000 + int(modelRateLimitTestUsers.Add(1)))
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	router := gin.New()
	router.GET("/:outcome", memoryRateLimitHandler(subject, 60, 0, 1), func(c *gin.Context) {
		if c.Param("outcome") == "failed" {
			close(entered)
			<-release
			status := relaycommon.NewStreamStatus()
			status.MarkFailed("server_error", "", 0)
			common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
		}
		c.Status(http.StatusOK)
	})
	go func() {
		defer close(finished)
		assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
	}()
	<-entered
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
	close(release)
	<-finished
	assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
}

// The memory limiter answers like the Redis limiter: an OpenAI error body
// naming the limit that was reached, not an empty 429.
func TestModelMemoryRateLimitRejectionsReturnOpenAIError(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, tc := range []struct {
		name                     string
		totalLimit, successLimit int
		messageKey               string
	}{
		{name: "total", totalLimit: 1, messageKey: i18n.MsgRateLimitTotalReached},
		{name: "success", successLimit: 1, messageKey: i18n.MsgRateLimitReached},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subject := strconv.Itoa(7200000 + int(modelRateLimitTestUsers.Add(1)))
			router := gin.New()
			router.GET("/", func(c *gin.Context) {
				c.Set(common.RequestIdKey, "req-limited")
			}, memoryRateLimitHandler(subject, 60, tc.totalLimit, tc.successLimit), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})
			require.Equal(t, http.StatusOK, performRateLimitRequest(router, "/", "127.0.0.1:1000").Code)

			resp := performRateLimitRequest(router, "/", "127.0.0.1:1000")
			assert.Equal(t, http.StatusTooManyRequests, resp.Code)
			message := i18n.Translate("", tc.messageKey, map[string]any{"Minutes": setting.ModelRequestRateLimitDurationMinutes, "Max": 1})
			want, err := common.Marshal(gin.H{"error": gin.H{
				"message":            common.MessageWithRequestId(message, "req-limited"),
				"type":               "rate_limit_error",
				"rate_limited_scope": "user",
				"code":               "",
			}})
			require.NoError(t, err)
			assert.JSONEq(t, string(want), resp.Body.String())
		})
	}
}

// The settlement decision: a success counts toward both limits, an upstream
// failure keeps only its admission, and a request rejected before the upstream
// (for example by the channel budget) returns its admission.
func TestModelRateLimitSettlementDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		status   int
		reached  bool
		wantNext modelRateLimitScope
	}{
		{name: "success counts both limits", status: http.StatusOK, reached: true, wantNext: modelRateLimitSuccessDenied},
		{name: "upstream failure keeps its admission", status: http.StatusBadGateway, reached: true, wantNext: modelRateLimitTotalDenied},
		{name: "rejection before the upstream releases", status: http.StatusTooManyRequests, reached: false, wantNext: modelRateLimitAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, redisClient := useRateLimitMiniRedis(t)
			subject := "settle-" + tc.name
			reservation, scope, _, err := reserveRedisModelRequest(redisClient, subject, 60, 1, 1, time.Now())
			require.NoError(t, err)
			require.Equal(t, modelRateLimitAllowed, scope)

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tc.reached {
				MarkModelRateLimitUpstreamReached(c)
			}
			c.Status(tc.status)
			settleModelRateLimitReservation(c, reservation)

			_, next, _, err := reserveRedisModelRequest(redisClient, subject, 60, 1, 1, time.Now())
			require.NoError(t, err)
			assert.Equal(t, tc.wantNext, next)
		})
	}
}

// An organization's total limit applies even when the user limits are off, and
// a request refused by it is reported with the organization message.
func TestOrganizationTotalLimitAppliesWithoutUserLimits(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousEnabled, previousRedis := setting.ModelRequestRateLimitEnabled, common.RedisEnabled
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled, common.RedisEnabled = previousEnabled, previousRedis
		service.ReplaceOrganizationTotalRateLimits(nil)
	})
	setting.ModelRequestRateLimitEnabled = false

	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			if backend == "redis" {
				useRateLimitMiniRedis(t)
			} else {
				common.RedisEnabled = false
			}
			organizationId := 7300000 + int(modelRateLimitTestUsers.Add(1))
			service.ReplaceOrganizationTotalRateLimits(map[int]int{organizationId: 2})
			router := gin.New()
			router.GET("/", func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyTokenBillingOrganizationId, organizationId)
			}, ModelRequestRateLimit(), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/", "127.0.0.1:1000").Code)
			assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/", "127.0.0.1:1000").Code)
			resp := performRateLimitRequest(router, "/", "127.0.0.1:1000")
			assert.Equal(t, http.StatusTooManyRequests, resp.Code)
			assert.Contains(t, resp.Body.String(), "organization", "the refusal must name the organization limit")
			retryAfter, err := strconv.Atoi(resp.Header().Get("Retry-After"))
			require.NoError(t, err)
			if backend == "redis" {
				assert.Equal(t, 30, retryAfter, "a token bucket of 2 requests per 60 seconds refills one request in 30 seconds")
			} else {
				assert.InDelta(t, 60, retryAfter, 1, "the sliding window frees a slot when its oldest request leaves the window")
			}
		})
	}
}

// A refused organization limit must not consume the user's total admission, and
// releasing the admission must give the organization its token back.
func TestOrganizationRefusalLeavesUserTotalUntouched(t *testing.T) {
	_, client := useRateLimitMiniRedis(t)
	organizationId := 7400000 + int(modelRateLimitTestUsers.Add(1))
	now := time.Now()
	ctx := context.Background()

	first, scope, _, err := reserveRedisModelRequestWithOrganization(client, "org-atomic", 60, 5, 0, organizationId, 1, now)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope)
	assert.Equal(t, "240", client.HGet(ctx, first.totalKey, "tokens").Val(), "one admission leaves 4 of 5 user tokens")
	assert.Equal(t, "0", client.HGet(ctx, first.orgKey, "tokens").Val(), "the organization's one token is spent")

	_, scope, _, err = reserveRedisModelRequestWithOrganization(client, "org-atomic", 60, 5, 0, organizationId, 1, now)
	require.NoError(t, err)
	assert.Equal(t, modelRateLimitOrganizationDenied, scope)
	assert.Equal(t, "240", client.HGet(ctx, first.totalKey, "tokens").Val(), "a refused organization limit must not spend a user token")

	first.release()
	assert.Equal(t, "60", client.HGet(ctx, first.orgKey, "tokens").Val(), "releasing returns the organization token")
	assert.Equal(t, "300", client.HGet(ctx, first.totalKey, "tokens").Val(), "releasing returns the user token")
}

// A request whose token may not use the model is refused with 403 even when the
// user's limit is already spent. The refusal takes no admission and is reported
// as a model refusal, not as a rate limit.
func TestTokenModelRefusalTakesNoAdmission(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousEnabled, previousMinutes := setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes
	previousTotal, previousRedisEnabled := setting.ModelRequestRateLimitCount, common.RedisEnabled
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = previousEnabled, previousMinutes
		setting.ModelRequestRateLimitCount, common.RedisEnabled = previousTotal, previousRedisEnabled
	})
	setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes, setting.ModelRequestRateLimitCount = true, 1, 1
	common.RedisEnabled = false

	userID := 7500000 + int(modelRateLimitTestUsers.Add(1))
	router := gin.New()
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("id", userID)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"allowed-model": true})
	}, TokenModelAccess(), ModelRequestRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	postModel := func(model string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"`+model+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:1000"
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp.Code
	}

	assert.Equal(t, http.StatusOK, postModel("allowed-model"), "the first request takes the only admission")
	assert.Equal(t, http.StatusForbidden, postModel("other-model"), "a model outside the token limit is refused as a model")
	assert.Equal(t, http.StatusTooManyRequests, postModel("allowed-model"), "the refusal left the user's limit as it was")
}

// A refusal by the user or organization limit names that layer in the error body and
// in the request's policy events, so administrators can see which limit refused it.
func TestModelRateLimitRefusalNamesItsLayer(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousEnabled, previousMinutes := setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes
	previousTotal, previousRedisEnabled := setting.ModelRequestRateLimitCount, common.RedisEnabled
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = previousEnabled, previousMinutes
		setting.ModelRequestRateLimitCount, common.RedisEnabled = previousTotal, previousRedisEnabled
		service.ReplaceOrganizationTotalRateLimits(nil)
	})
	setting.ModelRequestRateLimitDurationMinutes = 1
	common.RedisEnabled = false

	for _, tc := range []struct {
		name      string
		userLimit bool
		orgLimit  int
		wantScope string
	}{
		{name: "user", userLimit: true, wantScope: service.RateLimitScopeUser},
		{name: "organization", orgLimit: 1, wantScope: service.RateLimitScopeOrganization},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setting.ModelRequestRateLimitEnabled = tc.userLimit
			setting.ModelRequestRateLimitCount = 1
			userID := 7600000 + int(modelRateLimitTestUsers.Add(1))
			organizationID := 7700000 + int(modelRateLimitTestUsers.Add(1))
			if tc.orgLimit > 0 {
				service.ReplaceOrganizationTotalRateLimits(map[int]int{organizationID: tc.orgLimit})
			}
			var events []service.PolicyEvent
			router := gin.New()
			router.GET("/", func(c *gin.Context) {
				c.Set("id", userID)
				if tc.orgLimit > 0 {
					common.SetContextKey(c, constant.ContextKeyTokenBillingOrganizationId, organizationID)
				}
			}, func(c *gin.Context) {
				c.Next()
				events = service.RequestPolicy(c).Events()
			}, ModelRequestRateLimit(), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			require.Equal(t, http.StatusOK, performRateLimitRequest(router, "/", "127.0.0.1:1000").Code)
			resp := performRateLimitRequest(router, "/", "127.0.0.1:1000")
			require.Equal(t, http.StatusTooManyRequests, resp.Code)
			var body struct {
				Error struct {
					RateLimitedScope string `json:"rate_limited_scope"`
				} `json:"error"`
			}
			require.NoError(t, common.Unmarshal(resp.Body.Bytes(), &body))
			assert.Equal(t, tc.wantScope, body.Error.RateLimitedScope)
			assert.Equal(t, "1", resp.Header().Get("x-ratelimit-limit"), "the refusing limit is the configured maximum")
			assert.Equal(t, "0", resp.Header().Get("x-ratelimit-remaining"), "a refused window has nothing left")
			assert.Empty(t, resp.Header().Get("x-ratelimit-reset"), "a window length is not a wait time, so a user or organization refusal sets no reset")
			require.NotEmpty(t, events)
			last := events[len(events)-1]
			assert.Equal(t, tc.wantScope, last.RateLimitedScope)
			assert.Equal(t, "rate_limited", last.Decision.Reason)
		})
	}
}

// A refused channel budget reports the exact reset: the time to refill one request.
func TestChannelRateLimitHeadersCarryTheExactReset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	SetChannelRateLimitHeaders(c, 2, 30)

	assert.Equal(t, "2", c.Writer.Header().Get("x-ratelimit-limit"))
	assert.Equal(t, "0", c.Writer.Header().Get("x-ratelimit-remaining"))
	assert.Equal(t, "30", c.Writer.Header().Get("x-ratelimit-reset"))
}

// A token bucket refusal reports the exact time to refill the tokens the request needs.
// The memory limiter reports the time until its oldest request leaves the window.
func TestModelRateLimitRefusalReportsTheWait(t *testing.T) {
	_, client := useRateLimitMiniRedis(t)
	now := time.Now()
	// Two requests per 60 seconds refill one request every 30 seconds.
	for range 2 {
		_, scope, _, err := reserveRedisModelRequest(client, "wait-redis", 60, 2, 0, now)
		require.NoError(t, err)
		require.Equal(t, modelRateLimitAllowed, scope)
	}
	_, scope, retryAfter, err := reserveRedisModelRequest(client, "wait-redis", 60, 2, 0, now)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitTotalDenied, scope)
	assert.Equal(t, int64(30), retryAfter, "an empty bucket refills 2 tokens a second; a request needs 60")

	// 15 seconds later 30 tokens are back, so 15 more seconds are needed.
	_, scope, retryAfter, err = reserveRedisModelRequest(client, "wait-redis", 60, 2, 0, now.Add(15*time.Second))
	require.NoError(t, err)
	require.Equal(t, modelRateLimitTotalDenied, scope)
	assert.Equal(t, int64(15), retryAfter)

	inMemoryRateLimiter.Init(time.Minute)
	first, scope, _ := reserveMemoryModelRequest("wait-memory", 60, 1, 0)
	require.Equal(t, modelRateLimitAllowed, scope)
	first.commit(true)
	_, scope, retryAfter = reserveMemoryModelRequest("wait-memory", 60, 1, 0)
	require.Equal(t, modelRateLimitTotalDenied, scope)
	assert.InDelta(t, 60, retryAfter, 1, "the memory limiter frees the slot when the oldest request leaves the window")
}

// An admission reports the layer nearest its limit: the one with the fewest requests left.
func TestModelRateLimitAdmissionReportsTheTightestLayer(t *testing.T) {
	_, client := useRateLimitMiniRedis(t)
	organizationId := 7800000 + int(modelRateLimitTestUsers.Add(1))
	// The organization allows 2 requests a minute and the user 5, so the organization is nearer.
	reservation, scope, _, err := reserveRedisModelRequestWithOrganization(client, "tightest-redis", 60, 5, 0, organizationId, 2, time.Now())
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope)
	assert.Equal(t, int64(2), reservation.headerLimit)
	assert.Equal(t, int64(1), reservation.headerRemaining)

	inMemoryRateLimiter.Init(time.Minute)
	memory, scope, _ := reserveMemoryModelRequestWithOrganization("tightest-memory", 60, 5, 0, organizationId+1, 2)
	require.Equal(t, modelRateLimitAllowed, scope)
	assert.Equal(t, int64(2), memory.headerLimit)
	assert.Equal(t, int64(1), memory.headerRemaining)
}

// A request refused before its upstream, such as by quota pre-consume, gives its
// admission back. Otherwise the refused request would count toward the total limit.
func TestRequestRefusedBeforeUpstreamReleasesAdmission(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousEnabled, previousMinutes := setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes
	previousTotal, previousRedisEnabled := setting.ModelRequestRateLimitCount, common.RedisEnabled
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = previousEnabled, previousMinutes
		setting.ModelRequestRateLimitCount, common.RedisEnabled = previousTotal, previousRedisEnabled
	})
	setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes, setting.ModelRequestRateLimitCount = true, 1, 1
	common.RedisEnabled = false

	userID := 7500000 + int(modelRateLimitTestUsers.Add(1))
	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		c.Set("id", userID)
	}, ModelRequestRateLimit(), func(c *gin.Context) {
		MarkModelRateLimitUpstreamReached(c)
		if c.Query("refuse") == "1" {
			ClearModelRateLimitUpstreamReached(c)
			c.Status(http.StatusPaymentRequired)
			return
		}
		c.Status(http.StatusOK)
	})

	assert.Equal(t, http.StatusPaymentRequired, performRateLimitRequest(router, "/?refuse=1", "127.0.0.1:1000").Code)
	assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/", "127.0.0.1:1000").Code, "the refused request released the only admission")
}
