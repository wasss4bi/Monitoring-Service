//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"monitoring_service/internal/authjwt"
	"monitoring_service/internal/httpapi"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/service"

	"github.com/google/uuid"
)

func TestMonitorConflictsExplainTheirCauseAndReplaySafely(t *testing.T) {
	for _, tc := range []struct {
		name, update, message string
	}{
		{"browser page", `UPDATE monitored_endpoints SET browser_page_url='https://errors.example.com/card' WHERE id=$1`, "this URL is already configured with a different browser_page_url"},
		{"type", `UPDATE monitored_endpoints SET type='html' WHERE id=$1`, "this URL is already configured with a different type"},
		{"JSON source", `UPDATE monitored_endpoints SET json_source='{"script_contains":"__STATE__","object_key":"product","match":{"path":"sku","value":"one"}}'::jsonb WHERE id=$1`, "this URL is already configured with a different json_source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := monitoringTestDatabase(t)
			seed := createPriceMonitor(t, db, "https://errors.example.com/api/item", true)
			if _, err := db.ExecContext(t.Context(), tc.update, seed.EndpointID); err != nil {
				t.Fatal(err)
			}
			secret := uuid.NewString()
			token, err := authjwt.Generate(secret, authjwt.Claims{AuthVersion: 1, UserID: seed.UserID, ExpiresAt: time.Now().Add(time.Hour).Unix()})
			if err != nil {
				t.Fatal(err)
			}
			router := monitoringRouter(t, httpapi.RouterConfig{
				DB: db, Logger: logging.New(io.Discard), JWTSecret: secret,
				MonitorService:     service.NewMonitorService(repository.NewMonitorRepository(db)).WithTargetHostnameRepository(repository.NewTargetHostnameRepository(db)),
				IdempotencyService: service.NewIdempotencyService(repository.NewIdempotencyRepository(db), time.Hour),
			})
			key := uuid.NewString()
			body := `{"name":"Conflict test","url":"https://errors.example.com/api/item","page_url":"https://errors.example.com/card","type":"json","path_type":"json","session_mode":"managed","rules":[{"path":"price","operator":"<=","value":30000,"description":"Price <= 30000"}]}`
			// Stored JSON may normalize formatting; assert the decoded response on each delivery.
			for range 2 {
				request := httptest.NewRequest(http.MethodPost, "/me/monitors", strings.NewReader(body))
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("Idempotency-Key", key)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				var result struct {
					Error struct{ Code, Message string } `json:"error"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if response.Code != http.StatusConflict || result.Error.Code != "Conflict" || result.Error.Message != tc.message {
					t.Fatalf("incorrect conflict: %d %s", response.Code, response.Body.String())
				}
			}
			var count int
			if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM monitors").Scan(&count); err != nil || count != 1 {
				t.Fatalf("conflicting request changed monitors: count=%d err=%v", count, err)
			}
			request := httptest.NewRequest(http.MethodGet, "/me/monitors/9999999", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "monitor was not found") {
				t.Fatalf("incorrect missing monitor reason: %d %s", response.Code, response.Body.String())
			}
		})
	}
}
