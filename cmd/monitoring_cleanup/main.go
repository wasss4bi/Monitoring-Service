package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"monitoring_service/internal/config"
	"monitoring_service/internal/database"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/repository"
)

const (
	cleanupTimeout = 30 * time.Second
	cleanupBatch   = 1000
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatal("load configuration: ", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	logger := logging.New(os.Stdout)
	slog.SetDefault(logger)
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logging.Error(ctx, logger, "open database", nil, err)
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("close db", "error", err)
		}
	}()
	repo := repository.NewMonitoringMaintenanceRepository(db)
	run := func() {
		runCtx, stop := context.WithTimeout(ctx, cleanupTimeout)
		defer stop()
		result, err := repo.DeleteExpired(runCtx, cleanupBatch)
		if err != nil {
			logging.Error(runCtx, logger, "clean monitoring retention data", nil, err)
			return
		}
		logging.Info(runCtx, logger, "monitoring retention cleanup completed", map[string]any{"endpoint_fetches": result.EndpointFetches, "processed_messages": result.ProcessedMessages, "notifications": result.Notifications})
	}
	run()
	ticker := time.NewTicker(cfg.MonitorCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
