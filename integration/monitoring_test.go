//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"monitoring_service/internal/broker"
	"monitoring_service/internal/cache"
	"monitoring_service/internal/database"
	"monitoring_service/internal/ratelimit"
	"monitoring_service/internal/repository"

	"github.com/google/uuid"
)

func TestMonitoringInfrastructureAndInbox(t *testing.T) {
	databaseURL := os.Getenv("INTEGRATION_DATABASE_URL")
	redisURL := os.Getenv("INTEGRATION_REDIS_URL")
	rabbitURL := os.Getenv("INTEGRATION_RABBITMQ_URL")
	if databaseURL == "" || redisURL == "" || rabbitURL == "" {
		t.Skip("set INTEGRATION_DATABASE_URL, INTEGRATION_REDIS_URL and INTEGRATION_RABBITMQ_URL to run integration checks")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	db, err := database.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	}()
	redisClient, err := ratelimit.NewRedisClient(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			t.Errorf("close redisClient: %v", err)
		}
	}()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	rabbit := broker.New(rabbitURL)
	defer func() {
		if err := rabbit.Close(); err != nil {
			t.Errorf("close rabbit: %v", err)
		}
	}()
	if err := rabbit.DeclareTopology(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	messageID := uuid.NewString()
	processed, err := repository.NewProcessedMessageRepository().RecordOnce(ctx, tx, "integration-test", messageID)
	if err != nil || !processed {
		t.Fatalf("first inbox record: processed=%v err=%v", processed, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	already, err := repository.NewProcessedMessageRepository().IsProcessed(ctx, db, "integration-test", messageID)
	if err != nil || !already {
		t.Fatalf("inbox recovery check: processed=%v err=%v", already, err)
	}
	if err := cache.NewRedisCache(redisClient).Set(ctx, "integration:monitoring", "ok", time.Minute); err != nil {
		t.Fatal(err)
	}
}
