package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"monitoring_service/internal/broker"
	"monitoring_service/internal/config"
	"monitoring_service/internal/logging"
	"os"
	"os/signal"
	"syscall"
)

// The migrator is a one-shot bridge from the legacy generic queue to the JSON queue.
// Operators must stop all legacy consumers before starting it; see docs/monitoring.md.
func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatal("load configuration: ", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	l := logging.New(os.Stdout)
	slog.SetDefault(l)
	b := broker.New(cfg.RabbitMQURL)
	defer func() {
		if err := b.Close(); err != nil {
			slog.Error("close b", "error", err)
		}
	}()
	if err := b.DeclareTopology(ctx); err != nil {
		logging.Error(ctx, l, "declare RabbitMQ topology", nil, err)
		return
	}
	count, err := b.QueueMessageCount(ctx, broker.LegacyEndpointFetchQueue)
	if err != nil {
		logging.Error(ctx, l, "inspect legacy fetch queue", nil, err)
		return
	}
	if count == 0 {
		logging.Info(ctx, l, "legacy fetch queue is empty", nil)
		return
	}
	deliveries, err := b.Consume(ctx, broker.LegacyEndpointFetchQueue, "endpoint-fetch-queue-migrator", 1)
	if err != nil {
		logging.Error(ctx, l, "consume legacy fetch queue", nil, err)
		return
	}
	defer func() {
		if err := b.Cancel("endpoint-fetch-queue-migrator"); err != nil {
			slog.Error("cancel endpoint fetch queue consumer", "error", err)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case delivery, ok := <-deliveries:
			if !ok {
				logging.Error(ctx, l, "legacy fetch consumer closed", nil, fmt.Errorf("delivery channel closed"))
				return
			}
			if delivery.MessageId == "" {
				if nackErr := b.Nack(delivery, false); nackErr != nil {
					logging.Error(ctx, l, "dead-letter legacy message without message ID", nil, nackErr)
				}
				logging.Error(ctx, l, "legacy fetch message without message ID was dead-lettered", nil, fmt.Errorf("cannot migrate without idempotency key"))
				return
			}
			if err := b.Publish(ctx, "endpoint.fetch.json", delivery.Body, delivery.MessageId); err != nil {
				logging.Error(ctx, l, "publish migrated fetch message", nil, err)
				if nackErr := b.Nack(delivery, true); nackErr != nil {
					logging.Error(ctx, l, "requeue legacy fetch message", nil, nackErr)
				}
				return
			}
			if err := b.Ack(delivery); err != nil {
				logging.Error(ctx, l, "ack migrated legacy fetch message", nil, err)
				return
			}
			count, err = b.QueueMessageCount(ctx, broker.LegacyEndpointFetchQueue)
			if err != nil {
				logging.Error(ctx, l, "inspect legacy fetch queue", nil, err)
				return
			}
			if count == 0 {
				logging.Info(ctx, l, "legacy fetch queue migrated", nil)
				return
			}
		}
	}
}
