package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"monitoring_service/internal/authorization"
	"monitoring_service/internal/config"
	"monitoring_service/internal/database"
	"monitoring_service/internal/httpapi"
	"monitoring_service/internal/httpapi/handler"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/metrics"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/service"
)

func main() {
	cfg := config.Load(".env")
	logger := logging.New(os.Stdout)
	slog.SetDefault(logger)
	if cfg.JWTSecret == "" {
		logging.Error(context.Background(), logger, "JWT authentication is not configured", map[string]any{
			"configuration_key": "JWT_SECRET",
		}, errors.New("JWT_SECRET is empty"))
	}
	db, err := database.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logging.Error(context.Background(), logger, "failed to open database", nil, err)
		return
	}
	defer db.Close()

	userRepository := repository.NewUserRepository(db)
	access, err := handler.NewAccessControl(userRepository)
	if err != nil {
		logging.Error(context.Background(), logger, "configure authorization", nil, err)
		return
	}
	metrics.StartServer(context.Background(), cfg.MetricsPort, logger)
	idempotencyRepository := repository.NewIdempotencyRepository(db)
	contactRepository := repository.NewContactRepository(db)
	userContactRepository := repository.NewUserContactRepository(db)
	monitorRepository := repository.NewMonitorRepository(db)
	targetHostnameRepository := repository.NewTargetHostnameRepository(db)

	userService := service.NewUserService(userRepository)
	idempotencyService := service.NewIdempotencyService(idempotencyRepository, cfg.IdempotencyKeyTTL)
	contactService := service.NewContactService(contactRepository)
	userContactService := service.NewUserContactService(userContactRepository)
	// Retain the existing purpose so copied legacy ciphertext remains decryptable.
	sessionProtector, err := authorization.NewForPurpose(cfg.TargetSessionEncryptionKey, "target-session:v1")
	if err != nil {
		logging.Error(context.Background(), logger, "monitor session encryption is not configured", nil, err)
		return
	}
	monitorService := service.NewMonitorService(monitorRepository).WithTargetHostnameRepository(targetHostnameRepository).WithSessionProtector(sessionProtector)
	targetHostnameService := service.NewTargetHostnameService(targetHostnameRepository)
	notificationService := service.NewNotificationService(repository.NewNotificationRepository(db))

	router := httpapi.NewRouter(httpapi.RouterConfig{
		DB:                         db,
		Logger:                     logger,
		UserService:                userService,
		IdempotencyService:         idempotencyService,
		ContactService:             contactService,
		UserContactService:         userContactService,
		ContactVerificationService: service.NewContactVerificationService(repository.NewContactVerificationRepository(db), cfg.JWTSecret, nil, nil),
		MonitorService:             monitorService,
		NotificationService:        notificationService,
		TargetHostnameService:      targetHostnameService,
		JWTSecret:                  cfg.JWTSecret,
		AccessControl:              access,
	})

	addr := ":" + cfg.AppPort
	logging.Info(context.Background(), logger, "server started", map[string]any{"address": addr})
	if err := newHTTPServer(addr, router).ListenAndServe(); err != nil {
		logging.Error(context.Background(), logger, "server stopped", map[string]any{"address": addr}, err)
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second}
}
