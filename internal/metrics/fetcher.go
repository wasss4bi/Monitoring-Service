package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var fetcherEvents = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "monitoring_service", Subsystem: "fetcher", Name: "events_total", Help: "Fetcher and dependency outcomes; no destination or user labels."}, []string{"component", "outcome"})

func init()                                  { prometheus.MustRegister(fetcherEvents) }
func FetcherEvent(component, outcome string) { fetcherEvents.WithLabelValues(component, outcome).Inc() }
