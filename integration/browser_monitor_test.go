//go:build integration

package integration

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"context"
	v1 "github.com/wasss4bi/values-from-body-fetcher/api/fetcher/v1"
	"monitoring_service/internal/authjwt"
	"monitoring_service/internal/authorization"
	"monitoring_service/internal/domain"
	"monitoring_service/internal/httpapi"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/google/uuid"
)

func TestResumeNoContentIdempotency(t *testing.T) {
	db := monitoringTestDatabase(t)
	seed := createPriceMonitor(t, db, "https://resume.example.com/value", true)
	if _, err := db.ExecContext(t.Context(), `UPDATE monitors SET status='error' WHERE id=$1`, seed.ID); err != nil {
		t.Fatal(err)
	}
	secret := uuid.NewString()
	token, err := authjwt.Generate(secret, authjwt.Claims{AuthVersion: 1, UserID: seed.UserID, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	router := monitoringRouter(t, httpapi.RouterConfig{
		DB: db, Logger: logging.New(io.Discard), JWTSecret: secret,
		MonitorService:     service.NewMonitorService(repository.NewMonitorRepository(db)),
		IdempotencyService: service.NewIdempotencyService(repository.NewIdempotencyRepository(db), time.Hour),
	})
	key := uuid.NewString()
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/me/monitors/%d/resume", seed.ID), nil)
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Idempotency-Key", key)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
			t.Fatalf("resume HTTP %d body=%q", response.Code, response.Body.String())
		}
	}
	entry, err := repository.NewIdempotencyRepository(db).Get(t.Context(), key)
	if err != nil || entry.Status != domain.IdempotencyStatusCompleted || entry.ResponseCode != http.StatusNoContent || len(entry.ResponseBody) != 0 {
		t.Fatalf("stored no-content response=%+v err=%v", entry, err)
	}
}

