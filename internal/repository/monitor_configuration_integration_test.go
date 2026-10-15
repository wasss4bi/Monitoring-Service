//go:build integration

package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"monitoring_service/internal/domain"
)

func TestRuleConfigurationSupersedesInflightFetch(t *testing.T) {
	for _, operation := range []string{"add_rule", "threshold"} {
		for _, stopped := range []bool{false, true} {
			t.Run(operation+"/stopped="+map[bool]string{false: "false", true: "true"}[stopped], func(t *testing.T) {
				db := monitorSessionDB(t)
				a, b := monitorSessionPair(t, db)
				execution := NewMonitoringExecutionRepository(db)
				monitors := NewMonitorRepository(db)
				if _, err := execution.ScheduleDue(t.Context(), time.Minute, 100); err != nil {
					t.Fatal(err)
				}
				var fetchID string
				if err := db.QueryRowContext(t.Context(), `SELECT fetch_id::text FROM endpoint_fetches WHERE monitor_id=$1`, a.ID).Scan(&fetchID); err != nil {
					t.Fatal(err)
				}
				token := uuid.NewString()
				job, err := execution.ClaimFetch(t.Context(), fetchID, token, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				if stopped {
					if err := monitors.Stop(t.Context(), a.UserID, a.ID); err != nil {
						t.Fatal(err)
					}
				}
				var before int64
				if err := db.QueryRowContext(t.Context(), `SELECT generation FROM monitors WHERE id=$1`, a.ID).Scan(&before); err != nil {
					t.Fatal(err)
				}
				if _, err := db.ExecContext(t.Context(), `UPDATE monitors SET attempts_count=3,consecutive_errors=3,retry_started_at=NOW() WHERE id=$1`, a.ID); err != nil {
					t.Fatal(err)
				}
				if operation == "add_rule" {
					_, err = monitors.AddRule(t.Context(), a.UserID, a.ID, domain.CreateMonitorRuleInput{Description: "New price rule", JSONPath: "new_price", ComparisonOperator: "<", ExpectedValue: 10})
				} else {
					_, err = monitors.UpdateRulesCountToFinish(t.Context(), a.UserID, a.ID, 1)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := execution.FinishMonitor(t.Context(), fetchID, token, a.ID, true, "stale", a.Rules); err != nil {
					t.Fatal(err)
				}
				var generation int64
				var status, checkStatus string
				var attempts, errors, notifications int
				if err := db.QueryRowContext(t.Context(), `SELECT generation,status,check_status,attempts_count,consecutive_errors FROM monitors WHERE id=$1`, a.ID).Scan(&generation, &status, &checkStatus, &attempts, &errors); err != nil {
					t.Fatal(err)
				}
				wantStatus := domain.MonitorStatusMonitoring
				if stopped {
					wantStatus = domain.MonitorStatusStopped
				}
				if generation != before+1 || status != wantStatus || checkStatus != "idle" || attempts != 0 || errors != 0 {
					t.Fatalf("unexpected state: %d %s %s %d %d", generation, status, checkStatus, attempts, errors)
				}
				if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM notifications WHERE monitor_id=$1`, a.ID).Scan(&notifications); err != nil {
					t.Fatal(err)
				}
				if notifications != 0 {
					t.Fatal("stale result sent notification")
				}
				var otherGeneration int64
				if err := db.QueryRowContext(t.Context(), `SELECT generation FROM monitors WHERE id=$1`, b.ID).Scan(&otherGeneration); err != nil {
					t.Fatal(err)
				}
				if otherGeneration != job.Generation {
					t.Fatal("another subscriber was invalidated")
				}
				n, err := execution.ScheduleDue(t.Context(), time.Minute, 100)
				want := 1
				if stopped {
					want = 0
				}
				if err != nil || n != want {
					t.Fatalf("reschedule: %d %v", n, err)
				}
			})
		}
	}
}
