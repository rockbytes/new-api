package middleware

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var modelRateLimitTestUsers atomic.Int64

func TestModelRequestRateLimitRules(t *testing.T) {
	require.NoError(t, i18n.Init())
	type step struct {
		path string
		ok   bool
		code string
	}
	for _, tc := range []struct {
		name     string
		defaults [2]int
		override *model.UserModelRateLimit
		groups   map[string][2]int
		steps    []step
	}{
		{"total across groups and keys", [2]int{2, 0}, nil, map[string][2]int{"a": {1, 0}, "b": {2, 0}}, []step{
			{"/a/1", true, ""}, {"/a/2", false, "group_rate_limit_total"}, {"/b/1", true, ""},
			{"/other/1", false, "user_rate_limit_total"}, {"/a/1", false, "user_rate_limit_total"},
		}},
		{"custom limits can raise or lower defaults", [2]int{1, 10}, &model.UserModelRateLimit{Total: 3, Success: 2}, nil, []step{
			{"/a/1", true, ""}, {"/b/2", true, ""}, {"/a/3", false, "user_rate_limit_success"},
		}},
		{"zero override is not inheritance", [2]int{1, 1}, &model.UserModelRateLimit{Total: 0, Success: 0}, map[string][2]int{"a": {2, 0}}, []step{
			{"/a/1", true, ""}, {"/a/2", true, ""}, {"/a/1", false, "group_rate_limit_total"}, {"/other/1", true, ""},
		}},
		{"stream failure not counted as success", [2]int{0, 1}, nil, map[string][2]int{"a": {0, 1}}, []step{
			{"/a/failed", true, ""}, {"/a/1", true, ""}, {"/other/1", false, "user_rate_limit_success"},
		}},
		{"failed requests still count towards total", [2]int{1, 1}, nil, nil, []step{
			{"/a/failed", true, ""}, {"/a/1", false, "user_rate_limit_total"},
		}},
		{"explicit unmatched group does not fall back", [2]int{}, nil, map[string][2]int{"account": {0, 1}}, []step{
			{"/unset/1", true, ""}, {"/unset/2", false, "group_rate_limit_success"}, {"/other/1", true, ""},
		}},
	} {
		for _, backend := range []string{"memory", "redis"} {
			t.Run(tc.name+"/"+backend, func(t *testing.T) {
				if backend == "redis" {
					useModelRateLimitRedis(t)
				} else {
					previous := common.RedisEnabled
					common.RedisEnabled = false
					t.Cleanup(func() { common.RedisEnabled = previous })
				}
				router := modelRateLimitTestRouter(t, tc.defaults, tc.groups, tc.override, nil)
				for _, step := range tc.steps {
					response := performRateLimitRequest(router, step.path, "127.0.0.1:1000")
					if step.ok {
						assert.Equal(t, http.StatusOK, response.Code, step.path)
					} else {
						assert.Equal(t, http.StatusTooManyRequests, response.Code, step.path)
						assert.Contains(t, response.Body.String(), `"code":"`+step.code+`"`)
						assert.Contains(t, response.Body.String(), `"type":"rate_limit_error"`)
					}
				}
			})
		}
	}
}

func TestModelRequestRateLimitReservesSuccessAcrossGroups(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			var client *redis.Client
			if backend == "redis" {
				original := useModelRateLimitRedis(t)
				client = redis.NewClient(original.Options())
				t.Cleanup(func() { require.NoError(t, client.Close()) })
			} else {
				previous := common.RedisEnabled
				common.RedisEnabled = false
				t.Cleanup(func() { common.RedisEnabled = previous })
			}
			entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan int, 1)
			router := modelRateLimitTestRouter(t, [2]int{2, 1}, map[string][2]int{"a": {1, 1}, "b": {1, 1}}, nil, func(c *gin.Context) {
				if c.Param("key") == "failed" {
					close(entered)
					<-release
				}
			})
			go func() { finished <- performRateLimitRequest(router, "/a/failed", "127.0.0.1:1000").Code }()
			select {
			case <-entered:
			case code := <-finished:
				t.Fatalf("first request did not reach handler: HTTP %d", code)
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("first request did not reach handler")
			}
			if client != nil {
				// The first request is already holding its slots on the original client.
				common.RDB = client
			}
			blocked := performRateLimitRequest(router, "/b/1", "127.0.0.1:1000")
			assert.Equal(t, http.StatusTooManyRequests, blocked.Code)
			assert.Contains(t, blocked.Body.String(), `"code":"user_rate_limit_success"`)
			close(release)
			require.Equal(t, http.StatusOK, <-finished)
			require.Equal(t, http.StatusOK, performRateLimitRequest(router, "/b/2", "127.0.0.1:1000").Code)
		})
	}
}

// TEST_MODEL_RATE_LIMIT_REDIS_ADDR may point to a disposable test instance.
func useModelRateLimitRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("TEST_MODEL_RATE_LIMIT_REDIS_ADDR")
	if addr == "" {
		_, client := useRateLimitMiniRedis(t)
		return client
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	require.NoError(t, client.Ping(t.Context()).Err())
	previousEnabled, previousClient := common.RedisEnabled, common.RDB
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = previousEnabled, previousClient
		require.NoError(t, client.Close())
	})
	return client
}

func modelRateLimitTestRouter(t *testing.T, defaults [2]int, groups map[string][2]int, override *model.UserModelRateLimit, onAdmit func(*gin.Context)) *gin.Engine {
	t.Helper()
	previousEnabled, previousMinutes := setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes
	previousTotal, previousSuccess := setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount
	previousGroups := setting.ModelRequestRateLimitGroup
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = previousEnabled, previousMinutes
		setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = previousTotal, previousSuccess
		setting.ModelRequestRateLimitGroup = previousGroups
	})
	setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = true, 1
	setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = defaults[0], defaults[1]
	setting.ModelRequestRateLimitGroup = groups
	userID := 7200000 + int(modelRateLimitTestUsers.Add(1))
	router := gin.New()
	router.GET("/:group/:key", func(c *gin.Context) {
		c.Set("id", userID)
		common.SetContextKey(c, constant.ContextKeyUserGroup, "account")
		if c.Param("group") != "unset" {
			common.SetContextKey(c, constant.ContextKeyTokenGroup, c.Param("group"))
		}
		c.Set("token_id", strconv.Itoa(userID)+":"+c.Param("key"))
	}, modelRequestRateLimit(func(context.Context, int) (*model.UserModelRateLimit, error) { return override, nil }), func(c *gin.Context) {
		if onAdmit != nil {
			onAdmit(c)
		}
		if c.Param("key") == "failed" {
			status := relaycommon.NewStreamStatus()
			status.MarkFailed("server_error", "", 0)
			common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
		}
		c.Status(http.StatusOK)
	})
	return router
}
