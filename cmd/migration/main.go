package main

import (
	"context"
	"log/slog"
	"os"

	"monitoring_service/internal/config"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/migrator"
)

func main() {
	cfg := config.Load(".env")
	logger := logging.New(os.Stdout)
	slog.SetDefault(logger)

	if err := migrator.Up(cfg.DatabaseURL, cfg.MigrationsPath); err != nil {
		logging.Error(context.Background(), logger, "failed to run migrations", nil, err)
		os.Exit(1)
	}
}
