package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"monitoring_service/internal/config"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/migrator"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatal("load configuration: ", err)
	}
	logger := logging.New(os.Stdout)
	slog.SetDefault(logger)

	if err := migrator.Up(cfg.DatabaseURL, cfg.MigrationsPath); err != nil {
		logging.Error(context.Background(), logger, "failed to run migrations", nil, err)
		os.Exit(1)
	}
}
