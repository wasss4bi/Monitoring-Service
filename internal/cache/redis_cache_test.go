package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisClientStub struct {
	value    string
	getErr   error
	setErr   error
	setKey   string
	setValue string
	setTTL   time.Duration
	setNX    bool
}

func (c *redisClientStub) Get(context.Context, string) *redis.StringCmd {
	return redis.NewStringResult(c.value, c.getErr)
}

func (c *redisClientStub) Set(_ context.Context, key string, value interface{}, ttl time.Duration) *redis.StatusCmd {
	c.setKey = key
	c.setValue = value.(string)
	c.setTTL = ttl
	return redis.NewStatusResult("OK", c.setErr)
}

func (c *redisClientStub) SetNX(context.Context, string, interface{}, time.Duration) *redis.BoolCmd {
	return redis.NewBoolResult(c.setNX, c.setErr)
}

func (redisClientStub) Eval(context.Context, string, []string, ...interface{}) *redis.Cmd {
	return redis.NewCmdResult(int64(1), nil)
}

func TestRedisCacheGetReturnsCacheMiss(t *testing.T) {
	cache := NewRedisCache(&redisClientStub{getErr: redis.Nil})

	_, err := cache.Get(t.Context(), "monitor:example")
	if !errors.Is(err, ErrMiss) {
		t.Fatalf("Get() error = %v, want ErrMiss", err)
	}
}

func TestRedisCacheSetPassesValueAndTTL(t *testing.T) {
	client := &redisClientStub{}
	cache := NewRedisCache(client)

	if err := cache.Set(t.Context(), "key", "value", 15*time.Minute); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if client.setKey != "key" || client.setValue != "value" || client.setTTL != 15*time.Minute {
		t.Fatalf("Set() arguments = (%q, %q, %v), want (key, value, 15m)", client.setKey, client.setValue, client.setTTL)
	}
}

func TestRedisCacheSetNXReturnsAcquisitionResult(t *testing.T) {
	cache := NewRedisCache(&redisClientStub{setNX: true})

	acquired, err := cache.SetNX(t.Context(), "lock", "token", 30*time.Second)
	if err != nil {
		t.Fatalf("SetNX() error = %v", err)
	}
	if !acquired {
		t.Fatal("SetNX() = false, want true")
	}
}
