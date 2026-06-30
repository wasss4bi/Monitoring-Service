//go:build integration

package integration

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	v1 "github.com/wasss4bi/values-from-body-fetcher/api/fetcher/v1"
	"monitoring_service/internal/authorization"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/service"
)

func TestManagedPreparationProgressesWithOneHostnamePermit(t *testing.T) {
	for _, summary := range []string{"no_network", "network", "network_with_budget", "unknown"} {
		t.Run(summary, func(t *testing.T) {
			db := monitoringTestDatabase(t)
			budget := 10 * time.Second
			if summary == "network_with_budget" {
				budget = 200 * time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			monitor := createPriceMonitor(t, db, "https://quota.example.com/value", true)
			if _, err := db.ExecContext(ctx, `UPDATE monitored_endpoints SET browser_page_url='https://quota.example.com/card' WHERE id=$1`, monitor.EndpointID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `UPDATE target_hostnames SET limit_per_minute=1`); err != nil {
				t.Fatal(err)
			}
			protector, err := authorization.NewForPurpose(base64.StdEncoding.EncodeToString(make([]byte, 32)), "target-session:v1")
			if err != nil {
				t.Fatal(err)
			}
			fetches, preparations := 0, 0
			client := fixtureFetcher{
				fetch: func(_ context.Context, r *v1.FetchRequest) (*v1.FetchResponse, error) {
					fetches++
					out := fixtureFailure(r, v1.FetchErrorCode_FETCH_ERROR_CODE_SESSION_REQUIRED, 0)
					switch summary {
					case "network", "network_with_budget":
						out.Failure.Code = v1.FetchErrorCode_FETCH_ERROR_CODE_SESSION_REJECTED
						out.Fetch.NetworkPerformed = true
					case "unknown":
						out.Fetch = nil
					}
					return out, nil
				},
				prepare: func(_ context.Context, r *v1.PrepareSessionRequest) (*v1.PrepareSessionResponse, error) {
					preparations++
					return &v1.PrepareSessionResponse{RequestId: r.RequestId, SessionState: fixtureSession}, nil
				},
			}
			execution := service.NewMonitoringExecutionService(repository.NewMonitoringExecutionRepository(db), nil, time.Minute, time.Minute, nil, nil).
				WithFetcher(client).WithTargetSessionProtector(protector)
			if n, err := execution.Schedule(ctx); err != nil || n != 1 {
				t.Fatalf("schedule=%d err=%v", n, err)
			}
			var fetchID, messageID string
			if err := db.QueryRowContext(ctx, `SELECT payload->>'fetch_id',event_id::text FROM outbox_events WHERE aggregate_type='monitor' AND aggregate_id=$1 ORDER BY id DESC LIMIT 1`, monitor.ID).Scan(&fetchID, &messageID); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := execution.ProcessFetch(ctx, fetchID, messageID); err != nil {
					t.Fatal(err)
				}
			}
			wantPreparations, wantSession := 0, "missing"
			if summary == "no_network" || summary == "network_with_budget" {
				wantPreparations, wantSession = 1, "ready"
			}
			if fetches != 1 || preparations != wantPreparations {
				t.Fatalf("fetches=%d preparations=%d; want 1,%d", fetches, preparations, wantPreparations)
			}
			var session, check, reason string
			var attempts int
			var released bool
			if err := db.QueryRowContext(ctx, `SELECT m.session_status,m.check_status,m.attempts_count,m.refresh_lease_token IS NULL,f.defer_reason FROM monitors m JOIN endpoint_fetches f ON f.monitor_id=m.id WHERE f.fetch_id=$1::uuid`, fetchID).Scan(&session, &check, &attempts, &released, &reason); err != nil {
				t.Fatal(err)
			}
			if session != wantSession || check != "scheduled" || reason != "hostname_limit" || attempts != 0 || !released {
				t.Fatalf("session=%s check=%s defer=%s attempts=%d released=%t", session, check, reason, attempts, released)
			}
		})
	}
}
