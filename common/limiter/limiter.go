package limiter

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

//go:embed lua/rate_limit.lua
var rateLimitScript string

type RedisLimiter struct {
	client         *redis.Client
	limitScriptSHA string
}

var (
	instanceMu sync.Mutex
	instance   *RedisLimiter
)

// New returns the limiter for the client r. The instance is kept for the client it was
// built with. A call with a different client loads the script on that client and
// replaces the instance, so a client that was replaced is never used again.
func New(ctx context.Context, r *redis.Client) *RedisLimiter {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	if instance != nil && instance.client == r {
		return instance
	}
	// 预加载脚本
	limitSHA, err := r.ScriptLoad(ctx, rateLimitScript).Result()
	if err != nil {
		common.SysLog(common.LogText("Failed to load rate limit script: %v", err))
	}
	instance = &RedisLimiter{
		client:         r,
		limitScriptSHA: limitSHA,
	}
	return instance
}

func (rl *RedisLimiter) Allow(ctx context.Context, key string, opts ...Option) (bool, error) {
	allowed, _, err := rl.AllowWithRemaining(ctx, key, opts...)
	return allowed, err
}

// AllowWithRemaining takes the request's tokens from the bucket and also returns how many
// whole requests the bucket still holds after the admission. The remaining count is 0 when
// the request is refused.
func (rl *RedisLimiter) AllowWithRemaining(ctx context.Context, key string, opts ...Option) (bool, int64, error) {
	// 默认配置
	config := &Config{
		Capacity:  10,
		Rate:      1,
		Requested: 1,
	}

	// 应用选项模式
	for _, opt := range opts {
		opt(config)
	}

	// 执行限流
	reply, err := rl.client.EvalSha(
		ctx,
		rl.limitScriptSHA,
		[]string{key},
		config.Requested,
		config.Rate,
		config.Capacity,
	).Int64Slice()

	if err != nil {
		if strings.HasPrefix(err.Error(), "NOSCRIPT ") {
			reply, err = rl.client.Eval(
				ctx,
				rateLimitScript,
				[]string{key},
				config.Requested,
				config.Rate,
				config.Capacity,
			).Int64Slice()
			if err != nil {
				return false, 0, fmt.Errorf("rate limit failed: %w", err)
			}
		} else {
			return false, 0, fmt.Errorf("rate limit failed: %w", err)
		}
	}
	if len(reply) != 2 {
		return false, 0, fmt.Errorf("rate limit failed: unexpected reply length %d", len(reply))
	}
	return reply[0] == 1, reply[1], nil
}

// Config 配置选项模式
type Config struct {
	Capacity  int64
	Rate      int64
	Requested int64
}

type Option func(*Config)

func WithCapacity(c int64) Option {
	return func(cfg *Config) { cfg.Capacity = c }
}

func WithRate(r int64) Option {
	return func(cfg *Config) { cfg.Rate = r }
}

func WithRequested(n int64) Option {
	return func(cfg *Config) { cfg.Requested = n }
}
