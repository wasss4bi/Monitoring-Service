package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestCollectorsAreRegistered(t *testing.T) {
	ObserveHTTPRequest("GET", "/test", "200", 0)
	ObserveWorkerMessage("success", 0)

	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	found := make(map[string]bool, len(metricFamilies))
	for _, family := range metricFamilies {
		found[family.GetName()] = true
	}

	for _, name := range []string{
		"monitoring_service_http_requests_total",
		"monitoring_service_http_request_duration_seconds",
		"monitoring_service_outbox_pending_events",
		"monitoring_service_worker_messages_total",
	} {
		if !found[name] {
			t.Errorf("metric %q is not registered", name)
		}
	}
}
