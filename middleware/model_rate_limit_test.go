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

	first, scope, err := reserveRedisModelRequest(redisClient, "tz-subject", 60, 0, 1, time.Now())
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope)
	first.commit(true)

	_, scope, err = reserveRedisModelRequest(redisClient, "tz-subject", 60, 0, 1, time.Now())
	require.NoError(t, err)
	assert.Equal(t, modelRateLimitSuccessDenied, scope, "a recorded success inside the window must count on a non-UTC host")
}

// A refused reservation must leave no trace: the in-flight set and the admission
// bucket are unchanged by the refusal itself.
func TestModelRedisReservationRefusalWritesNothing(t *testing.T) {
	_, redisClient := useRateLimitMiniRedis(t)
	ctx := context.Background()
	now := time.Now()

	held, scope, err := reserveRedisModelRequest(redisClient, "refusal-subject", 60, 1, 5, now)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope)
	assert.Equal(t, int64(1), redisClient.ZCard(ctx, held.inflightKey).Val())

	_, scope, err = reserveRedisModelRequest(redisClient, "refusal-subject", 60, 1, 5, now)
	require.NoError(t, err)
	assert.Equal(t, modelRateLimitTotalDenied, scope)
	assert.Equal(t, int64(1), redisClient.ZCard(ctx, held.inflightKey).Val(), "the refused request must not hold a success slot")

	// Releasing the held request returns its admission token.
	held.release()
	assert.Equal(t, int64(0), redisClient.ZCard(ctx, held.inflightKey).Val())
	again, scope, err := reserveRedisModelRequest(redisClient, "refusal-subject", 60, 1, 5, now)
	require.NoError(t, err)
	assert.Equal(t, modelRateLimitAllowed, scope, "a released admission must be available again")
	again.commit(false)
}

// A request that reached the upstream keeps its admission even when it fails,
// but a failure does not use up the success limit.
func TestModelRedisReservationCommitFailureCountsAdmissionOnly(t *testing.T) {
	_, redisClient := useRateLimitMiniRedis(t)
	now := time.Now()

	first, scope, err := reserveRedisModelRequest(redisClient, "failure-subject", 60, 2, 1, now)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope)
	first.commit(false)

	second, scope, err := reserveRedisModelRequest(redisClient, "failure-subject", 60, 2, 1, now)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope, "a failed request must not use the success limit")
	second.commit(false)

	_, scope, err = reserveRedisModelRequest(redisClient, "failure-subject", 60, 2, 1, now)
	require.NoError(t, err)
	assert.Equal(t, modelRateLimitTotalDenied, scope, "both failed requests keep their admission in the total")
}

// An in-flight reservation whose process died is released after the in-flight TTL.
func TestModelRedisReservationPrunesAbandonedInflight(t *testing.T) {
	_, redisClient := useRateLimitMiniRedis(t)
	start := time.Now()

	_, scope, err := reserveRedisModelRequest(redisClient, "abandon-subject", 60, 0, 1, start)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope)

	_, scope, err = reserveRedisModelRequest(redisClient, "abandon-subject", 60, 0, 1, start)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitSuccessDenied, scope, "an in-flight reservation holds the success slot")

	later := start.Add(time.Duration(modelRateLimitInflightTTLSeconds+1) * time.Second)
	_, scope, err = reserveRedisModelRequest(redisClient, "abandon-subject", 60, 0, 1, later)
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
			reservation, scope, err := reserveRedisModelRequest(client, subject, 60, 0, successLimit, time.Now())
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
	_, scope, err := reserveRedisModelRequest(client, subject, 60, 0, successLimit, time.Now())
	require.NoError(t, err)
	assert.Equal(t, modelRateLimitSuccessDenied, scope, "the recorded successes keep the limit closed")
}

