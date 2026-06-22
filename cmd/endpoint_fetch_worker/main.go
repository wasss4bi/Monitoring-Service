package main

import (
	"context"
	"errors"
	"log"
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
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatal("load configuration: ", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	l := logging.New(os.Stdout)
	slog.SetDefault(l)
	sessionProtector, configErr := authorization.NewForPurpose(cfg.TargetSessionEncryptionKey, "target-session:v1")
	if configErr != nil {
		logging.Error(ctx, l, "target session encryption is not configured", map[string]any{"configuration_key": "TARGET_SESSION_ENCRYPTION_KEY"}, configErr)
		return
	}
	metrics.StartServer(ctx, cfg.MetricsPort, l)
	db, e := database.Open(ctx, cfg.DatabaseURL)
	if e != nil {
		logging.Error(ctx, l, "open database", nil, e)
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("close db", "error", err)
		}
	}()
	redisClient, e := ratelimit.NewMonitoringRedisClient(cfg.RedisURL)
	if e != nil {
		logging.Error(ctx, l, "open Redis", nil, e)
		return
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			slog.Error("close redisClient", "error", err)
		}
	}()
	execution := service.NewMonitoringExecutionService(repository.NewMonitoringExecutionRepository(db), ratelimit.NewRedisSlidingWindowLimiter(redisClient), cfg.MonitorInterval, cfg.MonitorWorkerLease, sender.NewEmailSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPName, cfg.SMTPPassword, cfg.SMTPFrom), sender.NewTelegramSender(cfg.TelegramBotToken)).WithTargetSessionProtector(sessionProtector)
	{
		client, err := config.NewFetcherClient()
		if err != nil {
			logging.Error(ctx, l, "configure Fetcher mTLS", nil, err)
			return
		}
		defer func() {
			if err := client.Close(); err != nil {
				slog.Error("close client", "error", err)
			}
		}()
		execution.WithFetcher(client)
	}

	b := broker.New(cfg.RabbitMQURL)
	defer func() {
		if err := b.Close(); err != nil {
			slog.Error("close b", "error", err)
		}
	}()
	for ctx.Err() == nil {
		if e = b.DeclareTopology(ctx); e != nil {
			logging.Error(ctx, l, "declare RabbitMQ topology", nil, e)
			time.Sleep(time.Second)
			continue
		}
		deliveries, e := b.Consume(ctx, broker.EndpointFetchJSONQueue, "endpoint-fetch-worker", 1)
		if e != nil {
			logging.Error(ctx, l, "consume fetch queue", nil, e)
			time.Sleep(time.Second)
			continue
		}
		for d := range deliveries {
			fetchID, valid := broker.DecodeFetchDelivery(d.Body, d.MessageId)
			if !valid {
				logging.Error(ctx, l, "invalid fetch delivery", nil, errors.New("invalid fetch payload or message ID"))
				if err := b.Nack(d, false); err != nil {
					logging.Error(ctx, l, "dead-letter malformed fetch", nil, err)
				}
				continue
			}
			e = execution.ProcessFetch(ctx, fetchID, d.MessageId)
			if e != nil {
				logging.Error(ctx, l, "process endpoint fetch", nil, e)
				if err := b.Nack(d, true); err != nil {
					logging.Error(ctx, l, "requeue fetch", nil, err)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			} else {
				if err := b.Ack(d); err != nil {
					logging.Error(ctx, l, "acknowledge fetch", nil, err)
				}
			}
		}
	}
}
