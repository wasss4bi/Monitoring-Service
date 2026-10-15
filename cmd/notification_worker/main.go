package main

import (
	"context"
	"encoding/json"
	"monitoring_service/internal/broker"
	"monitoring_service/internal/config"
	"monitoring_service/internal/database"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/metrics"
	"monitoring_service/internal/ratelimit"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/sender"
	"monitoring_service/internal/service"
	"log/slog"
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

func main() {
	cfg := config.Load(".env")
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
	defer db.Close()
	redisClient, e := ratelimit.NewRedisClient(cfg.RedisURL)
	if e != nil {
		logging.Error(ctx, l, "open Redis", nil, e)
		return
	}
	defer redisClient.Close()
	execution := service.NewMonitoringExecutionService(repository.NewMonitoringExecutionRepository(db), ratelimit.NewRedisSlidingWindowLimiter(redisClient), cfg.MonitorInterval, cfg.MonitorWorkerLease, sender.NewEmailSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPName, cfg.SMTPPassword, cfg.SMTPFrom), sender.NewTelegramSender(cfg.TelegramBotToken))
	ops := service.OperationalNotifications{Repository: repository.NewDependencyRepository(db), Sender: sender.NewTelegramSender(cfg.TelegramBotToken)}
	verification := service.NewContactVerificationService(repository.NewContactVerificationRepository(db), cfg.JWTSecret, sender.NewVerificationEmailSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPName, cfg.SMTPPassword, cfg.SMTPFrom), sender.NewTelegramSender(cfg.TelegramBotToken))
	b := broker.New(cfg.RabbitMQURL)
	defer b.Close()
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
			var p payload
			if json.Unmarshal(d.Body, &p) != nil || !broker.ValidDeliveryID(d.MessageId) || !((p.NotificationID > 0 && p.OperationalNotificationID == 0 && p.ContactVerificationID == "") || (p.OperationalNotificationID > 0 && p.NotificationID == 0 && p.ContactVerificationID == "") || (p.ContactVerificationID != "" && p.NotificationID == 0 && p.OperationalNotificationID == 0 && broker.ValidDeliveryID(p.ContactVerificationID))) {
				if err := b.Nack(d, false); err != nil {
					logging.Error(ctx, l, "dead-letter invalid notification", nil, err)
				}
				continue
			}
			if p.ContactVerificationID != "" {
				e = verification.Deliver(ctx, p.ContactVerificationID)
			} else if p.OperationalNotificationID > 0 {
				e = ops.Process(ctx, p.OperationalNotificationID, d.MessageId)
			} else {
				e = execution.ProcessNotification(ctx, p.NotificationID, d.MessageId)
			}
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
