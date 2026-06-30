//go:build integration

package integration

import (
	"github.com/google/uuid"
	"monitoring_service/internal/authjwt"
	"monitoring_service/internal/httpapi"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/service"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMonitorCreateRecoversAfterResponsePersistenceFailure(t *testing.T) {
	db := monitoringTestDatabase(t)
	seed := createPriceMonitor(t, db, "https://recovery.example.com/seed", true)
	secret := uuid.NewString()
	token, err := authjwt.Generate(secret, authjwt.Claims{AuthVersion: 1, UserID: seed.UserID, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	router := monitoringRouter(t, httpapi.RouterConfig{DB: db, Logger: logging.New(io.Discard), JWTSecret: secret, MonitorService: service.NewMonitorService(repository.NewMonitorRepository(db)).WithTargetHostnameRepository(repository.NewTargetHostnameRepository(db)), IdempotencyService: service.NewIdempotencyService(repository.NewIdempotencyRepository(db), time.Hour)})
	_, err = db.ExecContext(t.Context(), `CREATE FUNCTION reject_response() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.response_code IS NOT NULL THEN RAISE EXCEPTION 'injected'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_response BEFORE UPDATE ON idempotency_keys FOR EACH ROW EXECUTE FUNCTION reject_response()`)
	if err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	body := `{"name":"recover","url":"https://recovery.example.com/price","page_url":"https://recovery.example.com/product","type":"json","path_type":"json","rules":[{"description":"price","path":"price","operator":"<","value":10}]}`
	send := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/me/monitors", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := send(); w.Code != 500 {
		t.Fatalf("first response=%d %s", w.Code, w.Body)
	}
	if _, err = db.ExecContext(t.Context(), `DROP TRIGGER reject_response ON idempotency_keys`); err != nil {
		t.Fatal(err)
	}
	if w := send(); w.Code != 201 {
		t.Fatalf("recovered response=%d %s", w.Code, w.Body)
	}
	var count int
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM monitors WHERE name='recover'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("monitors=%d err=%v", count, err)
	}
	if _, err = db.ExecContext(t.Context(), `UPDATE monitors SET name='changed' WHERE name='recover'`); err != nil {
		t.Fatal(err)
	}
	if w := send(); w.Code != 201 || !strings.Contains(w.Body.String(), `"name":"changed"`) {
		t.Fatalf("updated replay=%d %s", w.Code, w.Body)
	}
	if _, err = db.ExecContext(t.Context(), `UPDATE monitors SET deleted_at=NOW() WHERE name='changed'`); err != nil {
		t.Fatal(err)
	}
	if w := send(); w.Code != 404 {
		t.Fatalf("deleted replay=%d %s", w.Code, w.Body)
	}
}
