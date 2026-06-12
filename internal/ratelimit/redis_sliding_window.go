package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const slidingWindowScript = `
local server_time = redis.call('TIME')
local now = tonumber(server_time[1]) * 1000000 + tonumber(server_time[2])
local window = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - window)
local count = redis.call('ZCARD', KEYS[1])
if count >= limit then
  local oldest = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
  return {0, math.max(0, tonumber(oldest[2]) + window - now)}
end
redis.call('ZADD', KEYS[1], now, ARGV[3])
redis.call('PEXPIRE', KEYS[1], math.ceil(window / 1000))
return {1, 0}`

type scriptRunner interface {
	Eval(context.Context, string, []string, ...interface{}) *redis.Cmd
}
type RedisSlidingWindowLimiter struct{ client scriptRunner }

func NewRedisSlidingWindowLimiter(client scriptRunner) *RedisSlidingWindowLimiter {
	return &RedisSlidingWindowLimiter{client: client}
}
func (l *RedisSlidingWindowLimiter) AcquireKey(ctx context.Context, key string, limit int, window time.Duration) (time.Duration, error) {
	if l == nil || l.client == nil {
		return 0, fmt.Errorf("redis rate limiter is not configured")
	}
	if key == "" || limit < 1 || window <= 0 {
		return 0, fmt.Errorf("rate limit key, limit and window are required")
	}
	result, err := l.client.Eval(ctx, slidingWindowScript, []string{key}, window.Microseconds(), limit, uuid.NewString()).Result()
	if err != nil {
		return 0, fmt.Errorf("execute redis rate limiter script: %w", err)
	}
	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		return 0, fmt.Errorf("unexpected redis rate limiter response: %#v", result)
	}
	allowed, err := redisInteger(values[0])
	if err != nil {
		return 0, err
	}
	if allowed == 1 {
		return 0, nil
	}
	if allowed != 0 {
		return 0, fmt.Errorf("unexpected redis rate limiter decision: %d", allowed)
	}
	wait, err := redisInteger(values[1])
	if err != nil {
		return 0, err
	}
	if wait <= 0 {
		wait = 1
	}
	return time.Duration(wait) * time.Microsecond, nil
}
func redisInteger(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}
