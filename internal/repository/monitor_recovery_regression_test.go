//go:build integration

package repository

import (
	"testing"
	"time"
)

func TestStoppedPendingFetchIsTerminal(t *testing.T) {
	db := monitorSessionDB(t)
	m, _ := monitorSessionPair(t, db)
	execution := NewMonitoringExecutionRepository(db)
	if _, err := execution.ScheduleDue(t.Context(), time.Minute, 100); err != nil {
		t.Fatal(err)
	}
	if err := NewMonitorRepository(db).Stop(t.Context(), m.UserID, m.ID); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRowContext(t.Context(), `SELECT status FROM endpoint_fetches WHERE monitor_id=$1`, m.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("stopped pending fetch status=%s; want failed", status)
	}
}

func TestDeletedOrReplacedPendingFetchIsTerminal(t *testing.T) {
	for _, operation := range []string{"delete", "replace"} {
		t.Run(operation, func(t *testing.T) {
			db := monitorSessionDB(t)
			m, _ := monitorSessionPair(t, db)
			execution := NewMonitoringExecutionRepository(db)
			if _, err := execution.ScheduleDue(t.Context(), time.Minute, 100); err != nil {
				t.Fatal(err)
			}
			repo := NewMonitorRepository(db)
			if operation == "delete" {
				if err := repo.SoftDelete(t.Context(), m.UserID, m.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := repo.ReplaceSession(t.Context(), m.UserID, m.ID, "managed", ""); err != nil {
					t.Fatal(err)
				}
			}
			var status string
			if err := db.QueryRowContext(t.Context(), `SELECT status FROM endpoint_fetches WHERE monitor_id=$1`, m.ID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != "failed" {
				t.Fatalf("status=%s", status)
			}
		})
	}
}

func TestPendingRecoveryAvoidsUnpublishedDuplicates(t *testing.T) {
	db := monitorSessionDB(t)
	m, _ := monitorSessionPair(t, db)
	execution := NewMonitoringExecutionRepository(db)
	if _, err := execution.ScheduleDue(t.Context(), time.Minute, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE endpoint_fetches SET next_dispatch_at=NOW()-INTERVAL '1 second' WHERE monitor_id=$1`, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := execution.RecoverPendingFetches(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM outbox_events WHERE aggregate_type='monitor' AND aggregate_id=$1`, m.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("unpublished events=%d; want 1", count)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE outbox_events SET published_at=NOW() WHERE aggregate_type='monitor' AND aggregate_id=$1`, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE endpoint_fetches SET next_dispatch_at=NOW()-INTERVAL '1 second' WHERE monitor_id=$1`, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := execution.RecoverPendingFetches(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM outbox_events WHERE aggregate_type='monitor' AND aggregate_id=$1 AND published_at IS NULL`, m.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recovery events after publication=%d; want 1", count)
	}
}
