package main

import (
	"context"
	"log"
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
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatal("load configuration: ", err)
	}
	logger := logging.New(os.Stdout)
	slog.SetDefault(logger)

	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logging.Error(ctx, logger, "failed to open database for idempotency cleanup", nil, err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("close db", "error", err)
		}
	}()

	deleted, err := repository.NewIdempotencyRepository(db).DeleteExpired(ctx)
	if err != nil {
		logging.Error(ctx, logger, "failed to delete expired idempotency keys", nil, err)
		os.Exit(1)
	}
	logging.Info(ctx, logger, "expired idempotency keys deleted", map[string]any{"deleted": deleted})
}
