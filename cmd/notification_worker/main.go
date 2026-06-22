package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"monitoring_service/internal/broker"
	"monitoring_service/internal/config"
	"monitoring_service/internal/database"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/metrics"
	"monitoring_service/internal/ratelimit"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/sender"
	"monitoring_service/internal/service"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type payload struct {
	NotificationID            int64  `json:"notification_id"`
	OperationalNotificationID int64  `json:"operational_notification_id"`
	ContactVerificationID     string `json:"contact_verification_id"`
}

const notificationShutdownTimeout = 25 * time.Second

// processInFlightNotification allows an accepted notification to finish after shutdown.
func processInFlightNotification(stop context.Context, timeout time.Duration, process func(context.Context) error) error {
	work, cancel := context.WithCancel(context.WithoutCancel(stop))
	defer cancel()
	stopTimer := context.AfterFunc(stop, func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancel()
		case <-work.Done():
		}
	})
	defer stopTimer()
	return process(work)
}

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatal("load configuration: ", err)
	}
	if err := cfg.ValidateJWTSecret(); err != nil {
		log.Fatal("load configuration: ", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	l := logging.New(os.Stdout)
	slog.SetDefault(l)
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
	redisClient, e := ratelimit.NewRedisClient(cfg.RedisURL)
	if e != nil {
		logging.Error(ctx, l, "open Redis", nil, e)
		return
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			slog.Error("close redisClient", "error", err)
		}
	}()
	execution := service.NewMonitoringExecutionService(repository.NewMonitoringExecutionRepository(db), ratelimit.NewRedisSlidingWindowLimiter(redisClient), cfg.MonitorInterval, cfg.MonitorWorkerLease, sender.NewEmailSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPName, cfg.SMTPPassword, cfg.SMTPFrom), sender.NewTelegramSender(cfg.TelegramBotToken))
	ops := service.OperationalNotifications{Repository: repository.NewDependencyRepository(db), Sender: sender.NewTelegramSender(cfg.TelegramBotToken)}
	verification := service.NewContactVerificationService(repository.NewContactVerificationRepository(db), cfg.JWTSecret, sender.NewVerificationEmailSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPName, cfg.SMTPPassword, cfg.SMTPFrom), sender.NewTelegramSender(cfg.TelegramBotToken))
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
		deliveries, e := b.Consume(ctx, broker.NotificationQueue, "notification-worker", 1)
		if e != nil {
			logging.Error(ctx, l, "consume notification queue", nil, e)
			time.Sleep(time.Second)
			continue
		}
		for d := range deliveries {
			if ctx.Err() != nil {
				if err := b.Nack(d, true); err != nil {
					logging.Error(ctx, l, "requeue notification during shutdown", nil, err)
				}
				return
			}
			var p payload
			decodeErr := json.Unmarshal(d.Body, &p)
			validPayload := (p.NotificationID > 0 && p.OperationalNotificationID == 0 && p.ContactVerificationID == "") || (p.OperationalNotificationID > 0 && p.NotificationID == 0 && p.ContactVerificationID == "") || (p.ContactVerificationID != "" && p.NotificationID == 0 && p.OperationalNotificationID == 0 && broker.ValidDeliveryID(p.ContactVerificationID))
			if decodeErr != nil || !broker.ValidDeliveryID(d.MessageId) || !validPayload {
				logging.Error(ctx, l, "invalid notification delivery", nil, errors.New("invalid notification payload or message ID"))
				if err := b.Nack(d, false); err != nil {
					logging.Error(ctx, l, "dead-letter invalid notification", nil, err)
				}
				continue
			}
			e = processInFlightNotification(ctx, notificationShutdownTimeout, func(workCtx context.Context) error {
				if p.ContactVerificationID != "" {
					return verification.Deliver(workCtx, p.ContactVerificationID)
				}
				if p.OperationalNotificationID > 0 {
					return ops.Process(workCtx, p.OperationalNotificationID, d.MessageId)
				}
				return execution.ProcessNotification(workCtx, p.NotificationID, d.MessageId)
			})
			if e != nil {
				logging.Error(ctx, l, "process notification", nil, e)
				if err := b.Nack(d, true); err != nil {
					logging.Error(ctx, l, "requeue notification", nil, err)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			} else {
				if err := b.Ack(d); err != nil {
					logging.Error(ctx, l, "acknowledge notification", nil, err)
				}
			}
		}
	}
}
