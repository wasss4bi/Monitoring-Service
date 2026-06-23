package service

import (
	"testing"
	"time"
)

func TestFullJitterRetryDelayBounds(t *testing.T) {
	for attempt, cap := range map[int64]time.Duration{1: 10 * time.Second, 2: 20 * time.Second, 3: 40 * time.Second, 4: 80 * time.Second, 5: 160 * time.Second} {
		for range 100 {
			delay := fullJitterRetryDelay(attempt)
			if delay < 0 || delay > cap {
				t.Fatalf("attempt %d returned %s outside [0,%s]", attempt, delay, cap)
			}
		}
	}
	if got := fullJitterRetryDelay(0); got != 0 {
		t.Fatalf("zero attempt delay = %s, want 0", got)
	}
}
