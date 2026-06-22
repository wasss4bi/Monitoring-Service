package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"time"
)

type counts struct {
	Users         int64 `json:"users"`
	Hosts         int64 `json:"target_hostnames"`
	Endpoints     int64 `json:"monitored_endpoints"`
	Monitors      int64 `json:"monitors"`
	Rules         int64 `json:"monitor_rules"`
	Fetches       int64 `json:"endpoint_fetches"`
	Evaluations   int64 `json:"monitor_fetch_evaluations"`
	Notifications int64 `json:"notifications"`
	Outbox        int64 `json:"outbox_events"`
	Processed     int64 `json:"processed_messages"`
	Keys          int64 `json:"idempotency_keys"`
}
type config struct {
	Mode                                                    string             `json:"mode"`
	Counts                                                  counts             `json:"counts"`
	Batch                                                   int64              `json:"batch_size"`
	Seed                                                    uint64             `json:"seed"`
	AsOf                                                    time.Time          `json:"as_of"`
	From                                                    time.Time          `json:"date_from"`
	MonitorWeights                                          map[string]float64 `json:"monitor_statuses"`
	EndpointWeights                                         map[string]float64 `json:"endpoint_statuses"`
	FetchWeights                                            map[string]float64 `json:"fetch_statuses"`
	NotificationWeights                                     map[string]float64 `json:"notification_statuses"`
	Overdue                                                 float64            `json:"overdue_fraction"`
	Errors                                                  float64            `json:"error_fraction"`
	Unpublished                                             float64            `json:"unpublished_pending_fraction"`
	HTML                                                    float64            `json:"html_fraction"`
	Deleted                                                 float64            `json:"soft_deleted_fraction"`
	active, success, terminal, good, bad, pending, fetching int64
	monitorParts, fetchParts                                map[string]int64
}

var monitorStates = []string{"monitoring", "success", "error", "stopped"}
var endpointStates = []string{"idle", "scheduled", "fetching", "retry_wait", "error", "access_denied"}
var fetchStates = []string{"succeeded", "failed", "pending", "fetching"}
var notificationStates = []string{"pending", "sending", "sent", "failed", "skipped"}

func defaults(mode string) (config, error) {
	if mode != "large" {
		return config{}, fmt.Errorf("only large mode is supported")
	}
	const n int64 = 1000000
	return config{Mode: mode, Counts: counts{max(100, n/10), max(10, n/1000), n, 2 * n, 3 * n, 5 * n / 2, 2 * n, n, 0, n, n}, Batch: 10000, Seed: 42,
		MonitorWeights:      map[string]float64{"monitoring": .55, "success": .30, "error": .10, "stopped": .05},
		EndpointWeights:     map[string]float64{"idle": .60, "scheduled": .15, "fetching": .05, "retry_wait": .15, "error": .04, "access_denied": .01},
		FetchWeights:        map[string]float64{"succeeded": .80, "failed": .10, "pending": .06, "fetching": .04},
		NotificationWeights: map[string]float64{"pending": .15, "sending": .05, "sent": .70, "failed": .07, "skipped": .03},
		Overdue:             .40, Errors: .15, Unpublished: .60, HTML: .30, Deleted: .03}, nil
}

func loadConfig(mode, path string) (config, error) {
	c, err := defaults(mode)
	if err != nil {
		return c, err
	}
	if path != "" {
		f, e := os.Open(path) // #nosec G304 -- path is supplied by local configuration or a test fixture
		if e != nil {
			return c, e
		}
		defer func() {
			if err := f.Close(); err != nil {
				slog.Error("close loadgen configuration", "error", err)
			}
		}()
		d := json.NewDecoder(f)
		d.DisallowUnknownFields()
		if e = d.Decode(&c); e != nil {
			return c, e
		}
		if e = d.Decode(new(any)); e != io.EOF {
			return c, fmt.Errorf("config must contain exactly one JSON object")
		}
		if c.Mode != mode {
			return c, fmt.Errorf("config mode %q does not match --mode=%s", c.Mode, mode)
		}
	}
	if c.AsOf.IsZero() {
		c.AsOf = time.Now().UTC().Truncate(time.Second)
	}
	if c.From.IsZero() {
		c.From = c.AsOf.Add(-90 * 24 * time.Hour)
	}
	return c, c.validate()
}

