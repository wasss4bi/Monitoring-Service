package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"monitoring_service/internal/httpapi/handler"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/metrics"
	"monitoring_service/internal/service"

	"github.com/google/uuid"
)

type RouterConfig struct {
	DB                         *sql.DB
	Logger                     *slog.Logger
	UserService                *service.UserService
	IdempotencyService         *service.IdempotencyService
	ContactService             *service.ContactService
	UserContactService         *service.UserContactService
	ContactVerificationService *service.ContactVerificationService
	MonitorService             *service.MonitorService
	NotificationService        *service.NotificationService
	TargetHostnameService      *service.TargetHostnameService
	JWTSecret                  string
	AccessControl              *handler.AccessControl
	BodyLimits                 map[string]int64
}

func NewRouter(cfg RouterConfig) http.Handler {
	healthHandler := handler.NewHealthHandler(cfg.DB)
	userHandler := handler.NewUserHandler(cfg.UserService, cfg.IdempotencyService)
	authHandler := handler.NewAuthHandler(cfg.UserService, cfg.JWTSecret, cfg.Logger)
	contactHandler := handler.NewContactHandler(cfg.ContactService)
	userContactHandler := handler.NewUserContactHandler(cfg.UserContactService)
	verificationHandler := handler.NewContactVerificationHandler(cfg.ContactVerificationService)
	monitorHandler := handler.NewMonitorHandler(cfg.MonitorService, cfg.IdempotencyService)
	notificationHandler := handler.NewNotificationHandler(cfg.NotificationService)
	targetHostnameHandler := handler.NewTargetHostnameHandler(cfg.TargetHostnameService)

	mux := http.NewServeMux()
	mux.HandleFunc("/openapi.json", handler.OpenAPI)
	mux.HandleFunc("/swagger", handler.Swagger)
	mux.HandleFunc("/swagger/", handler.Swagger)
	mux.HandleFunc("/health", healthHandler.Health)
	require := func(resource string, next http.HandlerFunc) http.Handler {
		if cfg.AccessControl == nil {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				if _, err := w.Write([]byte(`{"error":{"code":"Service Unavailable","message":"authorization unavailable"}}`)); err != nil {
					logging.Error(r.Context(), cfg.Logger, "failed to write authorization error response", nil, err)
				}
			})
		}
		return cfg.AccessControl.Require(cfg.JWTSecret, resource, next)
	}
	mux.Handle("POST /users", http.HandlerFunc(userHandler.Users))
	mux.Handle("/users", require("users", userHandler.Users))
	mux.Handle("/users/", require("profile", userHandler.UserByID))
	mux.Handle("/users/{id}/check-password", require("profile", userHandler.CheckPassword))
	mux.HandleFunc("POST /auth/login", authHandler.Login)
	mux.Handle("/contacts", require("contacts", contactHandler.Contacts))
	mux.Handle("/contacts/{id}", require("contacts", contactHandler.ContactByID))
	mux.Handle("/me/contacts", require("me", userContactHandler.UserContacts))
	mux.Handle("/me/contacts/{contact_id}", require("me", userContactHandler.UserContactByID))
	mux.Handle("POST /me/contacts/{contact_id}/verification", require("me", verificationHandler.Request))
	mux.Handle("POST /me/contacts/{contact_id}/verification/confirm", require("me", verificationHandler.Verify))
	mux.Handle("/me/notifications", require("me", notificationHandler.Notifications))
	mux.Handle("/me/monitors", require("me", monitorHandler.Monitors))
	mux.Handle("/me/monitors/{id}", require("me", monitorHandler.MonitorByID))
	mux.Handle("/target-hostnames", require("target-hostnames", targetHostnameHandler.TargetHostnames))
	mux.Handle("/target-hostnames/{id}", require("target-hostnames", targetHostnameHandler.TargetHostnameByID))
	mux.Handle("POST /target-hostnames/{id}/reset-session", require("target-hostnames", targetHostnameHandler.ResetSession))
	mux.Handle("POST /me/monitors/{id}/rule", require("me", monitorHandler.AddRule))
	mux.Handle("POST /me/monitors/{id}/stop", require("me", monitorHandler.Stop))
	mux.Handle("POST /me/monitors/{id}/resume", require("me", monitorHandler.Resume))

	return logRequest(withImmediateRequestTimeout(withBodyLimits(mux, cfg.BodyLimits)), cfg.Logger)
}

const immediateRequestTimeout = 2 * time.Second
const defaultBodyLimit int64 = 1 << 20

// BodyLimits uses the registered ServeMux pattern, e.g. "POST /users".
func withBodyLimits(mux *http.ServeMux, limits map[string]int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		limit := defaultBodyLimit
		if n, ok := limits[pattern]; ok && n > 0 {
			limit = n
		}
		if r.Body == nil {
			mux.ServeHTTP(w, r)
			return
		}
		body := http.MaxBytesReader(w, r.Body, limit)
		payload, err := io.ReadAll(body)
		_ = body.Close()
		if err != nil {
			var tooLarge *http.MaxBytesError
			w.Header().Set("Content-Type", "application/json")
			if errors.As(err, &tooLarge) {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				_, _ = w.Write([]byte(`{"error":{"code":"Request Entity Too Large","message":"request body is too large"}}`))
				return
			}
			setRequestError(w, err)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"Bad Request","message":"failed to read request body"}}`))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(payload))
		mux.ServeHTTP(w, r)
	})
}

func withImmediateRequestTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), immediateRequestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func logRequest(next http.Handler, logger *slog.Logger) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		ctx := logging.WithRequestID(r.Context(), uuid.NewString())
		r = r.WithContext(ctx)
		recorder := &statusRecorder{ResponseWriter: w}
		start := time.Now()
		metrics.IncHTTPRequestsInFlight()
		defer metrics.DecHTTPRequestsInFlight()
		next.ServeHTTP(recorder, r)
		route := r.Pattern
		if route == "" {
			route = r.URL.Path
		}
		observeHTTPRequest(r.Method, route, strconv.Itoa(recorder.Status()), time.Since(start))
		if recorder.Status() == http.StatusBadRequest && (recorder.RequestError() == nil || errors.Is(recorder.RequestError(), service.ErrValidation)) {
			return
		}

		payload := map[string]any{
			"method":   r.Method,
			"path":     r.URL.Path,
			"status":   recorder.Status(),
			"duration": time.Since(start).String(),
		}
		if err := recorder.RequestError(); err != nil {
			logging.Error(ctx, logger, "request processed", payload, err)
			return
		}
		if recorder.Status() >= http.StatusInternalServerError {
			logging.Error(ctx, logger, "request processed", payload, fmt.Errorf("request completed with status %d", recorder.Status()))
			return
		}

		logging.Info(ctx, logger, "request processed", payload)
	})
}
