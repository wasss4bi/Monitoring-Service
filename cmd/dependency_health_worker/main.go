package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"monitoring_service/internal/config"
	"monitoring_service/internal/database"
	"monitoring_service/internal/repository"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatal("load configuration: ", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	client, err := config.NewFetcherClient()
	if err != nil {
		slog.Error("configure Fetcher mTLS", "error", err)
		return
	}
	defer func() {
		if err := client.Close(); err != nil {
			slog.Error("close client", "error", err)
		}
	}()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("open dependency database", "error", err)
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("close db", "error", err)
		}
	}()
	var recipients []string
	seen := map[string]bool{}
	for _, value := range strings.Split(os.Getenv("FETCHER_ADMIN_TELEGRAM_CHAT_IDS"), ",") {
		id := strings.TrimSpace(value)
		if id == "" {
			continue
		}
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			slog.Error("invalid admin chat ID configuration")
			return
		}
		if !seen[id] {
			recipients = append(recipients, id)
			seen[id] = true
		}
	}
	repo := repository.NewDependencyRepository(db)
	token := uuid.NewString()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		for _, capability := range []string{"direct", "browser"} {
			probeCtx, done := context.WithTimeout(ctx, 1500*time.Millisecond)
			owned, err := repo.AcquireProbe(probeCtx, capability, token)
			if err == nil && owned {
				healthy := client.Healthy(probeCtx, capability)
				err = repo.RecordProbe(probeCtx, capability, token, healthy, recipients)
			}
			done()
			if err != nil {
				slog.Error("Fetcher dependency probe failed", "capability", capability, "error", err)
			}
		}
		recoverCtx, done := context.WithTimeout(ctx, time.Second)
		err := repo.RecoverAlerts(recoverCtx)
		done()
		if err != nil {
			slog.Error("recover operational alerts failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