// A memory reservation that is released gives back its admission, and one
// that is committed as a failure keeps it.
func TestModelMemoryReservationReleaseAndCommitAdmission(t *testing.T) {
	require.NoError(t, i18n.Init())
	subject := strconv.Itoa(7200000 + int(modelRateLimitTestUsers.Add(1)))
	inMemoryRateLimiter.Init(time.Duration(setting.ModelRequestRateLimitDurationMinutes) * time.Minute)

	released, scope := reserveMemoryModelRequest(subject, 60, 1, 0)
	require.Equal(t, modelRateLimitAllowed, scope)
	released.release()
	again, scope := reserveMemoryModelRequest(subject, 60, 1, 0)
	require.Equal(t, modelRateLimitAllowed, scope, "a released admission must be available again")
	again.commit(false)

	_, scope = reserveMemoryModelRequest(subject, 60, 1, 0)
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
				"message": common.MessageWithRequestId(message, "req-limited"),
				"type":    "rate_limit_error",
				"code":    "",
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
			reservation, scope, err := reserveRedisModelRequest(redisClient, subject, 60, 1, 1, time.Now())
			require.NoError(t, err)
			require.Equal(t, modelRateLimitAllowed, scope)

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tc.reached {
				MarkModelRateLimitUpstreamReached(c)
			}
			c.Status(tc.status)
			settleModelRateLimitReservation(c, reservation)

			_, next, err := reserveRedisModelRequest(redisClient, subject, 60, 1, 1, time.Now())
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
			assert.Equal(t, "60", resp.Header().Get("Retry-After"), "a refusal carries the window as its Retry-After")
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

	first, scope, err := reserveRedisModelRequestWithOrganization(client, "org-atomic", 60, 5, 0, organizationId, 1, now)
	require.NoError(t, err)
	require.Equal(t, modelRateLimitAllowed, scope)
	assert.Equal(t, "240", client.HGet(ctx, first.totalKey, "tokens").Val(), "one admission leaves 4 of 5 user tokens")
	assert.Equal(t, "0", client.HGet(ctx, first.orgKey, "tokens").Val(), "the organization's one token is spent")

	_, scope, err = reserveRedisModelRequestWithOrganization(client, "org-atomic", 60, 5, 0, organizationId, 1, now)
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

// A group with its own limit counts each user's requests on its own; groups
// without one keep sharing the user's global limit.
func TestModelRateLimitCountsConfiguredGroupsSeparately(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousEnabled, previousMinutes := setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes
	previousTotal, previousSuccess := setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount
	previousGroups, previousRedisEnabled := setting.ModelRequestRateLimitGroup, common.RedisEnabled
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = previousEnabled, previousMinutes
		setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = previousTotal, previousSuccess
		setting.ModelRequestRateLimitGroup, common.RedisEnabled = previousGroups, previousRedisEnabled
	})
	setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = true, 1
	common.RedisEnabled = false

	// Groups a and b have their own limits; c and d have none and share the global one.
	steps := []struct {
		group string
		want  int
	}{
		{"a", http.StatusOK}, {"a", http.StatusTooManyRequests},
		{"b", http.StatusOK}, {"b", http.StatusTooManyRequests},
		{"c", http.StatusOK}, {"d", http.StatusTooManyRequests},
	}
	// The Redis token bucket binds to the first Redis client it sees and another
	// test already drives it, so the total limit runs on the memory backend only.
	for _, tc := range []struct {
		backend, limit string
		counts         [2]int // [total, success] for groups a and b and for the global limit
	}{
		{"memory", "success", [2]int{0, 1}},
		{"redis", "success", [2]int{0, 1}},
		{"memory", "total", [2]int{1, 1000}},
	} {
		t.Run(tc.backend+"/"+tc.limit, func(t *testing.T) {
			if tc.backend == "redis" {
				useRateLimitMiniRedis(t)
			}
			setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = tc.counts[0], tc.counts[1]
			setting.ModelRequestRateLimitGroup = map[string][2]int{"a": tc.counts, "b": tc.counts}
			userID := 7200000 + int(modelRateLimitTestUsers.Add(1))
			router := gin.New()
			router.GET("/:group", func(c *gin.Context) {
				c.Set("id", userID)
				common.SetContextKey(c, constant.ContextKeyTokenGroup, c.Param("group"))
			}, ModelRequestRateLimit(), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			for _, step := range steps {
				assert.Equal(t, step.want, performRateLimitRequest(router, "/"+step.group, "127.0.0.1:1000").Code, "group %s", step.group)
			}
		})
	}
}
