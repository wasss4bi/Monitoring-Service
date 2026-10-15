//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"monitoring_service/internal/authjwt"
	"monitoring_service/internal/cache"
	"monitoring_service/internal/database"
	"monitoring_service/internal/domain"
	"monitoring_service/internal/httpapi"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/migrator"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/service"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

func monitoringTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	raw := os.Getenv("INTEGRATION_DATABASE_URL")
	if raw == "" {
		t.Skip("INTEGRATION_DATABASE_URL is required")
	}
	adminURL, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	adminURL.Path = "/postgres"
	admin, err := database.Open(t.Context(), adminURL.String())
	if err != nil {
		t.Fatal(err)
	}
	name := "monitoring_service_monitor_test_" + uuid.NewString()[:8]
	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+pq.QuoteIdentifier(name)); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), "DROP DATABASE "+pq.QuoteIdentifier(name)+" WITH (FORCE)"); err != nil {
			t.Errorf("drop temporary test database %s: %v", name, err)
		}
		admin.Close()
	})
	adminURL.Path = "/" + name
	query := adminURL.Query()
	query.Set("statement_timeout", "10000")
	query.Set("lock_timeout", "3000")
	adminURL.RawQuery = query.Encode()
	t.Setenv("ADMIN_EMAIL", "fixture-admin@example.com")
	t.Setenv("ADMIN_PASSWORD", "Fixture-admin-Password7!")
	if err := migrator.Up(adminURL.String(), "../migrations"); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(t.Context(), adminURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func monitoringTestCache(t *testing.T) (*cache.RedisCache, *redis.Client) {
	t.Helper()
	raw := os.Getenv("INTEGRATION_REDIS_URL")
	if raw == "" {
		t.Skip("INTEGRATION_REDIS_URL is required")
	}
	options, err := redis.ParseURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	options.DB = 15
	client := redis.NewClient(options)
	if err := client.Ping(t.Context()).Err(); err != nil {
		client.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return cache.NewRedisCache(client), client
}

func createPriceMonitor(t *testing.T, db *sql.DB, endpointURL string, verified bool) domain.Monitor {
	t.Helper()
	ctx := t.Context()
	var userID, contactID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO users(name,email,password) VALUES('Test','test-' || gen_random_uuid()::text || '@example.com','password') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO contacts(name) VALUES('email') ON CONFLICT(name) DO UPDATE SET name=EXCLUDED.name RETURNING id`).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO user_contacts(user_id,contact_id,contact,is_active,is_verified) VALUES($1,$2,'test@example.com',true,$3)`, userID, contactID, verified); err != nil {
		t.Fatal(err)
	}
	targetHostnameID := ensureTargetHostname(t, db, endpointURL)
	monitor, err := repository.NewMonitorRepository(db).Create(ctx, domain.CreateMonitorInput{
		UserID: userID, Name: "Price monitor", PageURL: endpointURL, CanonicalURL: endpointURL,
		TargetHostnameID: targetHostnameID, PathType: "json", RulesCountToFinish: 1,
		Rules: []domain.CreateMonitorRuleInput{{Description: "Price <= 400", JSONPath: "woocommerce_product_object.regular_price", ComparisonOperator: "<=", ExpectedValue: 400}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return monitor
}

func ensureTargetHostname(t *testing.T, db *sql.DB, endpointURL string) int64 {
	t.Helper()
	u, err := url.Parse(endpointURL)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.QueryRowContext(t.Context(), `
		INSERT INTO target_hostnames(hostname,limit_per_minute)
		VALUES($1,60)
		ON CONFLICT(hostname) DO UPDATE SET hostname=EXCLUDED.hostname
		RETURNING id
	`, strings.ToLower(u.Hostname())).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestScheduledPriceMonitorLifecycle(t *testing.T) {
	db := monitoringTestDatabase(t)
	cases := []struct {
		name, body, monitorStatus string
		notifications             int
	}{
		{"below threshold", `{"woocommerce_product_object":{"regular_price":399.99}}`, "success", 1},
		{"equal threshold", `{"woocommerce_product_object":{"regular_price":400}}`, "success", 1},
		{"above threshold", `{"woocommerce_product_object":{"regular_price":400.01}}`, "monitoring", 0},
		{"missing price", `{"woocommerce_product_object":{}}`, "monitoring", 0},
		{"numeric string price", `{"woocommerce_product_object":{"regular_price":"399.99"}}`, "success", 1},
		{"non-numeric string price", `{"woocommerce_product_object":{"regular_price":"not-a-number"}}`, "monitoring", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			monitor := createPriceMonitor(t, db, "https://example.com/"+uuid.NewString(), true)
			executionRepo := repository.NewMonitoringExecutionRepository(db)
			execution := service.NewMonitoringExecutionService(executionRepo, nil, 10*time.Minute, time.Minute, nil, nil).WithFetcher(fixturePriceBody(t, tc.body))
			count, err := execution.Schedule(t.Context())
			if err != nil || count != 1 {
				t.Fatalf("schedule: count=%d err=%v", count, err)
			}
			var fetchID, messageID string
			if err := db.QueryRowContext(t.Context(), `SELECT payload->>'fetch_id',event_id::text FROM outbox_events WHERE aggregate_type='monitor' AND aggregate_id=$1 ORDER BY id DESC LIMIT 1`, monitor.ID).Scan(&fetchID, &messageID); err != nil {
				t.Fatal(err)
			}
			if err := execution.ProcessFetch(t.Context(), fetchID, messageID); err != nil {
				t.Fatalf("process fetch: %v", err)
			}
			var fetchStatus, monitorStatus, endpointStatus string
			if err := db.QueryRowContext(t.Context(), `SELECT f.status,m.status,m.check_status FROM endpoint_fetches f JOIN monitored_endpoints e ON e.id=f.endpoint_id JOIN monitors m ON m.endpoint_id=e.id WHERE f.fetch_id=$1`, fetchID).Scan(&fetchStatus, &monitorStatus, &endpointStatus); err != nil {
				t.Fatal(err)
			}
			if fetchStatus != "succeeded" || endpointStatus != "idle" || monitorStatus != tc.monitorStatus {
				t.Fatalf("states: fetch=%s endpoint=%s monitor=%s; want succeeded, idle, %s", fetchStatus, endpointStatus, monitorStatus, tc.monitorStatus)
			}
			var notificationCount, inboxCount int
			if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM notifications WHERE monitor_id=$1`, monitor.ID).Scan(&notificationCount); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM processed_messages WHERE consumer_name=$1 AND message_id=$2`, repository.EndpointFetchConsumer, messageID).Scan(&inboxCount); err != nil {
				t.Fatal(err)
			}
			if notificationCount != tc.notifications || inboxCount != 1 {
				t.Fatalf("effects: notifications=%d inbox=%d", notificationCount, inboxCount)
			}
			var scheduled bool
			if err := db.QueryRowContext(t.Context(), `SELECT next_check_at IS NOT NULL FROM monitors WHERE id=$1`, monitor.ID).Scan(&scheduled); err != nil {
				t.Fatal(err)
			}
			if scheduled != (tc.monitorStatus == "monitoring") {
				t.Fatal("terminal monitor retained a next check or active monitor lost its schedule")
			}
			if err := execution.ProcessFetch(t.Context(), fetchID, messageID); err != nil {
				t.Fatalf("redelivery: %v", err)
			}
			if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM notifications WHERE monitor_id=$1`, monitor.ID).Scan(&notificationCount); err != nil {
				t.Fatal(err)
			}
			if notificationCount != tc.notifications {
				t.Fatalf("redelivery created %d notifications", notificationCount)
			}
			if tc.notifications == 1 {
				var notificationID int64
				if err := db.QueryRowContext(t.Context(), `SELECT id FROM notifications WHERE monitor_id=$1`, monitor.ID).Scan(&notificationID); err != nil {
					t.Fatal(err)
				}
				job, err := executionRepo.ClaimNotification(t.Context(), notificationID, time.Minute)
				if err != nil {
					t.Fatalf("claim notification: %v", err)
				}
				if job.Channel != "email" {
					t.Fatalf("notification channel=%s", job.Channel)
				}
				if err := executionRepo.CompleteNotification(t.Context(), notificationID, job.LeaseToken, uuid.NewString(), "", 0, false); err != nil {
					t.Fatalf("complete notification: %v", err)
				}
				var deliveryStatus string
				if err := db.QueryRowContext(t.Context(), `SELECT status FROM notifications WHERE id=$1`, notificationID).Scan(&deliveryStatus); err != nil {
					t.Fatal(err)
				}
				if deliveryStatus != "sent" {
					t.Fatalf("notification status=%s", deliveryStatus)
				}
			}
		})
	}
}

func TestUnverifiedContactDoesNotSchedule(t *testing.T) {
	db := monitoringTestDatabase(t)
	createPriceMonitor(t, db, "https://example.com/"+uuid.NewString(), false)
	count, err := repository.NewMonitoringExecutionRepository(db).ScheduleDue(t.Context(), 10*time.Minute, 100)
	if err != nil || count != 0 {
		t.Fatalf("schedule without verified contact: count=%d err=%v", count, err)
	}
}

func TestExpiredWorkerCannotApplyStaleMonitorResult(t *testing.T) {
	db := monitoringTestDatabase(t)
	monitor := createPriceMonitor(t, db, "https://example.com/"+uuid.NewString(), true)
	repo := repository.NewMonitoringExecutionRepository(db)
	if _, err := repo.ScheduleDue(t.Context(), 10*time.Minute, 100); err != nil {
		t.Fatal(err)
	}
	var fetchID string
	if err := db.QueryRowContext(t.Context(), `SELECT fetch_id::text FROM endpoint_fetches WHERE endpoint_id=$1`, monitor.ID).Scan(&fetchID); err != nil {
		t.Fatal(err)
	}
	staleToken := uuid.NewString()
	if _, err := repo.ClaimFetch(t.Context(), fetchID, staleToken, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE endpoint_fetches SET lease_until=NOW()-INTERVAL '1 second' WHERE fetch_id=$1`, fetchID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecoverExpiredFetches(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	activeToken := uuid.NewString()
	if _, err := repo.ClaimFetch(t.Context(), fetchID, activeToken, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishMonitor(t.Context(), fetchID, staleToken, monitor.ID, true, "stale result", monitor.Rules); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRowContext(t.Context(), `SELECT status FROM monitors WHERE id=$1`, monitor.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "monitoring" {
		t.Fatalf("stale worker changed monitor to %s", status)
	}
	if err := repo.FinishMonitor(t.Context(), fetchID, activeToken, monitor.ID, true, "fresh result", monitor.Rules); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT status FROM monitors WHERE id=$1`, monitor.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "success" {
		t.Fatalf("active worker left monitor in %s", status)
	}
}

func TestInvalidJSONRetriesAndOversizedResponseStopsMonitor(t *testing.T) {
	db := monitoringTestDatabase(t)
	for _, tc := range []struct {
		name, body, fetchStatus, endpointStatus, monitorStatus string
		maxBody                                                int64
	}{
		{"invalid JSON", `{"woocommerce_product_object":`, "failed", "retry_wait", "monitoring", 1 << 20},
		{"oversized JSON", `{"woocommerce_product_object":{"regular_price":399}}`, "failed", "error", "error", 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			monitor := createPriceMonitor(t, db, "https://example.com/"+uuid.NewString(), true)
			repo := repository.NewMonitoringExecutionRepository(db)
			if _, err := repo.ScheduleDue(t.Context(), 10*time.Minute, 100); err != nil {
				t.Fatal(err)
			}
			var fetchID, messageID string
			if err := db.QueryRowContext(t.Context(), `SELECT payload->>'fetch_id',event_id::text FROM outbox_events WHERE aggregate_id=$1 AND event_type='endpoint.fetch.json'`, monitor.ID).Scan(&fetchID, &messageID); err != nil {
				t.Fatal(err)
			}
			execution := service.NewMonitoringExecutionService(repo, nil, 10*time.Minute, time.Minute, nil, nil).WithFetcher(fixtureDocumentFailure(tc.name))
			if err := execution.ProcessFetch(t.Context(), fetchID, messageID); err != nil {
				t.Fatal(err)
			}
			var fetchStatus, endpointStatus, monitorStatus string
			if err := db.QueryRowContext(t.Context(), `SELECT f.status,m.check_status,m.status FROM endpoint_fetches f JOIN monitored_endpoints e ON e.id=f.endpoint_id JOIN monitors m ON m.endpoint_id=e.id WHERE f.fetch_id=$1`, fetchID).Scan(&fetchStatus, &endpointStatus, &monitorStatus); err != nil {
				t.Fatal(err)
			}
			if fetchStatus != tc.fetchStatus || endpointStatus != tc.endpointStatus || monitorStatus != tc.monitorStatus {
				t.Fatalf("states: fetch=%s endpoint=%s monitor=%s", fetchStatus, endpointStatus, monitorStatus)
			}
		})
	}
}

func TestNotificationRetryThenPermanentContactFailure(t *testing.T) {
	db := monitoringTestDatabase(t)
	monitor := createPriceMonitor(t, db, "https://example.com/"+uuid.NewString(), true)
	repo := repository.NewMonitoringExecutionRepository(db)
	if _, err := repo.ScheduleDue(t.Context(), 10*time.Minute, 100); err != nil {
		t.Fatal(err)
	}
	var fetchID, fetchMessageID string
	if err := db.QueryRowContext(t.Context(), `SELECT payload->>'fetch_id',event_id::text FROM outbox_events WHERE event_type='endpoint.fetch.json' AND aggregate_id=$1`, monitor.ID).Scan(&fetchID, &fetchMessageID); err != nil {
		t.Fatal(err)
	}
	execution := service.NewMonitoringExecutionService(repo, nil, 10*time.Minute, time.Minute, nil, nil).WithFetcher(fixturePrice("399"))
	if err := execution.ProcessFetch(t.Context(), fetchID, fetchMessageID); err != nil {
		t.Fatal(err)
	}
	var notificationID, contactID int64
	var firstMessageID string
	if err := db.QueryRowContext(t.Context(), `SELECT n.id,n.user_contact_id,o.event_id::text FROM notifications n JOIN outbox_events o ON o.aggregate_type='notification' AND o.aggregate_id=n.id WHERE n.monitor_id=$1`, monitor.ID).Scan(&notificationID, &contactID, &firstMessageID); err != nil {
		t.Fatal(err)
	}
	firstJob, err := repo.ClaimNotification(t.Context(), notificationID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteNotification(t.Context(), notificationID, firstJob.LeaseToken, firstMessageID, "temporary provider error", 0, true); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRowContext(t.Context(), `SELECT status FROM notifications WHERE id=$1`, notificationID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("retry notification status=%s", status)
	}
	var nextMessageID string
	if err := db.QueryRowContext(t.Context(), `SELECT event_id::text FROM outbox_events WHERE aggregate_type='notification' AND aggregate_id=$1 ORDER BY id DESC LIMIT 1`, notificationID).Scan(&nextMessageID); err != nil {
		t.Fatal(err)
	}
	if nextMessageID == firstMessageID {
		t.Fatal("retry did not create a new outbox event")
	}
	nextJob, err := repo.ClaimNotification(t.Context(), notificationID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DisableContactForPermanentNotificationFailure(t.Context(), notificationID, nextJob.LeaseToken, nextJob.Contact, nextJob.ContactUpdatedAt, nextMessageID, "provider returned HTTP 404"); err != nil {
		t.Fatal(err)
	}
	var active bool
	if err := db.QueryRowContext(t.Context(), `SELECT n.status,uc.is_active FROM notifications n JOIN user_contacts uc ON uc.id=n.user_contact_id WHERE n.id=$1`, notificationID).Scan(&status, &active); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || active {
		t.Fatalf("permanent failure: notification=%s contact_active=%v", status, active)
	}
}

func TestNotificationLeaseRecoveryAfterWorkerCrash(t *testing.T) {
	db := monitoringTestDatabase(t)
	monitor := createPriceMonitor(t, db, "https://example.com/"+uuid.NewString(), true)
	repo := repository.NewMonitoringExecutionRepository(db)
	if _, err := repo.ScheduleDue(t.Context(), 10*time.Minute, 100); err != nil {
		t.Fatal(err)
	}
	var fetchID, messageID string
	if err := db.QueryRowContext(t.Context(), `SELECT payload->>'fetch_id',event_id::text FROM outbox_events WHERE event_type='endpoint.fetch.json' AND aggregate_id=$1`, monitor.ID).Scan(&fetchID, &messageID); err != nil {
		t.Fatal(err)
	}
	execution := service.NewMonitoringExecutionService(repo, nil, 10*time.Minute, time.Minute, nil, nil).WithFetcher(fixturePrice("399"))
	if err := execution.ProcessFetch(t.Context(), fetchID, messageID); err != nil {
		t.Fatal(err)
	}
	var notificationID int64
	if err := db.QueryRowContext(t.Context(), `SELECT id FROM notifications WHERE monitor_id=$1`, monitor.ID).Scan(&notificationID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNotification(t.Context(), notificationID, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE notifications SET next_attempt_at=NOW()-INTERVAL '1 second' WHERE id=$1`, notificationID); err != nil {
		t.Fatal(err)
	}
	recovered, err := repo.RecoverExpiredNotifications(t.Context(), 100)
	if err != nil || recovered != 1 {
		t.Fatalf("recover notification: count=%d err=%v", recovered, err)
	}
	var status string
	if err := db.QueryRowContext(t.Context(), `SELECT status FROM notifications WHERE id=$1`, notificationID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("notification status=%s", status)
	}
	var events int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM outbox_events WHERE event_type='notification.deliver' AND aggregate_id=$1`, notificationID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 2 {
		t.Fatalf("notification outbox events=%d", events)
	}
}

func TestPriceMonitorAPIToDatabaseResult(t *testing.T) {
	db := monitoringTestDatabase(t)
	var userID, contactID int64
	if err := db.QueryRowContext(t.Context(), `INSERT INTO users(name,email,password) VALUES('Test','api-' || gen_random_uuid()::text || '@example.com','password') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `INSERT INTO contacts(name) VALUES('email') ON CONFLICT(name) DO UPDATE SET name=EXCLUDED.name RETURNING id`).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO user_contacts(user_id,contact_id,contact,is_active,is_verified) VALUES($1,$2,'test@example.com',true,true)`, userID, contactID); err != nil {
		t.Fatal(err)
	}
	secret := uuid.NewString()
	token, err := authjwt.Generate(secret, authjwt.Claims{AuthVersion: 1, UserID: userID, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	endpointURL := "https://example.com/" + uuid.NewString()
	ensureTargetHostname(t, db, endpointURL)
	targetRepository := repository.NewTargetHostnameRepository(db)
	router := monitoringRouter(t, httpapi.RouterConfig{DB: db, Logger: logging.New(io.Discard), MonitorService: service.NewMonitorService(repository.NewMonitorRepository(db)).WithTargetHostnameRepository(targetRepository), TargetHostnameService: service.NewTargetHostnameService(targetRepository), IdempotencyService: service.NewIdempotencyService(repository.NewIdempotencyRepository(db), time.Hour), JWTSecret: secret})
	requestBody := fmt.Sprintf(`{"name":"API price monitor","url":%q,"page_url":%q,"type":"json","path_type":"json","rules":[{"path":"woocommerce_product_object.regular_price","operator":"<=","value":400,"description":"Price <= 400"}]}`, endpointURL, endpointURL)
	request := httptest.NewRequest(http.MethodPost, "/me/monitors", strings.NewReader(requestBody))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create monitor HTTP %d: %s", response.Code, response.Body.String())
	}
	var created struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != "monitoring" || created.Name != "API price monitor" || created.Type != domain.EndpointTypeJSON {
		t.Fatalf("created monitor=%+v", created)
	}
	repo := repository.NewMonitoringExecutionRepository(db)
	patchRequest := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/me/monitors/%d", created.ID), strings.NewReader(`{"name":"Renamed price monitor"}`))
	patchRequest.Header.Set("Authorization", "Bearer "+token)
	patchResponse := httptest.NewRecorder()
	router.ServeHTTP(patchResponse, patchRequest)
	if patchResponse.Code != http.StatusOK {
		t.Fatalf("update monitor HTTP %d: %s", patchResponse.Code, patchResponse.Body.String())
	}
	var updated struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(patchResponse.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Renamed price monitor" {
		t.Fatalf("updated monitor=%+v", updated)
	}

	if _, err := db.ExecContext(t.Context(), `UPDATE monitors SET next_check_at=NOW()+INTERVAL '1 hour' WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.ScheduleDue(t.Context(), 10*time.Minute, 100); err != nil || count != 0 {
		t.Fatalf("scheduled before due time: count=%d err=%v", count, err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE monitors SET next_check_at=NOW()-INTERVAL '1 second' WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	count, err := repo.ScheduleDue(t.Context(), 10*time.Minute, 100)
	if err != nil || count != 1 {
		t.Fatalf("scheduled count=%d err=%v", count, err)
	}
	var fetchID, messageID string
	var endpointID int64
	if err := db.QueryRowContext(t.Context(), `SELECT f.fetch_id::text,o.event_id::text,f.endpoint_id FROM endpoint_fetches f JOIN outbox_events o ON o.aggregate_type='monitor' AND o.aggregate_id=f.monitor_id AND o.payload->>'fetch_id'=f.fetch_id::text ORDER BY f.created_at DESC LIMIT 1`).Scan(&fetchID, &messageID, &endpointID); err != nil {
		t.Fatal(err)
	}
	execution := service.NewMonitoringExecutionService(repo, nil, 10*time.Minute, time.Minute, nil, nil).WithFetcher(fixturePrice("399"))
	if err := execution.ProcessFetch(t.Context(), fetchID, messageID); err != nil {
		t.Fatal(err)
	}
	getRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/me/monitors/%d", created.ID), nil)
	getRequest.Header.Set("Authorization", "Bearer "+token)
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("read result HTTP %d: %s", getResponse.Code, getResponse.Body.String())
	}
	var result struct {
		Name   string `json:"name"`
		Type   string `json:"type"`
		Status string `json:"status"`
		Rules  []struct {
			Path string `json:"path"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(getResponse.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Name != "Renamed price monitor" || result.Type != domain.EndpointTypeJSON || result.Status != "success" || len(result.Rules) != 1 || result.Rules[0].Path != "woocommerce_product_object.regular_price" {
		t.Fatalf("result: %+v", result)
	}
	var fetchStatus string
	if err := db.QueryRowContext(t.Context(), `SELECT status FROM endpoint_fetches WHERE fetch_id=$1`, fetchID).Scan(&fetchStatus); err != nil {
		t.Fatal(err)
	}
	if fetchStatus != "succeeded" {
		t.Fatalf("fetch status=%s", fetchStatus)
	}
}
