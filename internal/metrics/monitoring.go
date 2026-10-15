package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	monitorFetchesTotal       = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "monitoring_service", Subsystem: "monitor", Name: "fetches_total", Help: "Endpoint fetches by final outcome."}, []string{"outcome"})
	monitorFetchDuration      = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "monitoring_service", Subsystem: "monitor", Name: "fetch_duration_seconds", Help: "Duration of monitored endpoint fetches.", Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 20}}, []string{"outcome"})
	monitorNotificationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "monitoring_service", Subsystem: "monitor", Name: "notifications_total", Help: "Notification delivery attempts by outcome."}, []string{"outcome"})
)

func init() {
	prometheus.MustRegister(monitorFetchesTotal, monitorFetchDuration, monitorNotificationsTotal)
}
func ObserveMonitorFetch(outcome string, duration time.Duration) {
	monitorFetchesTotal.WithLabelValues(outcome).Inc()
	monitorFetchDuration.WithLabelValues(outcome).Observe(duration.Seconds())
}
func ObserveMonitorNotification(outcome string) {
	monitorNotificationsTotal.WithLabelValues(outcome).Inc()
}
