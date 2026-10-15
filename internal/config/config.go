package config

import (
	"bufio"
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

func Load(path string) Config {
	loadDotEnv(path)

	return Config{
		AppPort:                    getenv("APP_PORT", "8080"),
		DatabaseURL:                getenv("DATABASE_URL", "postgres://monitoring_service:monitoring_service_password@localhost:5432/monitoring_service?sslmode=disable"),
		MigrationsPath:             getenv("MIGRATIONS_PATH", "migrations"),
		JWTSecret:                  getenv("JWT_SECRET", ""),
		TargetSessionEncryptionKey: getenv("TARGET_SESSION_ENCRYPTION_KEY", getenv("MONITOR_AUTHORIZATION_ENCRYPTION_KEY", "")),
		RedisURL:                   getenv("REDIS_URL", "redis://localhost:6379/0"),
		RabbitMQURL:                getenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		OutboxPollInterval:         getDuration("OUTBOX_POLL_INTERVAL", time.Second),
		OutboxLeaseDuration:        getDuration("OUTBOX_LEASE_DURATION", 30*time.Second),
		MetricsPort:                getenv("METRICS_PORT", ""),
		MetricsRefreshInterval:     getDuration("METRICS_REFRESH_INTERVAL", 15*time.Second),
		IdempotencyKeyTTL:          getDuration("IDEMPOTENCY_KEY_TTL", time.Hour),
		TelegramBotToken:           getenv("TELEGRAM_BOT_TOKEN", ""),
		SMTPHost:                   getenv("SMTP_HOST", ""),
		SMTPPort:                   getenv("SMTP_PORT", "587"),
		SMTPPassword:               getenv("SMTP_PASSWORD", ""),
		SMTPName:                   getenv("SMTP_NAME", ""),
		SMTPFrom:                   getenv("SMTP_FROM", ""),
		MonitorInterval:            getDuration("MONITOR_INTERVAL", 10*time.Minute),
		MonitorWorkerLease:         getDuration("MONITOR_WORKER_LEASE", 330*time.Second),
		MonitorSchedulerPoll:       getDuration("MONITOR_SCHEDULER_POLL", time.Second),
		MonitorCleanupInterval:     getDuration("MONITOR_CLEANUP_INTERVAL", 24*time.Hour),
	}
}

func getDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

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
