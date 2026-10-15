package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"monitoring_service/internal/config"
	"monitoring_service/internal/database"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/repository"
)

const cleanupTimeout = 30 * time.Second

func main() {
	cfg := config.Load(".env")
	logger := logging.New(os.Stdout)
	slog.SetDefault(logger)

	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logging.Error(ctx, logger, "failed to open database for idempotency cleanup", nil, err)
		os.Exit(1)
	}
	defer db.Close()

	deleted, err := repository.NewIdempotencyRepository(db).DeleteExpired(ctx)
	if err != nil {
		logging.Error(ctx, logger, "failed to delete expired idempotency keys", nil, err)
		os.Exit(1)
	}
	logging.Info(ctx, logger, "expired idempotency keys deleted", map[string]any{"deleted": deleted})
}
