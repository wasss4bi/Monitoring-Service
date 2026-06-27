package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadKeepsExplicitDatabaseURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("DATABASE_URL=postgres://from-file/example?sslmode=require\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := "postgres://from-environment/example?sslmode=disable"
	t.Setenv("DATABASE_URL", want)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.DatabaseURL; got != want {
		t.Fatalf("DatabaseURL = %q, want explicit environment value %q", got, want)
	}
}

func TestLoadRejectsInvalidDurations(t *testing.T) {
	keys := []string{"OUTBOX_POLL_INTERVAL", "OUTBOX_LEASE_DURATION", "METRICS_REFRESH_INTERVAL", "IDEMPOTENCY_KEY_TTL", "MONITOR_INTERVAL", "MONITOR_WORKER_LEASE", "MONITOR_SCHEDULER_POLL", "MONITOR_CLEANUP_INTERVAL"}
	for _, key := range keys {
		for _, value := range []string{"not-a-duration", "0s", "-1s"} {
			t.Run(key+"/"+value, func(t *testing.T) {
				t.Setenv(key, value)
				_, err := Load(filepath.Join(t.TempDir(), "missing.env"))
				if err == nil || !strings.Contains(err.Error(), key) {
					t.Fatalf("expected error naming %s, got %v", key, err)
				}
			})
		}
	}
}

func TestLoadAcceptsValidDuration(t *testing.T) {
	t.Setenv("MONITOR_INTERVAL", "45s")
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.env"))
	if err != nil || cfg.MonitorInterval != 45*time.Second {
		t.Fatalf("interval=%v, err=%v", cfg.MonitorInterval, err)
	}
}

func TestValidateJWTSecret(t *testing.T) {
	for _, value := range []string{"", "   "} {
		if err := (Config{JWTSecret: value}).ValidateJWTSecret(); err == nil {
			t.Fatalf("expected JWT_SECRET validation error for %q", value)
		}
	}
	if err := (Config{JWTSecret: "test-secret"}).ValidateJWTSecret(); err != nil {
		t.Fatal(err)
	}
}
