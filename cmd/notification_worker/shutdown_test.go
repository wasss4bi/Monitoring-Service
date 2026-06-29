package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInFlightNotificationFinishesAfterShutdown(t *testing.T) {
	stop, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	finish := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- processInFlightNotification(stop, time.Second, func(ctx context.Context) error {
			close(started)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-finish:
				return nil
			}
		})
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		t.Fatalf("notification ended before completion: %v", err)
	default:
	}
	close(finish)
	if err := <-result; err != nil {
		t.Fatalf("notification did not finish: %v", err)
	}
}

func TestInFlightNotificationIsCanceledAfterShutdownTimeout(t *testing.T) {
	stop, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- processInFlightNotification(stop, 20*time.Millisecond, func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("notification was not canceled after shutdown timeout")
	}
}
