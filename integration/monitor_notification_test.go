//go:build integration

package integration

import (
	"context"
	"encoding/base64"

	"strconv"
	"strings"
	"testing"
	"time"

	v1 "github.com/wasss4bi/values-from-body-fetcher/api/fetcher/v1"
	"monitoring_service/internal/authorization"
	"monitoring_service/internal/domain"

	"monitoring_service/internal/repository"
	"monitoring_service/internal/service"

	"github.com/google/uuid"
)

func TestMonitorNotificationContainsActualMatchedRules(t *testing.T) {
	for _, mode := range []string{"direct", "browser", "embedded", "html"} {
		t.Run(mode, func(t *testing.T) {
			db := monitoringTestDatabase(t)
			ctx := t.Context()
			monitor := createPriceMonitor(t, db, "https://notification.example.com/api", true)
			productURL := "https://notification.example.com/product"
			browserURL := "https://notification.example.com/browser-source"
			if _, err := db.ExecContext(ctx, `UPDATE monitors SET name='Кроссовки',page_url=$2,rules_count_to_finish=2 WHERE id=$1`, monitor.ID, productURL); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `UPDATE monitor_rules SET json_path='price',expected_value=28990,description='Цена ниже 28990' WHERE monitor_id=$1`, monitor.ID); err != nil {
				t.Fatal(err)
			}
			monitorRepo := repository.NewMonitorRepository(db)
			for _, rule := range []domain.CreateMonitorRuleInput{
				{JSONPath: "qty", ComparisonOperator: ">=", ExpectedValue: 1, Description: "Товар в наличии"},
				{JSONPath: "discount", ComparisonOperator: ">=", ExpectedValue: 10, Description: "Скидка от 10 процентов"},
			} {
				if _, err := monitorRepo.AddRule(ctx, monitor.UserID, monitor.ID, rule); err != nil {
					t.Fatal(err)
				}
			}
			var telegramID int64
			if err := db.QueryRowContext(ctx, `INSERT INTO contacts(name) VALUES('telegram') ON CONFLICT(name) DO UPDATE SET name=EXCLUDED.name RETURNING id`).Scan(&telegramID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO user_contacts(user_id,contact_id,contact,is_active,is_verified) VALUES($1,$2,'test-recipient',true,true)`, monitor.UserID, telegramID); err != nil {
				t.Fatal(err)
			}
			if mode == "browser" {
				if _, err := db.ExecContext(ctx, `UPDATE monitored_endpoints SET browser_page_url=$2 WHERE id=$1`, monitor.EndpointID, browserURL); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "embedded" {
				if _, err := db.ExecContext(ctx, `UPDATE monitored_endpoints SET json_source='{"script_contains":"__STATE__","object_key":"product","match":{"path":"sku","value":"test"}}'::jsonb WHERE id=$1`, monitor.EndpointID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "html" {
				if _, err := db.ExecContext(ctx, `UPDATE monitored_endpoints SET type='html' WHERE id=$1`, monitor.EndpointID); err != nil {
					t.Fatal(err)
				}
				if _, err := db.ExecContext(ctx, `UPDATE monitors SET path_type='selector' WHERE id=$1`, monitor.ID); err != nil {
					t.Fatal(err)
				}
			}
			repo := repository.NewMonitoringExecutionRepository(db)
			protector, err := authorization.NewForPurpose(base64.StdEncoding.EncodeToString(make([]byte, 32)), "target-session:v1")
			if err != nil {
				t.Fatal(err)
			}
			client := fixtureFetcher{
				fetch: func(_ context.Context, input *v1.FetchRequest) (*v1.FetchResponse, error) {
					wantMode := v1.FetchMode_FETCH_MODE_HTTP_JSON
					if mode == "browser" {
						wantMode = v1.FetchMode_FETCH_MODE_BROWSER_JSON_CAPTURE
					}
					if mode == "embedded" {
						wantMode = v1.FetchMode_FETCH_MODE_INLINE_JSON
					}
					if mode == "html" {
						wantMode = v1.FetchMode_FETCH_MODE_RENDERED_HTML
					}
					if input.Mode != wantMode {
						t.Error("wrong extraction mode")
					}
					if mode == "browser" && input.SourcePageUrl != browserURL {
						t.Error("wrong browser source")
					}
					if mode == "embedded" && (input.SourcePageUrl != productURL || input.InlineJsonSource == nil) {
						t.Error("missing embedded source")
					}
					if (mode == "browser" || mode == "embedded") && len(input.Session.State) == 0 {
						return fixtureFailure(input, v1.FetchErrorCode_FETCH_ERROR_CODE_SESSION_REQUIRED, 0), nil
					}
					values := map[string]string{}
					for _, selector := range input.Selectors {
						values[selector.Expression] = strconv.Itoa(notificationValue(selector.Expression))
					}
					return fixtureValues(input, values), nil
				},
				prepare: func(_ context.Context, input *v1.PrepareSessionRequest) (*v1.PrepareSessionResponse, error) {
					wantURL := browserURL
					wantMode := v1.SessionPreparationMode_SESSION_PREPARATION_MODE_BROWSER_CAPTURE
					if mode == "embedded" {
						wantURL = productURL
						wantMode = v1.SessionPreparationMode_SESSION_PREPARATION_MODE_INLINE_JSON
					}
					if input.SourcePageUrl != wantURL || input.Mode != wantMode {
						t.Error("wrong session preparation source")
					}
					return &v1.PrepareSessionResponse{RequestId: input.RequestId, SessionState: fixtureSession}, nil
				},
			}
			base := service.NewMonitoringExecutionService(repo, nil, time.Minute, time.Minute, nil, nil).WithFetcher(client).WithTargetSessionProtector(protector)
			if n, err := base.Schedule(ctx); err != nil || n != 1 {
				t.Fatalf("schedule=%d err=%v", n, err)
			}
			var fetchID, messageID string
			if err := db.QueryRowContext(ctx, `SELECT f.fetch_id::text,o.event_id::text FROM endpoint_fetches f JOIN outbox_events o ON o.payload->>'fetch_id'=f.fetch_id::text WHERE f.monitor_id=$1`, monitor.ID).Scan(&fetchID, &messageID); err != nil {
				t.Fatal(err)
			}
			process := func() error { return base.ProcessFetch(ctx, fetchID, messageID) }
			if err := process(); err != nil {
				t.Fatal(err)
			}
			loaded, err := monitorRepo.GetByUser(ctx, monitor.UserID, monitor.ID)
			if err != nil || loaded.Status != "success" {
				t.Fatalf("monitor=%+v err=%v", loaded, err)
			}
			var text, email string
			if err := db.QueryRowContext(ctx, `SELECT n.message FROM notifications n JOIN user_contacts uc ON uc.id=n.user_contact_id JOIN contacts c ON c.id=uc.contact_id WHERE n.monitor_id=$1 AND c.name='telegram'`, monitor.ID).Scan(&text); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, `SELECT n.message FROM notifications n JOIN user_contacts uc ON uc.id=n.user_contact_id JOIN contacts c ON c.id=uc.contact_id WHERE n.monitor_id=$1 AND c.name='email'`, monitor.ID).Scan(&email); err != nil {
				t.Fatal(err)
			}
			if email != `Monitor "Кроссовки" triggered!` {
				t.Fatalf("email changed: %s", email)
			}
			for _, part := range []string{"Монитор: Кроссовки", "URL товара: " + productURL, "Правила:\n- Цена ниже 28990\n- Товар в наличии", "Дата создания монитора: " + loaded.CreatedAt.UTC().Format("2006-01-02 15:04:05") + " GMT", "Дата выполнения монитора: " + loaded.CompletedAt.UTC().Format("2006-01-02 15:04:05") + " GMT"} {
				if !strings.Contains(text, part) {
					t.Errorf("missing %q in %s", part, text)
				}
			}
			if strings.Contains(text, "Скидка от") {
				t.Fatal("unmatched rule leaked into notification")
			}
			if strings.Contains(text, "API:") != (mode != "html") {
				t.Fatal("incorrect API line")
			}
			if _, err := monitorRepo.UpdateName(ctx, monitor.UserID, monitor.ID, "Changed later"); err != nil {
				t.Fatal(err)
			}
			if err := process(); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notifications WHERE monitor_id=$1`, monitor.ID).Scan(&count); err != nil || count != 2 {
				t.Fatalf("count=%d err=%v", count, err)
			}
			var persisted string
			if err := db.QueryRowContext(ctx, `SELECT message FROM notifications WHERE monitor_id=$1 AND message=$2`, monitor.ID, text).Scan(&persisted); err != nil || persisted != text {
				t.Fatal("notification snapshot changed")
			}
		})
	}
}

