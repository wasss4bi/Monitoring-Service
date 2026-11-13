// loadgen writes only to a marked, dedicated database. It never loads .env or application services.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lib/pq"
)

const marker = "monitoring_service dedicated synthetic loadtest v1"

var tables = []string{"users", "contacts", "target_hostnames", "user_contacts", "monitored_endpoints", "monitors", "monitor_rules", "endpoint_fetches", "monitor_fetch_evaluations", "notifications", "outbox_events", "processed_messages", "idempotency_keys", "contact_verifications", "operational_notifications", "service_dependencies"}

type fingerprint struct {
	Count int64  `json:"count"`
	XOR   string `json:"xor"`
	Sum   string `json:"sum"`
}
type report struct {
	Config            config                      `json:"config"`
	Started           time.Time                   `json:"started_at"`
	GenerationSeconds float64                     `json:"generation_seconds"`
	TotalSeconds      float64                     `json:"total_seconds"`
	Bytes             int64                       `json:"database_bytes"`
	Tables            map[string]fingerprint      `json:"tables"`
	Checks            map[string]int64            `json:"checks"`
	Distributions     map[string]map[string]int64 `json:"distributions"`
	Timings           map[string]float64          `json:"table_seconds"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "loadgen:", err)
		os.Exit(1)
	}
}
func run() error {
	mode := flag.String("mode", "large", "dataset mode (only large is supported)")
	path := flag.String("config", "", "JSON overrides; does not contain a DSN")
	out := flag.String("report", "loadgen-report.json", "result JSON file")
	reset := flag.Bool("reset", false, "clear an unchanged, previously completed synthetic dataset")
	confirmation := flag.String("confirm-reset", "", "exact database name required with --reset")
	dry := flag.Bool("dry-run", false, "print resolved config without connecting")
	validateOnly := flag.Bool("validate-only", false, "validate existing generated data without changing it")
	timeout := flag.Duration("timeout", 4*time.Hour, "overall database operation deadline")
	flag.Parse()
	c, err := loadConfig(*mode, *path)
	if err != nil {
		return err
	}
	if *dry {
		return json.NewEncoder(os.Stdout).Encode(c)
	}
	dbname, dsn, err := safeDSN(os.Getenv("LOADGEN_DATABASE_URL"))
	if err != nil {
		return err
	}
	for _, key := range []string{"APP_ENV", "ENVIRONMENT", "GO_ENV", "NODE_ENV"} {
		v := strings.ToLower(os.Getenv(key))
		if strings.Contains(v, "prod") {
			return fmt.Errorf("production environment is forbidden (%s)", key)
		}
	}
	if env := os.Getenv("LOADGEN_ENV"); env != "local" && env != "test" {
		return fmt.Errorf("LOADGEN_ENV must explicitly be local or test")
	}
	if *reset && *confirmation != dbname {
		return fmt.Errorf("--reset requires --confirm-reset=%s", dbname)
	}
	if *reset && *validateOnly {
		return fmt.Errorf("--reset and --validate-only are mutually exclusive")
	}
	// Open the output before touching the database so an invalid path cannot strand a successful run.
	var output *os.File
	if !*validateOnly {
		output, err = os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("create new report (use a fresh path): %w", err)
		}
		defer output.Close()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return errors.New("invalid PostgreSQL connection configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.New("connect to dedicated PostgreSQL failed; check LOADGEN_DATABASE_URL")
	}
	defer conn.Close()
	if err = guard(ctx, conn, dbname); err != nil {
		return err
	}
	var locked bool
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(719846123901)`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("another loadgen is running")
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(719846123901)`)
	if *validateOnly {
		var body []byte
		if err = conn.QueryRowContext(ctx, `SELECT report FROM loadgen_meta.run WHERE state='complete'`).Scan(&body); err != nil {
			return fmt.Errorf("read completed manifest: %w", err)
		}
		var saved report
		if err = json.Unmarshal(body, &saved); err != nil {
			return err
		}
		c = saved.Config
		if err = c.validate(); err != nil {
			return err
		}
		if saved.Checks, err = check(ctx, conn, c); err != nil {
			return err
		}
		actual, err := fingerprints(ctx, conn)
		if err != nil {
			return err
		}
		for _, table := range tables {
			if actual[table] != saved.Tables[table] {
				return fmt.Errorf("%s differs from the completed manifest", table)
			}
		}
		if saved.Distributions, err = distributions(ctx, conn, c.AsOf); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(saved)
	}
	if *reset {
		if err = resetData(ctx, conn); err != nil {
			return err
		}
	}
	if err = empty(ctx, conn); err != nil {
		return err
	}
	// Metadata is separate from application tables; no migration or application index is modified.
	if _, err = conn.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS loadgen_meta; CREATE TABLE IF NOT EXISTS loadgen_meta.run (state text NOT NULL, report jsonb NOT NULL); DELETE FROM loadgen_meta.run; INSERT INTO loadgen_meta.run VALUES ('building','{}')`); err != nil {
		return err
	}
	before, err := migrationFingerprint(ctx, conn)
	if err != nil {
		return err
	}
	r := report{Config: c, Started: time.Now().UTC(), Timings: map[string]float64{}}
	jobs := c.jobs()
	for _, job := range jobs {
		started := time.Now()
		if err = copyRows(ctx, conn, job, c.Batch); err != nil {
			return fmt.Errorf("%s: %w; partial data retained, recreate the dedicated database before retrying", job.name, err)
		}
		r.Timings[job.name] = time.Since(started).Seconds()
		fmt.Fprintf(os.Stderr, "%s: %d rows in %.2fs\n", job.name, job.count, r.Timings[job.name])
	}
	// COPY explicitly assigns identities; restore normal insert behavior without altering constraints.
	for _, table := range []string{"users", "contacts", "target_hostnames", "user_contacts", "monitored_endpoints", "monitors", "monitor_rules", "notifications", "outbox_events"} {
		q := `SELECT setval(pg_get_serial_sequence($1,'id'), COALESCE(MAX(id),1), MAX(id) IS NOT NULL) FROM ` + pq.QuoteIdentifier(table)
		if _, err = conn.ExecContext(ctx, q, table); err != nil {
			return err
		}
	}
	r.GenerationSeconds = time.Since(r.Started).Seconds()
	for _, table := range tables {
		if _, err = conn.ExecContext(ctx, `ANALYZE `+pq.QuoteIdentifier(table)); err != nil {
			return err
		}
	}
	if r.Checks, err = check(ctx, conn, c); err != nil {
		return err
	}
	if r.Tables, err = fingerprints(ctx, conn); err != nil {
		return err
	}
	for _, job := range jobs {
		expected := job.count
		if job.name == "users" {
			expected++
		}
		if r.Tables[job.name].Count != expected {
			return fmt.Errorf("%s: row count mismatch", job.name)
		}
	}
	if r.Distributions, err = distributions(ctx, conn, c.AsOf); err != nil {
		return err
	}
	after, err := migrationFingerprint(ctx, conn)
	if err != nil {
		return err
	}
	if before != after {
		return fmt.Errorf("schema_migrations changed during generation")
	}
	if err = conn.QueryRowContext(ctx, `SELECT pg_database_size(current_database())`).Scan(&r.Bytes); err != nil {
		return err
	}
	r.TotalSeconds = time.Since(r.Started).Seconds()
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `UPDATE loadgen_meta.run SET state='complete',report=$1::jsonb`, string(body)); err != nil {
		return err
	}
	if _, err = output.Write(body); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "complete: generation %.2fs, total %.2fs, database %d bytes; report %s\n", r.GenerationSeconds, r.TotalSeconds, r.Bytes, *out)
	return nil
}
func safeDSN(dsn string) (string, string, error) {
	u, err := url.Parse(dsn)
	if err != nil || u == nil || u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", "", fmt.Errorf("LOADGEN_DATABASE_URL must be an explicit postgres URL")
	}
	name := strings.TrimPrefix(u.Path, "/")
	if !strings.HasSuffix(name, "_loadtest") || strings.Contains(name, "/") || u.User == nil || u.User.Username() != "loadgen" || u.Hostname() == "" {
		return "", "", fmt.Errorf("dedicated database must end with _loadtest and connect as loadgen")
	}
	q := u.Query()
	for _, key := range []string{"host", "dbname", "user", "options", "service"} {
		if q.Has(key) {
			return "", "", fmt.Errorf("DSN query override %s is forbidden", key)
		}
	}
	q.Set("application_name", "monitoring-service-loadgen")
	q.Set("connect_timeout", "10")
	q.Set("search_path", "public")
	u.RawQuery = q.Encode()
	return name, u.String(), nil
}
func guard(ctx context.Context, c *sql.Conn, name string) error {
	var actual, user, comment string
	var clients int
	if err := c.QueryRowContext(ctx, `SELECT current_database(),current_user,COALESCE(shobj_description(oid,'pg_database'),'') FROM pg_database WHERE datname=current_database()`).Scan(&actual, &user, &comment); err != nil {
		return err
	}
	if actual != name || user != "loadgen" || comment != marker {
		return fmt.Errorf("dedicated database marker/identity does not match; use the isolated Compose setup")
	}
	if err := c.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND backend_type='client backend'`).Scan(&clients); err != nil {
		return err
	}
	if clients != 0 {
		return fmt.Errorf("database has %d other client sessions; stop them before loadgen", clients)
	}
	var version int
	var dirty bool
	if err := c.QueryRowContext(ctx, `SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		return fmt.Errorf("read migration state: %w", err)
	}
	if version != 2 || dirty {
		return fmt.Errorf("require clean current migration 2; found version=%d dirty=%t", version, dirty)
	}
	allowed := append(append([]string{}, tables...), "schema_migrations", "scheduler_capacity_locks")
	var unexpected int
	if err := c.QueryRowContext(ctx, `SELECT count(*) FROM pg_class t JOIN pg_namespace n ON n.oid=t.relnamespace WHERE n.nspname='public' AND t.relkind IN ('r','p') AND NOT(t.relname=ANY($1))`, pq.Array(allowed)).Scan(&unexpected); err != nil {
		return err
	}
	if unexpected > 0 {
		return fmt.Errorf("unexpected public tables: refusing to operate on an extended application schema")
	}
	if err := c.QueryRowContext(ctx, `SELECT count(*) FROM pg_constraint fk JOIN pg_class target ON target.oid=fk.confrelid JOIN pg_namespace tn ON tn.oid=target.relnamespace JOIN pg_class src ON src.oid=fk.conrelid JOIN pg_namespace sn ON sn.oid=src.relnamespace WHERE fk.contype='f' AND tn.nspname='public' AND target.relname=ANY($1) AND (sn.nspname<>'public' OR NOT(src.relname=ANY($1)))`, pq.Array(tables)).Scan(&unexpected); err != nil {
		return err
	}
	if unexpected > 0 {
		return fmt.Errorf("external foreign keys reference application tables; refusing generation/reset")
	}
	if err := c.QueryRowContext(ctx, `SELECT count(*) FROM pg_trigger tr JOIN pg_class t ON t.oid=tr.tgrelid JOIN pg_namespace n ON n.oid=t.relnamespace WHERE n.nspname='public' AND NOT tr.tgisinternal`).Scan(&unexpected); err != nil {
		return err
	}
	if unexpected > 0 {
		return fmt.Errorf("custom triggers are not supported by the isolated dataset generator")
	}
	return nil
}
func empty(ctx context.Context, c *sql.Conn) error {
	for _, t := range tables {
		var n int64
		q := `SELECT count(*) FROM ` + pq.QuoteIdentifier(t)
		if t == "users" {
			q += ` WHERE NOT(id=1 AND name='Administrator' AND role='admin')`
		}
		if err := c.QueryRowContext(ctx, q).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("%s contains existing data; refusing to append or delete it", t)
		}
	}
	var baseline int
	if err := c.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id=1`).Scan(&baseline); err != nil {
		return err
	}
	if baseline != 1 {
		return fmt.Errorf("expected bootstrap administrator id=1")
	}
	return nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func fingerprints(ctx context.Context, c queryer) (map[string]fingerprint, error) {
	result := map[string]fingerprint{}
	for _, t := range tables {
		var f fingerprint
		q := `SELECT count(*),COALESCE(bit_xor(hashtextextended(row_to_json(x)::text,0)),0)::text,COALESCE(sum(hashtextextended(row_to_json(x)::text,1)::numeric),0)::text FROM ` + pq.QuoteIdentifier(t) + ` x`
		if err := c.QueryRowContext(ctx, q).Scan(&f.Count, &f.XOR, &f.Sum); err != nil {
			return nil, err
		}
		result[t] = f
	}
	return result, nil
}
func migrationFingerprint(ctx context.Context, c queryer) (string, error) {
	var value string
	err := c.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(s))::text,'[]') FROM schema_migrations s`).Scan(&value)
	return value, err
}
func resetData(ctx context.Context, c *sql.Conn) error {
	tx, err := c.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	names := make([]string, len(tables))
	for i, t := range tables {
		names[i] = pq.QuoteIdentifier(t)
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL lock_timeout='5s'; LOCK TABLE `+strings.Join(names, ",")+` IN ACCESS EXCLUSIVE MODE`); err != nil {
		return err
	}
	var state string
	var body []byte
	if err = tx.QueryRowContext(ctx, `SELECT state,report FROM loadgen_meta.run`).Scan(&state, &body); err != nil {
		return fmt.Errorf("reset requires a completed loadgen manifest: %w", err)
	}
	if state != "complete" {
		return fmt.Errorf("partial dataset cannot be reset automatically; recreate dedicated database")
	}
	var previous report
	if err = json.Unmarshal(body, &previous); err != nil {
		return err
	}
	actual, err := fingerprints(ctx, tx)
	if err != nil {
		return err
	}
	for _, t := range tables {
		if actual[t] != previous.Tables[t] {
			return fmt.Errorf("%s changed after generation; refusing reset to preserve existing data", t)
		}
	}
	// Never CASCADE: a new referencing table must make reset fail rather than lose its rows.
	if _, err = tx.ExecContext(ctx, `TRUNCATE contact_verifications,operational_notifications,service_dependencies,contacts,target_hostnames,user_contacts,monitored_endpoints,monitors,monitor_rules,endpoint_fetches,monitor_fetch_evaluations,notifications,outbox_events,processed_messages,idempotency_keys RESTART IDENTITY`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM users WHERE id<>1`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM loadgen_meta.run`); err != nil {
		return err
	}
	return tx.Commit()
}

type copyJob struct {
	name    string
	columns []string
	count   int64
	row     func(int64) []any
	next    func() (row []any, ok bool)
}

func copyRows(ctx context.Context, c *sql.Conn, job copyJob, batch int64) error {
	for start := int64(1); start <= job.count; start += batch {
		end := min(start+batch-1, job.count)
		err := func() error {
			tx, err := c.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			stmt, err := tx.PrepareContext(ctx, pq.CopyIn(job.name, job.columns...))
			if err != nil {
				return err
			}
			defer stmt.Close()
			for i := start; i <= end; i++ {
				var row []any
				if job.next != nil {
					var ok bool
					row, ok = job.next()
					if !ok {
						return io.ErrUnexpectedEOF
					}
				} else {
					row = job.row(i)
				}
				if _, err = stmt.ExecContext(ctx, row...); err != nil {
					return err
				}
			}
			if _, err = stmt.ExecContext(ctx); err != nil {
				return err
			}
			if err = stmt.Close(); err != nil {
				return err
			}
			return tx.Commit()
		}()
		if err != nil {
			return fmt.Errorf("COPY rows %d..%d: %w", start, end, err)
		}
	}
	return nil
}

func (c config) jobs() []copyJob {
	processedNotification := int64(1)
	processedFetch := int64(1)
	processedIndex := int64(0)
	// Alternate consumers while candidates remain, always referencing a published, completed event.
	inbox := func() ([]any, bool) {
		processedIndex++
		preferNotification := processedIndex%5 == 0 || processedFetch > c.good+c.bad
		for tries := 0; tries < 2; tries++ {
			if preferNotification {
				for processedNotification <= c.Counts.Notifications {
					id := processedNotification
					processedNotification++
					n := c.notification(id)
					if n.state == "pending" || n.state == "sending" {
						continue
					}
					return []any{"notification-worker", c.uuid("event", c.Counts.Fetches+id), n.created.Add(2 * time.Second)}, true
				}
			} else if processedFetch <= c.good+c.bad {
				id := processedFetch
				processedFetch++
				f := c.fetch(id)
				return []any{"endpoint-fetch-worker", c.uuid("event", id), f.finished.Add(time.Millisecond)}, true
			}
			preferNotification = !preferNotification
		}
		return nil, false
	}
	contactCount := 2 * c.Counts.Users
	for u := int64(0); u < c.Counts.Users; u++ {
		if c.hash("third-contact", u)%5 == 0 {
			contactCount++
		}
	}
	contactUser := int64(0)
	contactSlot := int64(0)
	contactNext := func() ([]any, bool) {
		for contactUser < c.Counts.Users {
			u := contactUser
			slot := contactSlot
			contactSlot++
			if contactSlot == 3 {
				contactSlot = 0
				contactUser++
			}
			if slot == 2 && c.hash("third-contact", u)%5 != 0 {
				continue
			}
			id := 3*u + slot + 1
			channel := int64(1)
			value := fmt.Sprintf("user-%d-%d@loadgen.invalid", u, slot)
			if slot == 1 {
				channel = 2
				value = fmt.Sprintf("%d", -900000000000-u)
			}
			return []any{id, u + 2, channel, value, slot < 2, slot < 2, c.From, c.AsOf}, true
		}
		return nil, false
	}
	j := func(name, cols string, n int64, row func(int64) []any) copyJob {
		return copyJob{name: name, columns: strings.Split(cols, ","), count: n, row: row}
	}
	jobs := []copyJob{
		j("users", "id,name,email,password", c.Counts.Users, func(i int64) []any {
			return []any{i + 1, fmt.Sprintf("Synthetic user %d", i), fmt.Sprintf("user-%d@loadgen.invalid", i), "!loadgen-disabled"}
		}),
		j("contacts", "id,name", 2, func(i int64) []any { return []any{i, []string{"email", "telegram"}[i-1]} }),
		j("target_hostnames", "id,hostname,bootstrap_url,limit_per_minute,is_active,created_at,updated_at", c.Counts.Hosts, func(i int64) []any {
			return []any{i, c.hostname(i), "https://" + c.hostname(i) + "/", 1 + int64(c.hash("host-budget", i)%300), c.hash("host-active", i)%20 != 0, c.From, c.From}
		}),
		{name: "user_contacts", columns: strings.Split("id,user_id,contact_id,contact,is_verified,is_active,created_at,updated_at", ","), count: contactCount, next: contactNext},
		j("monitored_endpoints", "id,canonical_url,status,next_check_at,last_attempt_at,last_success_at,last_checked_at,attempts_count,consecutive_errors,retry_started_at,last_error,lease_token,lease_until,generation,created_at,updated_at,type,target_hostname_id", c.Counts.Endpoints, c.endpointRow),
		j("monitors", "id,user_id,endpoint_id,status,last_checked_at,last_error,completed_at,stopped_at,deleted_at,created_at,updated_at,name,path_type,rules_count_to_finish,page_url,check_status,next_check_at,attempts_count,consecutive_errors,retry_started_at,last_attempt_at,last_success_at,generation", c.Counts.Monitors, c.monitorRow),
		j("monitor_rules", "id,monitor_id,json_path,comparison_operator,expected_value,created_at,number_format,description", c.Counts.Rules, c.ruleRow),
		j("endpoint_fetches", "fetch_id,endpoint_id,generation,scheduled_at,started_at,finished_at,status,lease_token,lease_until,response_status,response_json,last_error,created_at,monitor_id", c.Counts.Fetches, c.fetchRow),
		j("monitor_fetch_evaluations", "fetch_id,monitor_id,created_at", c.Counts.Evaluations, func(i int64) []any {
			f := c.fetch(i)
			return []any{c.uuid("fetch", i), f.mid, f.finished.Add(-time.Millisecond)}
		}),
		j("notifications", "id,monitor_id,user_contact_id,event_type,message,status,attempts_count,last_error,next_attempt_at,created_at,sent_at,idempotency_key", c.Counts.Notifications, c.notificationRow),
		j("outbox_events", "id,created_at,published_at,lease_token,lease_until,publish_attempts,event_id,event_type,aggregate_type,aggregate_id,payload,next_attempt_at,last_error", c.Counts.Outbox, c.outboxRow),
		{name: "processed_messages", columns: []string{"consumer_name", "message_id", "processed_at"}, count: c.Counts.Processed, next: inbox},
		j("idempotency_keys", "key,status,request_hash,response_code,response_body,created_at,expires_at,user_id,ip,device,browser", c.Counts.Keys, func(i int64) []any {
			created := c.From.Add(time.Duration(c.fraction("key-time", i) * float64(c.AsOf.Sub(c.From))))
			state := "completed"
			var code, body any
			code = 204
			body = nil
			if c.hash("key-processing", i)%20 == 0 {
				state = "processing"
				code = nil
			}
			return []any{c.uuid("idempotency", i), state, fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("synthetic/%d/%d", c.Seed, i)))), code, body, created, created.Add(time.Hour), 2 + (i-1)%c.Counts.Users, "192.0.2.1", "loadgen", "synthetic"}
		}),
	}
	return jobs
}
