package main

import (
	"reflect"
	"testing"
	"time"
)

func fixture(t *testing.T) config {
	t.Helper()
	c, err := defaults("large")
	if err != nil {
		t.Fatal(err)
	}
	// Bounded counts exercise lifecycle invariants without exposing additional CLI profiles.
	c.Counts = counts{Users: 100, Hosts: 10, Endpoints: 1000, Monitors: 2000, Rules: 3000, Fetches: 2500, Evaluations: 2000, Notifications: 1000, Processed: 1000, Keys: 1000}
	c.AsOf = time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)
	c.From = c.AsOf.Add(-90 * 24 * time.Hour)
	if err = c.validate(); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestUnsupportedGenerationModes(t *testing.T) {
	for _, mode := range []string{"small", "medium"} {
		if _, err := loadConfig(mode, ""); err == nil {
			t.Fatalf("removed mode %s was accepted", mode)
		}
	}
}
func TestLifecycleAndOwnership(t *testing.T) {
	c := fixture(t)
	ids := map[int64]bool{}
	keys := map[[2]int64]bool{}
	states := map[string]int64{}
	contactOwners := map[int64]int64{}
	for _, job := range c.jobs() {
		if job.name == "user_contacts" {
			for i := int64(0); i < job.count; i++ {
				row, ok := job.next()
				if !ok {
					t.Fatal("contact stream ended early")
				}
				contactOwners[row[0].(int64)] = row[1].(int64)
			}
		}
	}
	for rank := int64(1); rank <= c.Counts.Monitors; rank++ {
		id := c.monitorID(rank)
		if ids[id] || id < 1 || id > c.Counts.Monitors {
			t.Fatal("invalid permutation")
		}
		ids[id] = true
		if c.rulesCount(id) < 1 || c.rulesCount(id) > 3 {
			t.Fatal("rule limit")
		}
	}
	var rules int64
	for id := int64(1); id <= c.Counts.Monitors; id++ {
		rules += c.rulesCount(id)
	}
	if rules != c.Counts.Rules {
		t.Fatal("rule count")
	}
	for id := int64(1); id <= c.Counts.Fetches; id++ {
		f := c.fetch(id)
		key := [2]int64{f.mid, f.generation}
		if keys[key] {
			t.Fatal("duplicate owned generation")
		}
		keys[key] = true
		states[f.status]++
		if f.eid != c.endpointID(f.mid) || f.scheduled.Before(c.created(f.mid)) {
			t.Fatal("bad owner/time")
		}
		if f.status == "pending" || f.status == "fetching" {
			if c.monitorStatus(f.rank) != "monitoring" {
				t.Fatal("unfinished terminal fetch")
			}
		}
		if f.status == "failed" {
			row := c.monitorRow(f.rank)
			if row[15] != "retry_wait" || row[4] != f.finished || row[10] != f.finished {
				t.Fatal("active transport failure must be a checked retry")
			}
		}
		if c.monitorStatus(f.rank) == "stopped" {
			row := c.monitorRow(f.rank)
			if row[22].(int64) <= f.generation {
				t.Fatal("stop did not fence previous generation")
			}
		}
	}
	if !reflect.DeepEqual(states, c.fetchParts) {
		t.Fatalf("fetch distribution: %v want %v", states, c.fetchParts)
	}
	for id := int64(1); id <= c.Counts.Evaluations; id++ {
		if c.fetch(id).status != "succeeded" {
			t.Fatal("non-success evaluation")
		}
	}
	for id := int64(1); id <= c.Counts.Notifications; id++ {
		n := c.notification(id)
		if contactOwners[n.cid] != c.userID(n.mid) {
			t.Fatal("wrong contact")
		}
		if c.monitorStatus(n.rank) != "success" && c.monitorStatus(n.rank) != "error" {
			t.Fatal("notification for nonterminal monitor")
		}
	}
	events := map[string]bool{}
	for i := int64(1); i <= c.Counts.Notifications; i++ {
		events[c.notificationRow(i)[3].(string)] = true
	}
	if !events["monitor.succeeded"] || !events["monitor.failed"] {
		t.Fatal("notification history must include both terminal event types")
	}
}
func TestThreeRuleLayoutAndExtraHistory(t *testing.T) {
	c := fixture(t)
	c.Counts.Rules = 3 * c.Counts.Monitors
	c.Counts.Fetches = 4000
	c.Counts.Outbox = 0
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	byMonitor := map[int64]int{}
	for i := int64(1); i <= c.Counts.Rules; i++ {
		row := c.ruleRow(i)
		byMonitor[row[1].(int64)]++
	}
	for _, n := range byMonitor {
		if n != 3 {
			t.Fatal("three-rule layout incomplete")
		}
	}
	previous := map[int64]fetch{}
	for i := int64(1); i <= c.Counts.Fetches; i++ {
		f := c.fetch(i)
		if p, ok := previous[f.mid]; ok {
			if f.generation <= p.generation || !f.scheduled.After(p.finished) {
				t.Fatal("history is not monotonic")
			}
		}
		previous[f.mid] = f
	}
}
func TestReproducibilityAcrossBatchSizes(t *testing.T) {
	a := fixture(t)
	b := a
	b.Batch = 17
	for _, id := range []int64{1, 2, 993, 1801, 2500} {
		if !reflect.DeepEqual(a.fetchRow(id), b.fetchRow(id)) {
			t.Fatal("batch changes rows")
		}
	}
	b.Seed++
	if reflect.DeepEqual(a.fetchRow(1), b.fetchRow(1)) {
		t.Fatal("seed ignored")
	}
}
func TestRejectInfeasibleCountsAndStatuses(t *testing.T) {
	for _, mutate := range []func(*config){func(c *config) { c.Counts.Rules = 4 * c.Counts.Monitors }, func(c *config) { c.Counts.Evaluations = c.Counts.Fetches }, func(c *config) { c.Counts.Notifications = 2 * c.Counts.Monitors }, func(c *config) { c.FetchWeights["invented"] = .1 }, func(c *config) { c.Overdue = 1.1 }, func(c *config) { c.Counts.Outbox = 1 }} {
		c := fixture(t)
		mutate(&c)
		if c.validate() == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
func TestDedicatedDSN(t *testing.T) {
	for _, dsn := range []string{"", "postgres://app@localhost/monitoring_service", "postgres://loadgen@localhost/monitoring_service_loadtest?host=production", "postgres://loadgen@localhost/monitoring_service_loadtest?dbname=prod"} {
		if _, _, err := safeDSN(dsn); err == nil {
			t.Fatal("unsafe DSN accepted")
		}
	}
	if _, _, err := safeDSN("postgres://loadgen@localhost/monitoring_service_loadtest?sslmode=disable"); err != nil {
		t.Fatal(err)
	}
}
func TestOptionalZeroCounts(t *testing.T) {
	c := fixture(t)
	c.Counts.Notifications = 0
	c.Counts.Processed = 0
	c.Counts.Keys = 0
	c.Counts.Outbox = 0
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	if c.Counts.Outbox != c.Counts.Fetches {
		t.Fatal("automatic outbox count")
	}
}
