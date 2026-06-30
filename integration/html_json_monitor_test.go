//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"monitoring_service/internal/authjwt"
	"monitoring_service/internal/httpapi"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/service"
)

func TestHTMLJSONSourceMonitorAPIAndIdentity(t *testing.T) {
	db := monitoringTestDatabase(t)
	seed := createPriceMonitor(t, db, "https://source.example.com/seed", true)
	secret := uuid.NewString()
	token, err := authjwt.Generate(secret, authjwt.Claims{AuthVersion: 1, UserID: seed.UserID, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	router := monitoringRouter(t, httpapi.RouterConfig{DB: db, Logger: logging.New(io.Discard), JWTSecret: secret,
		MonitorService:     service.NewMonitorService(repository.NewMonitorRepository(db)).WithTargetHostnameRepository(repository.NewTargetHostnameRepository(db)),
		IdempotencyService: service.NewIdempotencyService(repository.NewIdempotencyRepository(db), time.Hour)})
	source := map[string]any{"script_contains": "__STATE__", "object_key": "item", "match": map[string]any{"path": "sku", "value": "one"}}
	body := map[string]any{"url": "https://source.example.com/api/item", "page_url": "https://source.example.com/item/one", "type": "json", "path_type": "json", "name": "Embedded JSON", "json_source": source,
		"rules": []map[string]any{{"path": "price", "description": "Price <= 24000", "operator": "<=", "value": 24000}}}
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
	body["browser_page_url"] = body["page_url"]
	if got := post().Code; got != http.StatusBadRequest {
		t.Fatalf("ambiguous sources %d", got)
	}
	delete(body, "browser_page_url")
	first := post()
	if first.Code != http.StatusCreated {
		t.Fatalf("create %d: %s", first.Code, first.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["json_source"] == nil {
		t.Fatal("missing source in response")
	}
	for _, location := range []string{fmt.Sprintf("/me/monitors/%.0f", created["id"]), "/me/monitors"} {
		request := httptest.NewRequest(http.MethodGet, location, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "script_contains") {
			t.Fatalf("roundtrip %d: %s", response.Code, response.Body.String())
		}
	}
	if got := post().Code; got != http.StatusCreated {
		t.Fatalf("same source %d", got)
	}
	source["object_key"] = "other"
	if got := post().Code; got != http.StatusConflict {
		t.Fatalf("different source %d", got)
	}
	delete(body, "json_source")
	if got := post().Code; got != http.StatusConflict {
		t.Fatalf("source vs browser %d", got)
	}
	source["object_key"] = "item"
	body["json_source"] = source
	delete(body, "page_url")
	if got := post().Code; got != http.StatusBadRequest {
		t.Fatalf("missing page %d", got)
	}
	body["page_url"] = "https://source.example.com/item/one"
	source["unknown"] = true
	if got := post().Code; got != http.StatusBadRequest {
		t.Fatalf("unknown source field %d", got)
	}
}