func TestNativeBrowserMonitorAPIAndSharedIdentity(t *testing.T) {
	db := monitoringTestDatabase(t)
	seed := createPriceMonitor(t, db, "https://native.example.com/seed", true)
	secret := uuid.NewString()
	token, err := authjwt.Generate(secret, authjwt.Claims{AuthVersion: 1, UserID: seed.UserID, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	targets := repository.NewTargetHostnameRepository(db)
	router := monitoringRouter(t, httpapi.RouterConfig{
		DB: db, Logger: logging.New(io.Discard), JWTSecret: secret,
		MonitorService:     service.NewMonitorService(repository.NewMonitorRepository(db)).WithTargetHostnameRepository(targets),
		IdempotencyService: service.NewIdempotencyService(repository.NewIdempotencyRepository(db), time.Hour),
	})
	endpointURL := "https://native.example.com/api/value?item=1"
	pageURL := "https://native.example.com/product/1"
	body := map[string]any{"name": "Native", "url": endpointURL, "page_url": pageURL, "browser_page_url": pageURL, "type": "json", "path_type": "json",
		"rules": []map[string]any{{"path": "price", "description": "Price <= 400", "operator": "<=", "value": 400}}}
	post := func() *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/me/monitors", strings.NewReader(string(raw)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Idempotency-Key", uuid.NewString())
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	first := post()
	if first.Code != http.StatusCreated {
		t.Fatalf("create HTTP %d: %s", first.Code, first.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["page_url"] != pageURL || created["browser_page_url"] != pageURL {
		t.Fatalf("missing page_url in %s", first.Body.String())
	}
	for _, location := range []string{fmt.Sprintf("/me/monitors/%.0f", created["id"]), "/me/monitors"} {
		request := httptest.NewRequest(http.MethodGet, location, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), pageURL) {
			t.Fatalf("roundtrip HTTP %d: %s", response.Code, response.Body.String())
		}
	}
	if got := post().Code; got != http.StatusCreated {
		t.Fatalf("same source HTTP %d", got)
	}
	body["page_url"] = "https://native.example.com/another-product-link"
	if got := post().Code; got != http.StatusCreated {
		t.Fatalf("monitor-owned product link HTTP %d", got)
	}
	delete(body, "page_url")
	if got := post().Code; got != http.StatusBadRequest {
		t.Fatalf("missing product link HTTP %d", got)
	}
	body["page_url"] = pageURL
	configuredRule := body["rules"].([]map[string]any)[0]
	delete(configuredRule, "description")
	if got := post().Code; got != http.StatusBadRequest {
		t.Fatalf("missing rule description HTTP %d", got)
	}
	configuredRule["description"] = "Price <= 400"
	body["browser_page_url"] = "https://native.example.com/product/2"
	if got := post().Code; got != http.StatusConflict {
		t.Fatalf("conflicting page HTTP %d", got)
	}
	delete(body, "browser_page_url")
	if got := post().Code; got != http.StatusConflict {
		t.Fatalf("native versus HTTP HTTP %d", got)
	}
	body["browser_page_url"] = "https://other.example.com/product/1"
	if got := post().Code; got != http.StatusBadRequest {
		t.Fatalf("cross hostname HTTP %d", got)
	}
	body["browser_page_url"] = pageURL
	body["headers"] = map[string]string{"Cookie": "private"}
	if got := post().Code; got != http.StatusBadRequest {
		t.Fatalf("custom headers HTTP %d", got)
	}
	delete(body, "headers")
	delete(body, "browser_page_url")
	body["type"], body["path_type"], body["url"] = "html", "selector", endpointURL+"/html"
	configuredRule["path"] = ".price"
	if got := post().Code; got != http.StatusCreated {
		t.Fatalf("HTML product link HTTP %d", got)
	}
}

func TestNativeBrowserFetchLifecycle(t *testing.T) {
	for _, outcome := range []string{"missing", "reuse", "refresh", "missing_profile", "bootstrap_denied", "endpoint_denied", "bootstrap_busy", "evaluate_busy", "html_source"} {
		t.Run(outcome, func(t *testing.T) {
			db := monitoringTestDatabase(t)
			ctx := t.Context()
			endpointURL := "https://native.example.com/api/value?item=1"
			pageURL := "https://native.example.com/product/1"
			monitor := createPriceMonitor(t, db, endpointURL, true)
			if _, err := db.ExecContext(ctx, "UPDATE monitored_endpoints SET browser_page_url=$1 WHERE id=$2", pageURL, monitor.EndpointID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, "UPDATE monitors SET page_url=$1 WHERE id=$2", pageURL, monitor.ID); err != nil {
				t.Fatal(err)
			}
			var jsonSource *domain.JSONSource
			if outcome == "html_source" {
				jsonSource = &domain.JSONSource{ScriptContains: "__STATE__", ObjectKey: "item", Match: domain.JSONSourceMatch{Path: "sku", Value: "one"}}
				encoded, err := json.Marshal(jsonSource)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.ExecContext(ctx, "UPDATE monitored_endpoints SET browser_page_url=NULL,json_source=$1::jsonb WHERE id=$2", string(encoded), monitor.EndpointID); err != nil {
					t.Fatal(err)
				}
			}
			protector, err := authorization.NewForPurpose(base64.StdEncoding.EncodeToString(make([]byte, 32)), "target-session:v1")
			if err != nil {
				t.Fatal(err)
			}
			state := fixtureSession
			missing := outcome == "missing" || outcome == "bootstrap_denied" || outcome == "bootstrap_busy" || outcome == "html_source"
			if !missing {
				cipher, err := protector.Encrypt(string(state))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.ExecContext(ctx, "UPDATE monitors SET session_status='ready',session_ciphertext=$1 WHERE id=$2", cipher, monitor.ID); err != nil {
					t.Fatal(err)
				}
			}
			var bootstraps, evaluations atomic.Int32
			client := fixtureFetcher{
				fetch: func(_ context.Context, input *v1.FetchRequest) (*v1.FetchResponse, error) {
					call := evaluations.Add(1)
					if input.SourcePageUrl != pageURL || input.Url != endpointURL {
						t.Error("missing browser source page")
					}
					if outcome == "html_source" && (input.InlineJsonSource == nil || input.InlineJsonSource.ObjectKey != jsonSource.ObjectKey) {
						t.Error("missing inline source descriptor")
					}
					if outcome == "evaluate_busy" {
						return nil, status.Error(codes.ResourceExhausted, "fixture capacity")
					}
					if outcome == "endpoint_denied" || outcome == "refresh" && call == 1 {
						return fixtureFailure(input, v1.FetchErrorCode_FETCH_ERROR_CODE_SESSION_REJECTED, 403), nil
					}
					if outcome == "missing_profile" && call == 1 {
						return fixtureFailure(input, v1.FetchErrorCode_FETCH_ERROR_CODE_PROFILE_UNAVAILABLE, 0), nil
					}
					if len(input.Session.State) == 0 {
						return fixtureFailure(input, v1.FetchErrorCode_FETCH_ERROR_CODE_SESSION_REQUIRED, 0), nil
					}
					values := map[string]string{}
					for _, selector := range input.Selectors {
						values[selector.Expression] = "450"
					}
					result := fixtureValues(input, values)
					result.UpdatedSessionState = state
					return result, nil
				},
				prepare: func(_ context.Context, input *v1.PrepareSessionRequest) (*v1.PrepareSessionResponse, error) {
					bootstraps.Add(1)
					expected := v1.SessionPreparationMode_SESSION_PREPARATION_MODE_BROWSER_CAPTURE
					if outcome == "html_source" {
						expected = v1.SessionPreparationMode_SESSION_PREPARATION_MODE_INLINE_JSON
					}
					if input.Mode != expected || input.SourcePageUrl != pageURL || input.EndpointUrl != endpointURL {
						t.Error("unexpected bootstrap mode/source")
					}
					if outcome == "html_source" && (input.InlineJsonSource == nil || input.InlineJsonSource.ObjectKey != jsonSource.ObjectKey) {
						t.Error("missing inline source in preparation")
					}
					if outcome == "bootstrap_denied" {
						code := uint32(498)
						return &v1.PrepareSessionResponse{RequestId: input.RequestId, Failure: &v1.FetchError{Code: v1.FetchErrorCode_FETCH_ERROR_CODE_SESSION_REJECTED, HttpStatus: &code}}, nil
					}
					if outcome == "bootstrap_busy" {
						return nil, status.Error(codes.ResourceExhausted, "fixture capacity")
					}
					return &v1.PrepareSessionResponse{RequestId: input.RequestId, SessionState: state}, nil
				},
			}
			execution := service.NewMonitoringExecutionService(repository.NewMonitoringExecutionRepository(db), nil, time.Minute, time.Minute, nil, nil).WithFetcher(client).WithTargetSessionProtector(protector)
			if count, err := execution.Schedule(ctx); err != nil || count != 1 {
				t.Fatalf("schedule %d: %v", count, err)
			}
			var fetchID, messageID string
			if err := db.QueryRowContext(ctx, "SELECT payload->>'fetch_id',event_id::text FROM outbox_events WHERE aggregate_type='monitor' AND aggregate_id=$1 ORDER BY id DESC LIMIT 1", monitor.ID).Scan(&fetchID, &messageID); err != nil {
				t.Fatal(err)
			}
			for delivery := 0; delivery < 2; delivery++ {
				if err := execution.ProcessFetch(ctx, fetchID, messageID); err != nil {
					t.Fatal(err)
				}
			}
			wantBootstrap, wantEvaluate := int32(0), int32(1)
			wantSession, wantCheck, wantMonitor := "ready", "idle", "monitoring"
			switch outcome {
			case "missing", "html_source":
				wantBootstrap, wantEvaluate = 1, 2
			case "refresh", "missing_profile":
				wantBootstrap, wantEvaluate = 1, 2
			case "endpoint_denied":
				wantBootstrap, wantEvaluate, wantCheck, wantMonitor = 1, 2, "access_denied", "error"
			case "bootstrap_denied":
				wantBootstrap, wantEvaluate, wantSession, wantCheck, wantMonitor = 1, 1, "blocked", "access_denied", "error"
			case "bootstrap_busy":
				wantBootstrap, wantEvaluate, wantSession, wantCheck = 1, 1, "missing", "scheduled"
			case "evaluate_busy":
				wantCheck = "scheduled"
			}
			if bootstraps.Load() != wantBootstrap || evaluations.Load() != wantEvaluate {
				t.Fatalf("calls bootstrap=%d evaluate=%d; want %d,%d", bootstraps.Load(), evaluations.Load(), wantBootstrap, wantEvaluate)
			}
			var targetStatus, endpointStatus, monitorStatus, ciphertext string
			var leaseReleased bool
			if err := db.QueryRowContext(ctx, "SELECT m.session_status,m.check_status,m.status,COALESCE(m.session_ciphertext,''),m.refresh_lease_token IS NULL FROM monitors m JOIN monitored_endpoints e ON e.id=m.endpoint_id JOIN target_hostnames t ON t.id=e.target_hostname_id WHERE m.id=$1", monitor.ID).
				Scan(&targetStatus, &endpointStatus, &monitorStatus, &ciphertext, &leaseReleased); err != nil {
				t.Fatal(err)
			}
			if targetStatus != wantSession || endpointStatus != wantCheck || monitorStatus != wantMonitor || !leaseReleased {
				t.Fatalf("states target=%s endpoint=%s monitor=%s released=%t", targetStatus, endpointStatus, monitorStatus, leaseReleased)
			}
			if wantSession == "ready" {
				plain, err := protector.Decrypt(ciphertext)
				if err != nil || plain != string(state) || ciphertext == plain {
					t.Fatal("session metadata not preserved encrypted")
				}
			}
		})
	}
}
