//go:build integration

package repository

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"monitoring_service/internal/domain"
)

func observationJob(t *testing.T, db *sql.DB, m domain.Monitor) (*MonitoringExecutionRepository, domain.FetchJob, string) {
	t.Helper()
	r := NewMonitoringExecutionRepository(db)
	if _, err := r.ScheduleDue(t.Context(), time.Minute, 100); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.QueryRowContext(t.Context(), `SELECT fetch_id::text FROM endpoint_fetches WHERE monitor_id=$1 ORDER BY generation DESC LIMIT 1`, m.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	token := uuid.NewString()
	job, err := r.ClaimFetch(t.Context(), id, token, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return r, job, token
}

func TestObservationFinalizationAtomicityAndReplay(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit_and_replay", true: "rollback_before_inbox"}[rollback], func(t *testing.T) {
			db := monitorSessionDB(t)
			a, _ := monitorSessionPair(t, db)
			r, job, token := observationJob(t, db, a)
			if rollback {
				_, err := db.ExecContext(t.Context(), `CREATE FUNCTION reject_observation_inbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated inbox failure'; END $$;
 CREATE TRIGGER reject_observation_inbox BEFORE INSERT ON processed_messages FOR EACH ROW EXECUTE FUNCTION reject_observation_inbox()`)
				if err != nil {
					t.Fatal(err)
				}
			}
			message := uuid.NewString()
			o := FetchObservation{Observed: true, Metadata: []byte(`{"results":[{"id":"price"}]}`), Terminal: true, Succeeded: true, Matched: a.Rules, NextCheck: time.Minute, SessionCiphertext: "rotated", SessionGeneration: job.SessionGeneration}
			err := r.FinalizeObservation(t.Context(), job.FetchID, token, message, o)
			if rollback && err == nil {
				t.Fatal("simulated commit failure ignored")
			}
			if !rollback && err != nil {
				t.Fatal(err)
			}
			if !rollback {
				if err := r.FinalizeObservation(t.Context(), job.FetchID, token, message, o); err != nil {
					t.Fatal(err)
				}
			}
			var monitorStatus, fetchStatus string
			var storedBody sql.NullString
			var notifications, evaluations, inbox int
			if err := db.QueryRowContext(t.Context(), `SELECT status FROM monitors WHERE id=$1`, a.ID).Scan(&monitorStatus); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(t.Context(), `SELECT status,response_json::text FROM endpoint_fetches WHERE fetch_id=$1::uuid`, job.FetchID).Scan(&fetchStatus, &storedBody); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM notifications WHERE monitor_id=$1),(SELECT count(*) FROM monitor_fetch_evaluations WHERE fetch_id=$2::uuid),(SELECT count(*) FROM processed_messages WHERE message_id=$3::uuid)`, a.ID, job.FetchID, message).Scan(&notifications, &evaluations, &inbox); err != nil {
				t.Fatal(err)
			}
			if storedBody.Valid {
				t.Fatal("full document persisted")
			}
			if rollback {
				if monitorStatus != "monitoring" || fetchStatus != "fetching" || notifications != 0 || evaluations != 0 || inbox != 0 {
					t.Fatal("partial finalization committed")
				}
			} else if monitorStatus != "success" || fetchStatus != "succeeded" || notifications != 1 || evaluations != 1 || inbox != 1 {
				t.Fatal("finalization or replay lost atomicity")
			}
		})
	}
}

func TestInfrastructureDeferPreservesTargetStateAndActiveRetention(t *testing.T) {
	db := monitorSessionDB(t)
	a, _ := monitorSessionPair(t, db)
	r, job, token := observationJob(t, db, a)
	if _, err := db.ExecContext(t.Context(), `UPDATE monitors SET attempts_count=3,consecutive_errors=2,last_error='previous target error',last_checked_at='2020-01-01',last_success_at='2020-01-01' WHERE id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	message := uuid.NewString()
	if err := r.DeferInfrastructure(t.Context(), job.FetchID, token, message, "fetcher_unavailable", time.Second); err != nil {
		t.Fatal(err)
	}
	var status, check, errorText, reason string
	var attempts, failures int
	var checked time.Time
	if err := db.QueryRowContext(t.Context(), `SELECT status,check_status,attempts_count,consecutive_errors,last_error,last_checked_at FROM monitors WHERE id=$1`, a.ID).Scan(&status, &check, &attempts, &failures, &errorText, &checked); err != nil {
		t.Fatal(err)
	}
	if status != "monitoring" || check != "scheduled" || attempts != 3 || failures != 2 || errorText != "previous target error" || checked.Year() != 2020 {
		t.Fatal("infrastructure polluted target state")
	}
	if err := db.QueryRowContext(t.Context(), `SELECT defer_reason FROM endpoint_fetches WHERE fetch_id=$1::uuid`, job.FetchID).Scan(&reason); err != nil || reason != "fetcher_unavailable" {
		t.Fatal(reason, err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE endpoint_fetches SET created_at=NOW()-INTERVAL '8 days'`); err != nil {
		t.Fatal(err)
	}
	result, err := NewMonitoringMaintenanceRepository(db).DeleteExpired(t.Context(), 100)
	if err != nil || result.EndpointFetches != 0 {
		t.Fatal(result, err)
	}
}
