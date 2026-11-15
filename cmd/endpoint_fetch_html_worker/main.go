package main

import (
	"context"
	"errors"
	"monitoring_service/internal/authorization"
	"monitoring_service/internal/broker"

	"monitoring_service/internal/config"
	"monitoring_service/internal/database"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/metrics"

	"log/slog"
	"monitoring_service/internal/ratelimit"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/sender"
	"monitoring_service/internal/service"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg := config.Load(".env")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	l := logging.New(os.Stdout)
	slog.SetDefault(l)
	metrics.StartServer(ctx, cfg.MetricsPort, l)
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logging.Error(ctx, l, "open database", nil, err)
		return
	}
	defer db.Close()
	redisClient, err := ratelimit.NewMonitoringRedisClient(cfg.RedisURL)
	if err != nil {
		logging.Error(ctx, l, "open Redis", nil, err)
		return
	}
	defer redisClient.Close()
	base := service.NewMonitoringExecutionService(repository.NewMonitoringExecutionRepository(db), ratelimit.NewRedisSlidingWindowLimiter(redisClient), cfg.MonitorInterval, cfg.MonitorWorkerLease, sender.NewEmailSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPName, cfg.SMTPPassword, cfg.SMTPFrom), sender.NewTelegramSender(cfg.TelegramBotToken))

	sessionProtector, err := authorization.NewForPurpose(cfg.TargetSessionEncryptionKey, "target-session:v1")
	if err != nil {
		logging.Error(ctx, l, "monitor session encryption is not configured", nil, err)
		return
	}
	base.WithTargetSessionProtector(sessionProtector)
	execution := base
	{
		client, err := config.NewFetcherClient()
		if err != nil {
			logging.Error(ctx, l, "configure Fetcher mTLS", nil, err)
			return
		}
		defer client.Close()
		base.WithFetcher(client)
	}

	b := broker.New(cfg.RabbitMQURL)
	defer b.Close()
	for ctx.Err() == nil {
		if err = b.DeclareTopology(ctx); err != nil {
			logging.Error(ctx, l, "declare RabbitMQ topology", nil, err)
			time.Sleep(time.Second)
			continue
		}
		deliveries, err := b.Consume(ctx, broker.EndpointFetchHTMLQueue, "endpoint-fetch-worker", 1)
		if err != nil {
			logging.Error(ctx, l, "consume HTML fetch queue", nil, err)
			time.Sleep(time.Second)
			continue
		}
		for delivery := range deliveries {
			fetchID, valid := broker.DecodeFetchDelivery(delivery.Body, delivery.MessageId)
			if !valid {
				logging.Error(ctx, l, "invalid HTML fetch delivery", nil, errors.New("invalid fetch payload or message ID"))
				if err := b.Nack(delivery, false); err != nil {
					logging.Error(ctx, l, "dead-letter malformed fetch", nil, err)
				}
				continue
			}
			err = execution.ProcessFetch(ctx, fetchID, delivery.MessageId)
			if err != nil {
				logging.Error(ctx, l, "process HTML endpoint fetch", nil, err)
				if e := b.Nack(delivery, true); e != nil {
					logging.Error(ctx, l, "requeue fetch", nil, e)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			} else {
				if e := b.Ack(delivery); e != nil {
					logging.Error(ctx, l, "acknowledge fetch", nil, e)
				}
			}
		}
	}
}
