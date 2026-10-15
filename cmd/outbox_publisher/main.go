package main

import (
	"context"
	"database/sql"
	"errors"
	"monitoring_service/internal/broker"
	"monitoring_service/internal/config"
	"monitoring_service/internal/database"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/metrics"
	"monitoring_service/internal/repository"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
)

func main() {
	cfg := config.Load(".env")
	l := logging.New(os.Stdout)
	slog.SetDefault(l)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	metrics.StartServer(ctx, cfg.MetricsPort, l)
	db, e := database.Open(ctx, cfg.DatabaseURL)
	if e != nil {
		logging.Error(ctx, l, "open database", nil, e)
		return
	}
	defer db.Close()
	b := broker.New(cfg.RabbitMQURL)
	defer b.Close()
	out := repository.NewOutboxRepository(db)
	go observeOutboxStats(ctx, out, cfg.MetricsRefreshInterval, l)
	for ctx.Err() == nil {
		if e = b.Connect(ctx); e == nil {
			e = b.DeclareTopology(ctx)
		}
		if e != nil {
			logging.Error(ctx, l, "RabbitMQ unavailable", nil, e)
			metrics.ObserveOutboxEvent("broker_unavailable")
			continue
		}
		token := uuid.NewString()
		event, e := out.Claim(ctx, token, cfg.OutboxLeaseDuration)
		if errors.Is(e, sql.ErrNoRows) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(cfg.OutboxPollInterval):
			}
			continue
		}
		if e != nil {
			logging.Error(ctx, l, "claim outbox event", nil, e)
			metrics.ObserveOutboxEvent("claim_error")
			continue
		}
		metrics.ObserveOutboxEvent("claimed")
		routingKey, e := routingKeyForEvent(event.EventType)
		if e != nil {
			_ = out.Release(context.Background(), event.ID, token)
			logging.Error(ctx, l, "unsupported outbox event type", map[string]any{"event_id": event.EventID, "event_type": event.EventType}, e)
			metrics.ObserveOutboxEvent("unsupported_event")
			continue
		}
		if e = b.Publish(ctx, routingKey, event.Payload, event.EventID); e != nil {
			_ = out.Release(context.Background(), event.ID, token)
			logging.Error(ctx, l, "publish outbox event", map[string]any{"event_id": event.EventID}, e)
			metrics.ObserveOutboxEvent("publish_error")
			continue
		}
		if e = out.MarkPublished(ctx, event.ID, token); e != nil {
			logging.Error(ctx, l, "mark outbox published", map[string]any{"event_id": event.EventID}, e)
			metrics.ObserveOutboxEvent("mark_published_error")
			continue
		}
		metrics.ObserveOutboxEvent("published")
	}
}

func routingKeyForEvent(eventType string) (string, error) {
	if eventType == "" {
		return "", errors.New("outbox event type is empty")
	}
	return eventType, nil
}

func observeOutboxStats(ctx context.Context, out *repository.OutboxRepository, interval time.Duration, logger *slog.Logger) {
	observe := func() {
		pending, oldestAge, err := out.PendingStats(ctx)
		if err != nil {
			logging.Error(ctx, logger, "get outbox metrics", nil, err)
			return
		}
		metrics.SetOutboxPendingEvents(pending)
		metrics.SetOutboxOldestPendingAge(oldestAge)
	}
	observe()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			observe()
		}
	}
}
