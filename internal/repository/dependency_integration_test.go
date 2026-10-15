//go:build integration

package repository

import (
	"testing"

	"github.com/google/uuid"
)

func TestDependencyEpisodesFenceOwnersAndDeliverAlerts(t *testing.T) {
	db := monitorSessionDB(t)
	r := NewDependencyRepository(db)
	owner, other := uuid.NewString(), uuid.NewString()
	if acquired, err := r.AcquireProbe(t.Context(), "direct", owner); err != nil || !acquired {
		t.Fatalf("acquire probe: acquired=%v err=%v", acquired, err)
	}
	if acquired, err := r.AcquireProbe(t.Context(), "direct", other); err != nil || acquired {
		t.Fatalf("second owner acquired live lease: acquired=%v err=%v", acquired, err)
	}
	recipients := []string{"fixture-recipient", "fixture-recipient"}
	probe := func(token string, healthy bool) {
		t.Helper()
		if err := r.RecordProbe(t.Context(), "direct", token, healthy, recipients); err != nil {
			t.Fatal(err)
		}
	}
	assertState := func(want string, alerts int) {
		t.Helper()
		var state string
		var count int
		if err := db.QueryRowContext(t.Context(), `SELECT status,(SELECT count(*) FROM operational_notifications) FROM service_dependencies WHERE name='direct'`).Scan(&state, &count); err != nil {
			t.Fatal(err)
		}
		if state != want || count != alerts {
			t.Fatalf("status=%s alerts=%d; want %s/%d", state, count, want, alerts)
		}
	}
	probe(owner, true)
	probe(owner, true)
	assertState("up", 0) // Initial readiness is not recovery from an outage.
	probe(owner, false)
	probe(owner, false)
	assertState("up", 0)
	probe(owner, false)
	probe(owner, false)
	assertState("down", 1)
	if _, err := db.ExecContext(t.Context(), `UPDATE service_dependencies SET lease_until=NOW()-INTERVAL '1 second' WHERE name='direct'`); err != nil {
		t.Fatal(err)
	}
	if acquired, err := r.AcquireProbe(t.Context(), "direct", other); err != nil || !acquired {
		t.Fatalf("take over expired probe: acquired=%v err=%v", acquired, err)
	}
	probe(owner, true)
	probe(owner, true)
	assertState("down", 1)
	probe(other, true)
	assertState("down", 1)
	probe(other, true)
	assertState("up", 2)
	var id int64
	if err := db.QueryRowContext(t.Context(), `SELECT id FROM operational_notifications WHERE transition='down'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	job, err := r.ClaimAlert(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	stale := job
	stale.Token = uuid.NewString()
	message := uuid.NewString()
	if err := r.FinishAlert(t.Context(), stale, message, true); err != nil {
		t.Fatal(err)
	}
	if processed, err := r.IsAlertProcessed(t.Context(), message); err != nil || processed {
		t.Fatalf("stale completion entered inbox: processed=%v err=%v", processed, err)
	}
	if err := r.FinishAlert(t.Context(), job, message, true); err != nil {
		t.Fatal(err)
	}
	if err := r.FinishAlert(t.Context(), job, message, true); err != nil {
		t.Fatal(err)
	}
	if processed, err := r.IsAlertProcessed(t.Context(), message); err != nil || !processed {
		t.Fatalf("completed alert missing from inbox: processed=%v err=%v", processed, err)
	}
	var state string
	var events int
	if err := db.QueryRowContext(t.Context(), `SELECT status,(SELECT count(*) FROM outbox_events WHERE event_type='operational.notification.deliver') FROM operational_notifications WHERE id=$1`, id).Scan(&state, &events); err != nil {
		t.Fatal(err)
	}
	if state != "sent" || events != 2 {
		t.Fatalf("alert status=%s outbox events=%d", state, events)
	}
}
