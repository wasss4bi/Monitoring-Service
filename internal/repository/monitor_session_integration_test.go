//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"monitoring_service/internal/domain"
)

// Run explicitly against the current DB, but isolate fixtures in a new schema.
// No public table or live delivery is changed by this test.
func monitorSessionDB(t *testing.T, through ...int) *sql.DB {
	t.Helper()
	if os.Getenv("MONITORING_SERVICE_CURRENT_DB_TESTS") != "1" {
		t.Skip("MONITORING_SERVICE_CURRENT_DB_TESTS=1 required")
	}
	u, err := url.Parse(os.Getenv("DATABASE_URL"))
	if err != nil || u.Host == "" {
		t.Fatal("DATABASE_URL is required")
	}
	admin, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	name := "test_monitor_sessions_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.ExecContext(t.Context(), "CREATE SCHEMA "+pq.QuoteIdentifier(name)); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", name)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP SCHEMA "+pq.QuoteIdentifier(name)+" CASCADE"); err != nil {
			t.Errorf("cleanup owned schema: %v", err)
		}
		admin.Close()
	})
	paths, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if len(through) > 0 {
			prefix, _, _ := strings.Cut(filepath.Base(p), "_")
			version, err := strconv.Atoi(prefix)
			if err != nil {
				t.Fatal(err)
			}
			if version > through[0] {
				continue
			}
		}
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(filepath.Base(p), "000002_") {
			// Isolated schema fixture; production migrator supplies a bcrypt hash.
			if _, err := db.ExecContext(t.Context(), `SELECT set_config('monitoring_service.admin_email','admin-fixture@example.com',false),set_config('monitoring_service.admin_password_hash','$2a$10$fixturehashonly',false)`); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = db.ExecContext(t.Context(), string(body)); err != nil {
			t.Fatalf("migration %s: %v", filepath.Base(p), err)
		}
	}
	return db
}
func monitorSessionPair(t *testing.T, db *sql.DB) (domain.Monitor, domain.Monitor) {
	t.Helper()
	var target, contact int64
	if err := db.QueryRowContext(t.Context(), "INSERT INTO target_hostnames(hostname,limit_per_minute) VALUES('sessions.example.com',60) RETURNING id").Scan(&target); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "INSERT INTO contacts(name) VALUES('email') RETURNING id").Scan(&contact); err != nil {
		t.Fatal(err)
	}
	out := make([]domain.Monitor, 0, 2)
	for i := range 2 {
		var user int64
		if err := db.QueryRowContext(t.Context(), "INSERT INTO users(name,email,password) VALUES('Test',$1,'unused') RETURNING id", fmt.Sprintf("subscriber%d@example.com", i)).Scan(&user); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(t.Context(), "INSERT INTO user_contacts(user_id,contact_id,contact,is_active,is_verified) VALUES($1,$2,$3,true,true)", user, contact, fmt.Sprintf("subscriber%d@example.com", i)); err != nil {
			t.Fatal(err)
		}
		m, err := NewMonitorRepository(db).Create(t.Context(), domain.CreateMonitorInput{UserID: user, TargetHostnameID: target,
			PageURL: "https://sessions.example.com/product", CanonicalURL: "https://sessions.example.com/price", EndpointType: "json", PathType: "json", Name: "isolated",
			RulesCountToFinish: 1, Headers: map[string]string{"X-Subscriber": fmt.Sprint(i)}, SessionMode: "provided", SessionCiphertext: fmt.Sprintf("ciphertext-%d", i),
			Rules: []domain.CreateMonitorRuleInput{{Description: "Price <= 75000", JSONPath: "price", ComparisonOperator: "<=", ExpectedValue: 75000}}})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	if out[0].EndpointID != out[1].EndpointID {
		t.Fatal("canonical URL split")
	}
	return out[0], out[1]
}
func TestSubscriberSchedulingAndLeaseIsolation(t *testing.T) {
	db := monitorSessionDB(t)
	a, b := monitorSessionPair(t, db)
	r := NewMonitoringExecutionRepository(db)
	n, err := r.ScheduleDue(t.Context(), time.Minute, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 subscriber jobs, got %d", n)
	}
	for i, m := range []domain.Monitor{a, b} {
		var fetch string
		if err := db.QueryRowContext(t.Context(), "SELECT fetch_id::text FROM endpoint_fetches WHERE monitor_id=$1", m.ID).Scan(&fetch); err != nil {
			t.Fatal(err)
		}
		job, err := r.ClaimFetch(t.Context(), fetch, uuid.NewString(), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if job.MonitorID != m.ID || job.SessionCiphertext != fmt.Sprintf("ciphertext-%d", i) {
			t.Fatal("subscriber credentials crossed")
		}
		if len(job.Headers) != 0 {
			t.Fatal("disabled legacy headers reached the worker job")
		}
		loaded, err := NewMonitorRepository(db).GetByUser(t.Context(), m.UserID, m.ID)
		if err != nil || len(loaded.Headers) != 0 {
			t.Fatal("disabled legacy headers read from the database")
		}
	}
	if n, err = r.ScheduleDue(t.Context(), time.Minute, 100); err != nil || n != 0 {
		t.Fatalf("double scheduling: %d %v", n, err)
	}
}
func TestSubscriberReplacementFencesInflightResult(t *testing.T) {
	db := monitorSessionDB(t)
	a, _ := monitorSessionPair(t, db)
	r := NewMonitoringExecutionRepository(db)
	if _, err := r.ScheduleDue(t.Context(), time.Minute, 100); err != nil {
		t.Fatal(err)
	}
	var fetch string
	if err := db.QueryRowContext(t.Context(), "SELECT fetch_id::text FROM endpoint_fetches WHERE monitor_id=$1", a.ID).Scan(&fetch); err != nil {
		t.Fatal(err)
	}
	token := uuid.NewString()
	if _, err := r.ClaimFetch(t.Context(), fetch, token, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMonitorRepository(db).ReplaceSession(t.Context(), a.UserID, a.ID, "provided", "replacement"); err != nil {
		t.Fatal(err)
	}
	if err := r.FinishMonitor(t.Context(), fetch, token, a.ID, true, "stale response", a.Rules); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRowContext(t.Context(), "SELECT status FROM monitors WHERE id=$1", a.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "monitoring" {
		t.Fatal("old credentials completed monitor")
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM notifications").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("old response sent notification")
	}
}
func TestSubscriberFailureAndRetryAreIndependent(t *testing.T) {
	db := monitorSessionDB(t)
	a, b := monitorSessionPair(t, db)
	r := NewMonitoringExecutionRepository(db)
	if _, err := r.ScheduleDue(t.Context(), time.Minute, 100); err != nil {
		t.Fatal(err)
	}
	var fetchA, fetchB string
	if err := db.QueryRowContext(t.Context(), "SELECT fetch_id::text FROM endpoint_fetches WHERE monitor_id=$1", a.ID).Scan(&fetchA); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT fetch_id::text FROM endpoint_fetches WHERE monitor_id=$1", b.ID).Scan(&fetchB); err != nil {
		t.Fatal(err)
	}
	tokenA, tokenB := uuid.NewString(), uuid.NewString()
	job, err := r.ClaimFetch(t.Context(), fetchA, tokenA, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if acquired, err := r.AcquireSessionRefresh(t.Context(), a.ID, job.SessionGeneration, uuid.NewString(), time.Minute); err != nil || acquired {
		t.Fatal("provided session entered automatic refresh")
	}
	if err := r.RejectMonitorSession(t.Context(), a.ID, job.SessionGeneration); err != nil {
		t.Fatal(err)
	}
	if err := r.FinishMonitor(t.Context(), fetchA, tokenA, a.ID, false, "session rejected", nil); err != nil {
		t.Fatal(err)
	}
	code := 403
	if _, err := r.CompleteFetchFailure(t.Context(), fetchA, tokenA, uuid.NewString(), &code, "session rejected", 0, false, "access_denied"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ClaimFetch(t.Context(), fetchB, tokenB, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := r.FinishMonitor(t.Context(), fetchB, tokenB, b.ID, true, "fresh response", b.Rules); err != nil {
		t.Fatal(err)
	}
	if err := r.CompleteFetchSuccess(t.Context(), fetchB, tokenB, uuid.NewString(), 200, []byte(`{"price":68150}`), time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id            int64
		status, check string
	}{{a.ID, "error", "access_denied"}, {b.ID, "success", "idle"}} {
		var status, check string
		if err := db.QueryRowContext(t.Context(), "SELECT status,check_status FROM monitors WHERE id=$1", item.id).Scan(&status, &check); err != nil {
			t.Fatal(err)
		}
		if status != item.status || check != item.check {
			t.Fatalf("monitor %d: %s/%s", item.id, status, check)
		}
	}
	var active bool
	if err := db.QueryRowContext(t.Context(), "SELECT is_active FROM target_hostnames").Scan(&active); err != nil || !active {
		t.Fatal("subscriber error blocked hostname")
	}
	if err := NewMonitorRepository(db).Resume(t.Context(), a.UserID, a.ID); err != ErrMonitorNotResumable {
		t.Fatal("rejected provided session resumed without replacement")
	}
}

func TestSubscriberRecoveryAndDeferral(t *testing.T) {
	db := monitorSessionDB(t)
	a, _ := monitorSessionPair(t, db)
	r := NewMonitoringExecutionRepository(db)
	if _, err := r.ScheduleDue(t.Context(), time.Minute, 100); err != nil {
		t.Fatal(err)
	}
	var fetch string
	if err := db.QueryRowContext(t.Context(), "SELECT fetch_id::text FROM endpoint_fetches WHERE monitor_id=$1", a.ID).Scan(&fetch); err != nil {
		t.Fatal(err)
	}
	token := uuid.NewString()
	if _, err := r.ClaimFetch(t.Context(), fetch, token, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := r.DeferFetch(t.Context(), fetch, token, uuid.NewString(), 0); err != nil {
		t.Fatal(err)
	}
	token = uuid.NewString()
	if _, err := r.ClaimFetch(t.Context(), fetch, token, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "UPDATE endpoint_fetches SET lease_until=NOW()-INTERVAL '1 second' WHERE fetch_id=$1::uuid", fetch); err != nil {
		t.Fatal(err)
	}
	if n, err := r.RecoverExpiredFetches(t.Context(), 100); err != nil || n != 1 {
		t.Fatalf("recovery count=%d err=%v", n, err)
	}
	token = uuid.NewString()
	if _, err := r.ClaimFetch(t.Context(), fetch, token, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CompleteFetchFailure(t.Context(), fetch, token, uuid.NewString(), nil, "temporary failure", 0, true, "error"); err != nil {
		t.Fatal(err)
	}
	if n, err := r.ScheduleDue(t.Context(), time.Minute, 100); err != nil || n != 1 {
		t.Fatalf("retry count=%d err=%v", n, err)
	}
}
