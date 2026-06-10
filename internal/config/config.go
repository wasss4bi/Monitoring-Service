package config

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

type Config struct {
	AppPort                    string
	DatabaseURL                string
	MigrationsPath             string
	JWTSecret                  string
	TargetSessionEncryptionKey string
	RedisURL                   string
	RabbitMQURL                string
	OutboxPollInterval         time.Duration
	OutboxLeaseDuration        time.Duration
	MetricsPort                string
	MetricsRefreshInterval     time.Duration
	IdempotencyKeyTTL          time.Duration
	TelegramBotToken           string
	SMTPHost                   string
	SMTPPort                   string
	SMTPPassword               string
	SMTPName                   string
	SMTPFrom                   string
	MonitorInterval            time.Duration
	MonitorWorkerLease         time.Duration
	MonitorSchedulerPoll       time.Duration
	MonitorCleanupInterval     time.Duration
}

func Load(path string) (Config, error) {
	loadDotEnv(path)
	var durationErr error
	duration := func(key string, fallback time.Duration) time.Duration {
		value, err := getDuration(key, fallback)
		if err != nil && durationErr == nil {
			durationErr = err
		}
		return value
	}

	cfg := Config{
		AppPort:                    getenv("APP_PORT", "8080"),
		DatabaseURL:                getenv("DATABASE_URL", "postgres://monitoring_service:monitoring_service_password@localhost:5432/monitoring_service?sslmode=disable"),
		MigrationsPath:             getenv("MIGRATIONS_PATH", "migrations"),
		JWTSecret:                  getenv("JWT_SECRET", ""),
		TargetSessionEncryptionKey: getenv("TARGET_SESSION_ENCRYPTION_KEY", getenv("MONITOR_AUTHORIZATION_ENCRYPTION_KEY", "")),
		RedisURL:                   getenv("REDIS_URL", "redis://localhost:6379/0"),
		RabbitMQURL:                getenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		OutboxPollInterval:         duration("OUTBOX_POLL_INTERVAL", time.Second),
		OutboxLeaseDuration:        duration("OUTBOX_LEASE_DURATION", 30*time.Second),
		MetricsPort:                getenv("METRICS_PORT", ""),
		MetricsRefreshInterval:     duration("METRICS_REFRESH_INTERVAL", 15*time.Second),
		IdempotencyKeyTTL:          duration("IDEMPOTENCY_KEY_TTL", time.Hour),
		TelegramBotToken:           getenv("TELEGRAM_BOT_TOKEN", ""),
		SMTPHost:                   getenv("SMTP_HOST", ""),
		SMTPPort:                   getenv("SMTP_PORT", "587"),
		SMTPPassword:               getenv("SMTP_PASSWORD", ""),
		SMTPName:                   getenv("SMTP_NAME", ""),
		SMTPFrom:                   getenv("SMTP_FROM", ""),
		MonitorInterval:            duration("MONITOR_INTERVAL", 10*time.Minute),
		MonitorWorkerLease:         duration("MONITOR_WORKER_LEASE", 330*time.Second),
		MonitorSchedulerPoll:       duration("MONITOR_SCHEDULER_POLL", time.Second),
		MonitorCleanupInterval:     duration("MONITOR_CLEANUP_INTERVAL", 24*time.Hour),
	}
	if durationErr != nil {
		return Config{}, durationErr
	}
	return cfg, nil
}

func (c Config) ValidateJWTSecret() error {
	if strings.TrimSpace(c.JWTSecret) == "" {
		return errors.New("JWT_SECRET is required")
	}
	return nil
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid positive duration: %w", key, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return duration, nil
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func loadDotEnv(path string) {
	file, err := os.Open(path) // #nosec G304 -- path is supplied by local configuration or a test fixture
	if err != nil {
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Error("close environment file", "error", err)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" && strings.TrimSpace(os.Getenv(key)) == "" {
			_ = os.Setenv(key, value)
		}
	}
}
