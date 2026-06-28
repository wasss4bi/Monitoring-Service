package ratelimit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type sharedLimitFunc func(context.Context, string, int, time.Duration) (time.Duration, error)

func (f sharedLimitFunc) AcquireKey(c context.Context, k string, l int, w time.Duration) (time.Duration, error) {
	return f(c, k, l, w)
}

func TestMonitoringLimiterRetainsHealthyPermitsDuringOutage(t *testing.T) {
	var outage atomic.Bool
	l := NewMonitoringLimiter(sharedLimitFunc(func(context.Context, string, int, time.Duration) (time.Duration, error) {
		if outage.Load() {
			return 0, errors.New("redis unavailable")
		}
		return 0, nil
	}))
	now := time.Unix(100, 0)
	l.now = func() time.Time { return now }
	for range 2 {
		if w, e := l.AcquireKey(t.Context(), "host", 3, time.Minute); e != nil || w != 0 {
			t.Fatal(w, e)
		}
	}
	outage.Store(true)
	if w, e := l.AcquireKey(t.Context(), "host", 3, time.Minute); e != nil || w != 0 {
		t.Fatal(w, e)
	}
	if w, e := l.AcquireKey(t.Context(), "host", 3, time.Minute); e != nil || w != time.Minute {
		t.Fatal(w, e)
	}
	now = now.Add(time.Minute)
	if w, e := l.AcquireKey(t.Context(), "host", 3, time.Minute); e != nil || w != 0 {
		t.Fatal(w, e)
	}
}

func TestMonitoringLimiterConcurrentCap(t *testing.T) {
	l := NewMonitoringLimiter(nil)
	var permitted atomic.Int32
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if w, e := l.AcquireKey(t.Context(), "host", 7, time.Minute); e == nil && w == 0 {
				permitted.Add(1)
			}
		})
	}
	wg.Wait()
	if permitted.Load() != 7 {
		t.Fatalf("permits=%d", permitted.Load())
	}
}

func TestMonitoringLimiterSharedDenialDoesNotConsumeLocalPermit(t *testing.T) {
	var deny atomic.Bool
	deny.Store(true)
	l := NewMonitoringLimiter(sharedLimitFunc(func(context.Context, string, int, time.Duration) (time.Duration, error) {
		if deny.Load() {
			return time.Second, nil
		}
		return 0, errors.New("offline")
	}))
	if w, e := l.AcquireKey(t.Context(), "host", 1, time.Minute); e != nil || w != time.Second {
		t.Fatal(w, e)
	}
	deny.Store(false)
	if w, e := l.AcquireKey(t.Context(), "host", 1, time.Minute); e != nil || w != 0 {
		t.Fatal(w, e)
	}
}

func TestMonitoringLimiterDependencyBudgetAndCancellation(t *testing.T) {
	l := NewMonitoringLimiter(sharedLimitFunc(func(c context.Context, _ string, _ int, _ time.Duration) (time.Duration, error) {
		<-c.Done()
		return 0, c.Err()
	}))
	started := time.Now()
	if w, e := l.AcquireKey(t.Context(), "host", 1, time.Minute); e != nil || w != 0 {
		t.Fatal(w, e)
	}
	if time.Since(started) > time.Second {
		t.Fatal("Redis failure did not fail open promptly")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e := l.AcquireKey(ctx, "other", 1, time.Minute); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
