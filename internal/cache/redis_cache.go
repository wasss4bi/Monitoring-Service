package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrMiss = errors.New("cache miss")

type redisClient interface {
	Get(context.Context, string) *redis.StringCmd
	Set(context.Context, string, interface{}, time.Duration) *redis.StatusCmd
	SetNX(context.Context, string, interface{}, time.Duration) *redis.BoolCmd
	Eval(context.Context, string, []string, ...interface{}) *redis.Cmd
}

type RedisCache struct {
	client redisClient
}

func NewRedisCache(client redisClient) *RedisCache {
	return &RedisCache{client: client}
}

func (c *RedisCache) Get(ctx context.Context, key string) (string, error) {
	if c == nil || c.client == nil {
		return "", fmt.Errorf("redis cache is not configured")
	}

	value, err := c.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrMiss
	}
	if err != nil {
		return "", fmt.Errorf("get cache value: %w", err)
	}

	return value, nil
}

func (c *RedisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("redis cache is not configured")
	}

	if err := c.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("set cache value: %w", err)
	}

	return nil
}

func (c *RedisCache) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if c == nil || c.client == nil {
		return false, fmt.Errorf("redis cache is not configured")
	}

	acquired, err := c.client.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("set cache value if absent: %w", err)
	}

	return acquired, nil
}

func (c *RedisCache) Release(ctx context.Context, key, value string) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("redis cache is not configured")
	}

	if _, err := c.client.Eval(ctx, releaseLockScript, []string{key}, value).Result(); err != nil {
		return fmt.Errorf("release cache lock: %w", err)
	}

	return nil
}

const releaseLockScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`
