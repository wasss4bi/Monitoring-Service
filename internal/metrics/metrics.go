package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "monitoring_service",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests processed by the API.",
		},
		[]string{"method", "route", "status"},
	)
	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "monitoring_service",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "End-to-end duration of HTTP requests processed by the API.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		},
		[]string{"method", "route", "status"},
	)
	httpRequestsInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "monitoring_service",
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "Current number of HTTP requests being processed by the API.",
		},
	)
	outboxEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "monitoring_service",
			Subsystem: "outbox",
			Name:      "events_total",
			Help:      "Total outbox-event handling attempts by outcome.",
		},
		[]string{"outcome"},
	)
	outboxPendingEvents = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "monitoring_service",
			Subsystem: "outbox",
			Name:      "pending_events",
			Help:      "Current number of unpublished outbox events.",
		},
	)
	outboxOldestPendingAge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "monitoring_service",
			Subsystem: "outbox",
			Name:      "oldest_pending_age_seconds",
			Help:      "Age in seconds of the oldest unpublished outbox event.",
		},
	)
	workerMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "monitoring_service",
			Subsystem: "worker",
			Name:      "messages_total",
			Help:      "Total worker message handling attempts by outcome.",
		},
		[]string{"outcome"},
	)
	workerMessageDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "monitoring_service",
			Subsystem: "worker",
			Name:      "message_duration_seconds",
			Help:      "Duration of worker message handling attempts.",
			Buckets:   []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60},
		},
		[]string{"outcome"},
	)
)

func init() {
	prometheus.MustRegister(
		httpRequestsTotal,
		httpRequestDuration,
		httpRequestsInFlight,
		outboxEventsTotal,
		outboxPendingEvents,
		outboxOldestPendingAge,
		workerMessagesTotal,
		workerMessageDuration,
	)
}

func ObserveHTTPRequest(method, route, status string, duration time.Duration) {
	httpRequestsTotal.WithLabelValues(method, route, status).Inc()
	httpRequestDuration.WithLabelValues(method, route, status).Observe(duration.Seconds())
}

func IncHTTPRequestsInFlight() { httpRequestsInFlight.Inc() }

func DecHTTPRequestsInFlight() { httpRequestsInFlight.Dec() }

func ObserveOutboxEvent(outcome string) { outboxEventsTotal.WithLabelValues(outcome).Inc() }

func SetOutboxPendingEvents(value int64) { outboxPendingEvents.Set(float64(value)) }

func SetOutboxOldestPendingAge(age time.Duration) { outboxOldestPendingAge.Set(age.Seconds()) }

func ObserveWorkerMessage(outcome string, duration time.Duration) {
	workerMessagesTotal.WithLabelValues(outcome).Inc()
	workerMessageDuration.WithLabelValues(outcome).Observe(duration.Seconds())
}

// StartServer exposes the process metrics on the Docker network.
func StartServer(ctx context.Context, port string, logger *slog.Logger) {
	if port == "" {
		return
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           promhttp.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && logger != nil {
			logger.Error("shutdown metrics server", "error", err)
		}
	}()
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) && logger != nil {
			logger.Error("metrics server stopped", "address", server.Addr, "error", err)
		}
	}()
}