func notificationValue(path string) int {
	switch path {
	case "price":
		return 20000
	case "qty":
		return 1
	default:
		return 0
	}
}

func TestMonitorFailureNotificationKeepsExistingMessage(t *testing.T) {
	db := monitoringTestDatabase(t)
	ctx := t.Context()
	monitor := createPriceMonitor(t, db, "https://notification.example.com/failure", true)
	if _, err := db.ExecContext(ctx, `UPDATE contacts SET name='telegram' WHERE name='email'`); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewMonitoringExecutionRepository(db)
	if _, err := repo.ScheduleDue(ctx, time.Minute, 100); err != nil {
		t.Fatal(err)
	}
	var fetch string
	if err := db.QueryRowContext(ctx, `SELECT fetch_id::text FROM endpoint_fetches WHERE monitor_id=$1`, monitor.ID).Scan(&fetch); err != nil {
		t.Fatal(err)
	}
	token := uuid.NewString()
	if _, err := repo.ClaimFetch(ctx, fetch, token, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishMonitor(ctx, fetch, token, monitor.ID, false, "upstream failed", nil); err != nil {
		t.Fatal(err)
	}
	var text string
	if err := db.QueryRowContext(ctx, `SELECT message FROM notifications WHERE monitor_id=$1 AND event_type='monitor.failed'`, monitor.ID).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if text != `Monitor "Price monitor" stopped: upstream failed` {
		t.Fatalf("failure format changed: %s", text)
	}
}