func partition(total int64, weights map[string]float64, states []string) (map[string]int64, error) {
	sum := 0.0
	for k, v := range weights {
		known := false
		for _, s := range states {
			known = known || k == s
		}
		if !known || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("invalid status weight %s", k)
		}
		sum += v
	}
	if math.Abs(sum-1) > 1e-9 {
		return nil, fmt.Errorf("status weights must sum to 1, got %g", sum)
	}
	result := map[string]int64{}
	remaining := total
	for i, s := range states {
		n := int64(math.Floor(weights[s] * float64(total)))
		if i == len(states)-1 {
			n = remaining
		}
		result[s] = n
		remaining -= n
	}
	return result, nil
}

func (c *config) validate() error {
	n := c.Counts
	if c.Batch < 1 || c.Batch > 100000 {
		return fmt.Errorf("batch_size must be 1..100000")
	}
	if c.From.After(c.AsOf.Add(-24 * time.Hour)) {
		return fmt.Errorf("date_from must precede as_of by at least 24 hours")
	}
	if c.AsOf.Sub(c.From) > 5*365*24*time.Hour {
		return fmt.Errorf("date range exceeds five years")
	}
	for _, v := range []float64{c.Overdue, c.Errors, c.Unpublished, c.HTML, c.Deleted} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return fmt.Errorf("fractions must be within [0,1]")
		}
	}
	if n.Users < 1 || n.Hosts < 1 || n.Endpoints < 1 || n.Monitors < 2 || n.Rules < n.Monitors || n.Rules > 3*n.Monitors || n.Fetches < n.Monitors || n.Evaluations < 0 || n.Notifications < 0 || n.Processed < 0 || n.Keys < 0 || n.Outbox < 0 {
		return fmt.Errorf("invalid counts: require positive dimensions, 1..3 rules/monitor, fetches >= monitors, nonnegative optional counts")
	}
	for _, v := range []int64{n.Users, n.Hosts, n.Endpoints, n.Monitors, n.Fetches, n.Keys, n.Outbox, n.Processed, n.Notifications} {
		if v > 100000000 {
			return fmt.Errorf("count exceeds supported limit 100000000")
		}
	}
	var err error
	c.monitorParts, err = partition(n.Monitors, c.MonitorWeights, monitorStates)
	if err != nil {
		return err
	}
	c.fetchParts, err = partition(n.Fetches, c.FetchWeights, fetchStates)
	if err != nil {
		return err
	}
	if _, err = partition(n.Endpoints, c.EndpointWeights, endpointStates); err != nil {
		return err
	}
	if _, err = partition(n.Notifications, c.NotificationWeights, notificationStates); err != nil {
		return err
	}
	c.active = c.monitorParts["monitoring"]
	c.success = c.monitorParts["success"]
	c.terminal = c.success + c.monitorParts["error"]
	c.good = c.fetchParts["succeeded"]
	c.bad = c.fetchParts["failed"]
	c.pending = c.fetchParts["pending"]
	c.fetching = c.fetchParts["fetching"]
	if c.active < 1 || c.good < n.Monitors || c.bad+c.pending+c.fetching > c.active || n.Evaluations > c.good || n.Notifications > 2*c.terminal {
		return fmt.Errorf("infeasible lifecycle: succeeded fetches >= monitors, failed+pending+fetching <= active monitors, evaluations <= succeeded fetches, notifications <= 2*(success+error monitors)")
	}
	if n.Evaluations < c.terminal {
		return fmt.Errorf("evaluations must cover terminal monitors")
	}
	if n.Outbox == 0 {
		c.Counts.Outbox = n.Fetches + n.Notifications
	} else if n.Outbox < n.Fetches+n.Notifications {
		return fmt.Errorf("outbox_events must cover every fetch and notification (or use 0 for automatic count)")
	}
	// Additional retained events are legitimate delayed re-deliveries of pending fetches.
	if c.Counts.Outbox > n.Fetches+n.Notifications && c.pending == 0 {
		return fmt.Errorf("extra outbox retries require pending fetches")
	}
	if n.Processed > c.good+c.bad+terminalNotifications(c) {
		return fmt.Errorf("processed_messages exceeds completed event count")
	}
	return nil
}

func terminalNotifications(c *config) int64 {
	var n int64
	for i := int64(1); i <= c.Counts.Notifications; i++ {
		s := c.notificationStatus(i)
		if s != "pending" && s != "sending" {
			n++
		}
	}
	return n
}
