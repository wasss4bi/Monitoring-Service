//go:build integration

package repository

import (
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestExpiredNotificationWorkerCannotOverwriteSuccess(t *testing.T) {
	db := monitorSessionDB(t)
	m, _ := monitorSessionPair(t, db)
	var id int64
	if err := db.QueryRowContext(t.Context(), `INSERT INTO notifications(monitor_id,user_contact_id,event_type,message) SELECT $1,id,'monitor.succeeded','test' FROM user_contacts WHERE user_id=$2 RETURNING id`, m.ID, m.UserID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	r := NewMonitoringExecutionRepository(db)
	old, err := r.ClaimNotification(t.Context(), id, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE notifications SET next_attempt_at=NOW()-INTERVAL '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	newJob, err := r.ClaimNotification(t.Context(), id, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CompleteNotification(t.Context(), id, newJob.LeaseToken, uuid.NewString(), "", 0, false); err != nil {
		t.Fatal(err)
	}
	if err := r.CompleteNotification(t.Context(), id, old.LeaseToken, uuid.NewString(), "late error", time.Second, true); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRowContext(t.Context(), `SELECT status FROM notifications WHERE id=$1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "sent" {
		t.Fatalf("stale worker overwrote success: status=%s", status)
	}
	if err := r.DisableContactForPermanentNotificationFailure(t.Context(), id, old.LeaseToken, old.Contact, old.ContactUpdatedAt, uuid.NewString(), "late permanent error"); err != nil {
		t.Fatal(err)
	}
	var active bool
	if err := db.QueryRowContext(t.Context(), `SELECT is_active FROM user_contacts WHERE user_id=$1`, m.UserID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("stale worker disabled contact")
	}
}

func TestPermanentFailureDoesNotDisableChangedContact(t *testing.T) {
	db := monitorSessionDB(t)
	m, _ := monitorSessionPair(t, db)
	var id int64
	if err := db.QueryRowContext(t.Context(), `INSERT INTO notifications(monitor_id,user_contact_id,event_type,message) SELECT $1,id,'monitor.succeeded','test' FROM user_contacts WHERE user_id=$2 RETURNING id`, m.ID, m.UserID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	r := NewMonitoringExecutionRepository(db)
	job, err := r.ClaimNotification(t.Context(), id, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), `UPDATE user_contacts SET contact='new@example.invalid',is_verified=true,is_active=true WHERE user_id=$1`, m.UserID); err != nil {
		t.Fatal(err)
	}
	if err = r.DisableContactForPermanentNotificationFailure(t.Context(), id, job.LeaseToken, job.Contact, job.ContactUpdatedAt, uuid.NewString(), "old address failed"); err != nil {
		t.Fatal(err)
	}
	var active bool
	if err = db.QueryRowContext(t.Context(), `SELECT is_active FROM user_contacts WHERE user_id=$1`, m.UserID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("new contact disabled")
	}
}
