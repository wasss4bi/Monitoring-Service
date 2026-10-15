package ratelimit

import (
	"context"
	"fmt"
	"monitoring_service/internal/metrics"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type keyLimiter interface {
	AcquireKey(context.Context, string, int, time.Duration) (time.Duration, error)
}

type localPermit struct {
	id uint64
	at time.Time
}

// MonitoringLimiter preserves recent permits locally even while Redis is healthy.
// During an outage its cap is per process, not a global hostname guarantee.
type MonitoringLimiter struct {
	shared    keyLimiter
	mu        sync.Mutex
	sequence  uint64
	permits   map[string][]localPermit
	now       func() time.Time
	lastSweep time.Time
}

func NewMonitoringLimiter(shared keyLimiter) *MonitoringLimiter {
	return &MonitoringLimiter{shared: shared, permits: make(map[string][]localPermit), now: time.Now}
}

func (l *MonitoringLimiter) AcquireKey(ctx context.Context, key string, limit int, window time.Duration) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if key == "" || limit < 1 || window != time.Minute {
		return 0, fmt.Errorf("monitoring limiter requires a key, positive limit and one-minute window")
	}
	l.mu.Lock()
	now := l.now()
	if now.Sub(l.lastSweep) >= window {
		for k, items := range l.permits {
			i := 0
			for i < len(items) && !items[i].at.After(now.Add(-window)) {
				i++
			}
			if i == len(items) {
				delete(l.permits, k)
			} else {
				l.permits[k] = items[i:]
			}
		}
		l.lastSweep = now
	}
	items := l.permits[key]
	i := 0
	for i < len(items) && !items[i].at.After(now.Add(-window)) {
		i++
	}
	items = items[i:]
	if len(items) >= limit {
		metrics.FetcherEvent("hostname_limiter", "local_wait")
		l.permits[key] = items
		wait := items[0].at.Add(window).Sub(now)
		l.mu.Unlock()
		return wait, nil
	}
	l.sequence++
	id := l.sequence
	l.permits[key] = append(items, localPermit{id: id, at: now})
	l.mu.Unlock()
	if l.shared == nil {
		metrics.FetcherEvent("hostname_limiter", "local")
		return 0, nil
	}
	dependencyCtx, cancel := context.WithTimeout(ctx, 75*time.Millisecond)
	wait, err := l.shared.AcquireKey(dependencyCtx, key, limit, window)
	cancel()
	if ctx.Err() != nil || (err == nil && wait > 0) {
		l.mu.Lock()
		items = l.permits[key]
		for i, item := range items {
			if item.id == id {
				l.permits[key] = append(items[:i], items[i+1:]...)
				break
			}
		}
		l.mu.Unlock()
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return wait, nil
	}
	if err != nil {
		metrics.FetcherEvent("hostname_limiter", "local")
	} else {
		metrics.FetcherEvent("hostname_limiter", "shared")
	}
	// A failed Redis operation may already have granted a permit. Retaining the
	// local reservation bounds the fail-open path without repeating the script.
	return 0, nil
}

// NewMonitoringRedisClient disables automatic retries for monitoring requests.
func NewMonitoringRedisClient(redisURL string) (*redis.Client, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse monitoring Redis URL: %w", err)
	}
	options.MaxRetries = -1
	options.DialTimeout = 50 * time.Millisecond
	options.ReadTimeout = 50 * time.Millisecond
	options.WriteTimeout = 50 * time.Millisecond
	options.PoolTimeout = 50 * time.Millisecond
	options.ContextTimeoutEnabled = true
	return redis.NewClient(options), nil
}
