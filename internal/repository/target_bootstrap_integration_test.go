//go:build integration

package repository

import (
	"encoding/json"
	"github.com/google/uuid"
	"monitoring_service/internal/domain"
	"testing"
	"time"
)

func TestBootstrapPolicyChangeFencesJobsAndPreservesStoppedProvidedState(t *testing.T) {
	db := monitorSessionDB(t)
	active, stopped := monitorSessionPair(t, db)
	execution, job, token := observationJob(t, db, active)
	if _, err := db.ExecContext(t.Context(), `UPDATE monitors SET status='stopped',check_status='idle',session_mode='provided',session_ciphertext='fixture-owned-envelope' WHERE id=$1`, stopped.ID); err != nil {
		t.Fatal(err)
	}
	var targetID int64
	if err := db.QueryRowContext(t.Context(), `SELECT target_hostname_id FROM monitored_endpoints WHERE id=$1`, active.EndpointID).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	rules := json.RawMessage(`[{"origin":"https://sessions.example.com","path_prefix":"/verify/","methods":["POST"]}]`)
	target, err := NewTargetHostnameRepository(db).Update(t.Context(), targetID, domain.UpdateTargetHostnameInput{BootstrapRules: rules})
	if err != nil || len(target.BootstrapRules) == 0 {
		t.Fatal("bootstrap policy update failed", err)
	}
	if err := execution.FinalizeObservation(t.Context(), job.FetchID, token, uuid.NewString(), FetchObservation{Observed: true, Terminal: true, Succeeded: true, Matched: active.Rules, NextCheck: time.Minute}); err != nil {
		t.Fatal(err)
	}
	var status, check string
	var generation int64
	var due bool
	if err := db.QueryRowContext(t.Context(), `SELECT status,check_status,generation,next_check_at<=NOW() FROM monitors WHERE id=$1`, active.ID).Scan(&status, &check, &generation, &due); err != nil {
		t.Fatal(err)
	}
	if status != "monitoring" || check != "idle" || generation <= job.Generation || !due {
		t.Fatal("old result was accepted or active check was not rescheduled")
	}
	var session string
	var scheduled bool
	if err := db.QueryRowContext(t.Context(), `SELECT status,session_ciphertext,next_check_at IS NOT NULL FROM monitors WHERE id=$1`, stopped.ID).Scan(&status, &session, &scheduled); err != nil {
		t.Fatal(err)
	}
	if status != "stopped" || session != "fixture-owned-envelope" || scheduled {
		t.Fatal("stopped/provided monitor was changed beyond fencing")
	}
	var notifications int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM notifications WHERE monitor_id=$1`, active.ID).Scan(&notifications); err != nil || notifications != 0 {
		t.Fatal("stale observation triggered a notification", err)
	}
	if count, err := execution.ScheduleDue(t.Context(), time.Minute, 100); err != nil || count != 1 {
		t.Fatalf("rescheduled jobs=%d err=%v", count, err)
	}
}
