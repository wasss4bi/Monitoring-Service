package main

import (
	"context"
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
	redisClient, err := ratelimit.NewRedisClient(cfg.RedisURL)
	if err != nil {
		logging.Error(ctx, l, "open Redis", nil, err)
		return
	}
	defer redisClient.Close()
	execution := service.NewMonitoringExecutionService(repository.NewMonitoringExecutionRepository(db).WithDependencyGate(true), ratelimit.NewRedisSlidingWindowLimiter(redisClient), cfg.MonitorInterval, cfg.MonitorWorkerLease, sender.NewEmailSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPName, cfg.SMTPPassword, cfg.SMTPFrom), sender.NewTelegramSender(cfg.TelegramBotToken))
	ticker := time.NewTicker(cfg.MonitorSchedulerPoll)
	defer ticker.Stop()
	for {
		if n, e := execution.Schedule(ctx); e != nil {
			logging.Error(ctx, l, "schedule monitoring", nil, e)
		} else if n > 0 {
			logging.Info(ctx, l, "scheduled monitoring fetches", map[string]any{"count": n})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
