package ratelimit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type fakeScriptRunner struct {
	result interface{}
	err    error
	keys   []string
	args   []interface{}
}

func (r *fakeScriptRunner) Eval(ctx context.Context, _ string, keys []string, args ...interface{}) *redis.Cmd {
	r.keys = keys
	r.args = args
	cmd := redis.NewCmd(ctx)
	if r.err != nil {
		cmd.SetErr(r.err)
		return cmd
	}
	cmd.SetVal(r.result)
	return cmd
}

func TestRedisSlidingWindowLimiterAllowsRequest(t *testing.T) {
	runner := &fakeScriptRunner{result: []interface{}{int64(1), int64(0)}}
	limiter := NewRedisSlidingWindowLimiter(runner)

	retryAfter, err := limiter.AcquireKey(t.Context(), "monitor_target:1", 10, time.Second)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if retryAfter != 0 {
		t.Errorf("Acquire() retryAfter = %v, want 0", retryAfter)
	}
	if len(runner.keys) != 1 || runner.keys[0] != "monitor_target:1" {
		t.Errorf("script keys = %#v, want monitor_target:1", runner.keys)
	}
	if len(runner.args) != 3 || runner.args[0] != time.Second.Microseconds() || runner.args[1] != 10 {
		t.Errorf("script args = %#v, want window and limit", runner.args)
	}
	if !strings.Contains(slidingWindowScript, "ZREMRANGEBYSCORE") || !strings.Contains(slidingWindowScript, "ZADD") {
		t.Error("rate limiter script must atomically remove expired entries and add the new entry")
	}
}

func TestRedisSlidingWindowLimiterReturnsRetryAfter(t *testing.T) {
	limiter := NewRedisSlidingWindowLimiter(&fakeScriptRunner{result: []interface{}{int64(0), int64(125000)}})

	retryAfter, err := limiter.AcquireKey(t.Context(), "monitor_target:1", 10, time.Second)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if retryAfter != 125*time.Millisecond {
		t.Errorf("Acquire() retryAfter = %v, want %v", retryAfter, 125*time.Millisecond)
	}
}

func TestRedisSlidingWindowLimiterReturnsRedisErrors(t *testing.T) {
	want := errors.New("Redis unavailable")
	limiter := NewRedisSlidingWindowLimiter(&fakeScriptRunner{err: want})

	_, err := limiter.AcquireKey(t.Context(), "monitor_target:1", 10, time.Second)
	if !errors.Is(err, want) {
		t.Fatalf("Acquire() error = %v, want wrapped %v", err, want)
	}
}
