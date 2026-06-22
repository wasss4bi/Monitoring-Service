package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"
)

// Stateless streams keep output independent of COPY batch boundaries and avoid retaining rows.
func (c config) hash(stream string, i int64) uint64 {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d/%s/%d", c.Seed, stream, i)))
	return binary.BigEndian.Uint64(h[:8])
}
func (c config) fraction(stream string, i int64) float64 {
	return float64(c.hash(stream, i)>>11) / float64(uint64(1)<<53)
}
func (c config) uuid(stream string, i int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d/%s/%d", c.Seed, stream, i)))
	h[6] = (h[6] & 15) | 0x50
	h[8] = (h[8] & 63) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
func (c config) weighted(stream string, i int64, w map[string]float64, states []string) string {
	v := c.fraction(stream, i)
	for _, s := range states {
		v -= w[s]
		if v < 0 {
			return s
		}
	}
	return states[len(states)-1]
}
func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
func (c config) permute(rank, n int64) int64 {
	if n <= 0 {
		panic("permutation size must be positive")
	}
	a := int64(104729)
	for gcd(a, n) != 1 {
		a += 2
	}
	return ((rank-1)*a+int64(c.Seed%uint64(n)))%n + 1 // #nosec G115 -- n is positive int64, so the remainder is within int64 range.
}
func (c config) monitorID(rank int64) int64 { return c.permute(rank, c.Counts.Monitors) }
func (c config) monitorStatus(rank int64) string {
	if rank <= c.terminal {
		if c.permute(rank, c.terminal) <= c.success {
			return "success"
		}
		return "error"
	}
	if rank <= c.terminal+c.active {
		return "monitoring"
	}
	return "stopped"
}
func (c config) endpointID(mid int64) int64 { return (mid-1)%c.Counts.Endpoints + 1 }
func (c config) userID(mid int64) int64 {
	u := c.fraction("user", mid)
	if c.fraction("heavy-user", mid) < .70 {
		return 2 + int64(u*float64(max(1, c.Counts.Users/10)))
	}
	return 2 + int64(u*float64(c.Counts.Users))
}
func (c config) hostID(eid int64) int64    { return (eid-1)%c.Counts.Hosts + 1 }
func (c config) hostname(hid int64) string { return fmt.Sprintf("host-%d.loadgen.invalid", hid) }
func (c config) url(eid int64) string {
	return fmt.Sprintf("https://%s/resources/%d", c.hostname(c.hostID(eid)), eid)
}
func (c config) endpointType(eid int64) string {
	if c.fraction("type", eid) < c.HTML {
		return "html"
	}
	return "json"
}
func (c config) created(mid int64) time.Time {
	span := c.AsOf.Sub(c.From) - time.Hour
	return c.From.Add(time.Duration(float64(span) * (.1 + .7*c.fraction("created", mid))))
}
func (c config) due(stream string, id int64) time.Time {
	if c.fraction(stream+"-overdue", id) < c.Overdue {
		return c.AsOf.Add(-time.Duration(1+c.hash(stream, id)%600) * time.Second)
	}
	return c.AsOf.Add(time.Duration(1+c.hash(stream, id)%604800) * time.Second)
}
func (c config) notificationStatus(id int64) string {
	return c.weighted("notification-status", id, c.NotificationWeights, notificationStates)
}
func (c config) terminalTime(rank int64) time.Time {
	for n := 2*rank - 1; n <= 2*rank && n <= c.Counts.Notifications; n++ {
		s := c.notificationStatus(n)
		if s == "pending" || s == "sending" {
			return c.AsOf.Add(-time.Minute)
		}
	}
	mid := c.monitorID(rank)
	created := c.created(mid)
	return created.Add(time.Duration(float64(c.AsOf.Sub(created)-10*time.Minute) * (.1 + .8*c.fraction("terminal-time", mid))))
}
func (c config) successCount(rank int64) int64 {
	if rank <= c.terminal || rank > c.terminal+c.active {
		return 1
	}
	a := rank - c.terminal
	extra := c.good - c.Counts.Monitors
	return 1 + extra/c.active + boolInt(a <= extra%c.active)
}
func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

type fetch struct {
	id, rank, mid, eid, generation int64
	status                         string
	scheduled, started, finished   time.Time
}

func (c config) fetch(id int64) fetch {
	f := fetch{id: id, status: "succeeded", generation: 1}
	switch {
	case id <= c.Counts.Monitors:
		f.rank = id
	case id <= c.good:
		f.rank = c.terminal + 1 + (id-c.Counts.Monitors-1)%c.active
		f.generation = 2 + (id-c.Counts.Monitors-1)/c.active
	default:
		a := id - c.good
		f.rank = c.terminal + a
		switch {
		case a <= c.bad:
			f.status = "failed"
		case a <= c.bad+c.pending:
			f.status = "pending"
		default:
			f.status = "fetching"
		}
		f.generation = c.successCount(f.rank) + 1
	}
	f.mid = c.monitorID(f.rank)
	f.eid = c.endpointID(f.mid)
	if f.status == "succeeded" {
		if f.rank <= c.terminal {
			f.finished = c.terminalTime(f.rank)
		} else {
			created := c.created(f.mid)
			f.finished = created.Add(time.Duration(float64(c.AsOf.Add(-15*time.Minute).Sub(created)) * float64(f.generation) / float64(c.successCount(f.rank)+1)))
		}
		f.started = f.finished.Add(-time.Duration(200+c.hash("latency", id)%4800) * time.Millisecond)
		f.scheduled = f.started.Add(-time.Duration(50+c.hash("queue", id)%500) * time.Millisecond)
	} else {
		f.scheduled = c.AsOf.Add(-time.Duration(90+c.hash("live-age", id)%20) * time.Second)
		f.started = f.scheduled.Add(100 * time.Millisecond)
		if f.status == "failed" {
			f.finished = f.started.Add(time.Second)
		}
	}
	return f
}
func (c config) latestSuccess(rank int64) fetch {
	if c.successCount(rank) == 1 {
		return c.fetch(rank)
	}
	return c.fetch(c.Counts.Monitors + (c.successCount(rank)-2)*c.active + (rank - c.terminal))
}
func (c config) checkState(rank int64) (string, *fetch) {
	a := rank - c.terminal
	if a > 0 && a <= c.bad+c.pending+c.fetching {
		f := c.fetch(c.good + a)
		if f.status == "pending" {
			return "scheduled", &f
		}
		if f.status == "fetching" {
			return "fetching", &f
		}
		return "retry_wait", &f
	}
	return "idle", nil
}
func (c config) rulesCount(mid int64) int64 {
	extra := c.Counts.Rules - c.Counts.Monitors
	return 1 + boolInt(mid <= extra) + boolInt(mid <= extra-c.Counts.Monitors)
}
func marshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
func (c config) endpointRow(id int64) []any {
	state := c.weighted("endpoint-status", id, c.EndpointWeights, endpointStates)
	created := c.From
	checked := c.created(id)
	var lease, until, lastErr, retry, next any
	if state == "scheduled" || state == "retry_wait" || state == "idle" {
		next = c.due("endpoint-due", id)
	}
	attempts := int64(0)
	errors := int64(0)
	if state == "retry_wait" || state == "error" || state == "access_denied" {
		lastErr = "synthetic upstream failure"
		retry = checked.Add(-time.Minute)
		attempts = 1 + int64(c.hash("attempts", id)%5)
		errors = attempts
	}
	if state == "fetching" {
		lease = c.uuid("endpoint-lease", id)
		until = c.due("endpoint-lease", id)
	}
	return []any{id, c.url(id), state, next, checked, checked.Add(-time.Minute), checked, attempts, errors, retry, lastErr, lease, until, 1 + int64(c.hash("generation", id)%50), created, checked, c.endpointType(id), c.hostID(id)}
}
func (c config) monitorRow(rank int64) []any {
	mid := c.monitorID(rank)
	eid := c.endpointID(mid)
	status := c.monitorStatus(rank)
	check, current := c.checkState(rank)
	latest := c.latestSuccess(rank)
	created := c.created(mid)
	updated := latest.finished
	generation := latest.generation
	var completed, stopped, deleted, errText, retry, next any
	var attempts int64
	if status == "success" || status == "error" {
		completed = c.terminalTime(rank)
		if status == "error" {
			errText = "synthetic rule value is missing"
		}
	}
	if status == "stopped" {
		stopped = c.AsOf.Add(-5 * time.Minute)
		updated = stopped.(time.Time)
		generation++
	}
	if current != nil {
		generation = current.generation
		updated = current.started
		if check == "retry_wait" || check == "error" || check == "access_denied" {
			errText = "synthetic upstream failure"
			attempts = 1 + int64(c.hash("attempts", mid)%4)
			retry = current.scheduled
		}
	}
	if status == "monitoring" && (check == "idle" || check == "retry_wait") {
		next = c.due("monitor-due", mid)
	}
	if check == "scheduled" {
		next = c.AsOf.Add(10 * time.Minute)
	}
	if status == "monitoring" && current == nil && c.fraction("soft-delete", mid) < c.Deleted {
		deleted = c.AsOf.Add(-time.Minute)
		updated = deleted.(time.Time)
		generation++
	}
	path := "json"
	if c.endpointType(eid) == "html" {
		path = "selector"
		if c.hash("path-type", mid)%2 == 0 {
			path = "xpath"
		}
	}
	lastAttempt := latest.finished
	lastChecked := latest.finished
	if current != nil {
		lastAttempt = current.started
		if current.status == "failed" {
			lastAttempt = current.finished
			lastChecked = current.finished
			updated = current.finished
		}
	}
	return []any{mid, c.userID(mid), eid, status, lastChecked, errText, completed, stopped, deleted, created, updated, fmt.Sprintf("Synthetic monitor %d", mid), path, c.rulesCount(mid), c.url(eid), check, next, attempts, attempts, retry, lastAttempt, latest.finished, generation}
}
func (c config) ruleRow(id int64) []any {
	mid := id
	ordinal := int64(0)
	if id > c.Counts.Monitors {
		ordinal = 1
		mid = id - c.Counts.Monitors
		if mid > c.Counts.Monitors {
			ordinal = 2
			mid -= c.Counts.Monitors
		}
	}
	path := []string{"price", "stock.available", "offers[0].price"}[ordinal]
	format := "number"
	if c.endpointType(c.endpointID(mid)) == "html" {
		if c.hash("path-type", mid)%2 == 0 {
			path = []string{"//span[@class='price']", "//div[@id='stock']", "//span[@data-price]"}[ordinal]
		} else {
			path = []string{".price", "#stock", "[data-price]"}[ordinal]
		}
		if ordinal != 1 {
			format = "money"
		}
	}
	op := []string{"<", ">", "=", "<=", ">=", "!="}[c.hash("operator", id)%6]
	return []any{id, mid, path, op, float64(1+c.hash("expected", id)%100000) / 100, c.created(mid), format, fmt.Sprintf("Synthetic condition %d", id)}
}
func (c config) fetchRow(id int64) []any {
	f := c.fetch(id)
	var started, finished, token, until, http, body, lastErr any
	if f.status != "pending" {
		started = f.started
	}
	if f.status == "succeeded" || f.status == "failed" {
		finished = f.finished
	}
	if f.status == "fetching" {
		token = c.uuid("fetch-lease", id)
		until = c.AsOf.Add(30 * time.Second)
		if c.fraction("expired-lease", id) < c.Overdue {
			until = c.AsOf.Add(-15 * time.Second)
		}
	}
	switch f.status {
	case "succeeded":
		http = []int{200, 200, 200, 201, 204}[c.hash("http", id)%5]
		body = `{}`
	case "failed":
		http = []any{429, 500, 502, 503, 504, nil}[c.hash("http-error", id)%6]
		lastErr = "synthetic upstream failure"
	}
	return []any{c.uuid("fetch", id), f.eid, f.generation, f.scheduled, started, finished, f.status, token, until, http, body, lastErr, f.scheduled, f.mid}
}

type notification struct {
	id, rank, mid, cid int64
	state              string
	created            time.Time
}

func (c config) notification(id int64) notification {
	rank := (id + 1) / 2
	mid := c.monitorID(rank)
	return notification{id, rank, mid, (c.userID(mid)-2)*3 + 1 + (id-1)%2, c.notificationStatus(id), c.terminalTime(rank)}
}
func (c config) notificationRow(id int64) []any {
	n := c.notification(id)
	event := "monitor.succeeded"
	message := fmt.Sprintf("Monitor %q triggered!", fmt.Sprintf("Synthetic monitor %d", n.mid))
	if c.monitorStatus(n.rank) == "error" {
		event = "monitor.failed"
		message = fmt.Sprintf("Monitor %q stopped: synthetic rule value is missing", fmt.Sprintf("Synthetic monitor %d", n.mid))
	}
	attempts := int64(0)
	var errText, sent any
	next := n.created
	if n.state == "sent" {
		attempts = 1
		sent = n.created.Add(time.Second)
	}
	if n.state == "sending" {
		attempts = 1
		next = c.AsOf.Add(30 * time.Second)
		if c.fraction("notification-expired", id) < c.Overdue {
			next = c.AsOf.Add(-time.Second)
		}
	}
	if n.state == "failed" {
		attempts = 6
		errText = "synthetic delivery retry budget exhausted"
	}
	if n.state == "skipped" {
		attempts = 0
		errText = "contact was inactive at delivery time"
	}
	if n.state == "pending" {
		if c.fraction("notification-retry", id) < c.Errors {
			attempts = 1 + int64(c.hash("notification-attempts", id)%4)
			errText = "synthetic temporary delivery error"
		}
		next = c.AsOf.Add(20 * time.Second)
		if c.fraction("notification-due", id) < c.Overdue {
			next = c.AsOf.Add(-time.Second)
		}
	}
	return []any{id, n.mid, n.cid, event, message, n.state, attempts, errText, next, n.created, sent, c.uuid("notification-key", id)}
}
func (c config) outboxRow(id int64) []any {
	var event, aggregate, payload string
	var aggregateID int64
	var created time.Time
	eligible := false
	if id <= c.Counts.Fetches || id > c.Counts.Fetches+c.Counts.Notifications {
		fid := id
		if fid > c.Counts.Fetches {
			fid = c.good + c.bad + 1 + (id-c.Counts.Fetches-c.Counts.Notifications-1)%c.pending
		}
		f := c.fetch(fid)
		event = "endpoint.fetch." + c.endpointType(f.eid)
		aggregate = "monitor"
		aggregateID = f.mid
		payload = marshal(map[string]string{"fetch_id": c.uuid("fetch", fid)})
		created = f.scheduled
		eligible = f.status == "pending"
	} else {
		n := c.notification(id - c.Counts.Fetches)
		event = "notification.deliver"
		aggregate = "notification"
		aggregateID = n.id
		payload = marshal(map[string]int64{"notification_id": n.id})
		created = n.created
		eligible = n.state == "pending"
	}
	var published, token, until, lastErr any
	next := created
	attempts := int64(1)
	if eligible && c.fraction("unpublished", id) < c.Unpublished {
		attempts = 0
		next = c.AsOf.Add(20 * time.Second)
		if c.fraction("outbox-due", id) < c.Overdue {
			next = c.AsOf.Add(-time.Second)
		}
		if c.fraction("outbox-error", id) < c.Errors {
			attempts = 1 + int64(c.hash("outbox-attempts", id)%5)
			lastErr = "synthetic broker unavailable"
		}
		if c.hash("outbox-lease", id)%4 == 0 {
			token = c.uuid("outbox-lease", id)
			attempts++
			until = c.AsOf.Add(30 * time.Second)
			if c.fraction("outbox-expired", id) < c.Overdue {
				until = c.AsOf.Add(-time.Second)
			}
		}
	} else {
		published = created.Add(50 * time.Millisecond)
	}
	return []any{id, created, published, token, until, attempts, c.uuid("event", id), event, aggregate, aggregateID, payload, next, lastErr}
}
