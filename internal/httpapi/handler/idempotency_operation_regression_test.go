package handler

import (
	"monitoring_service/internal/service"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIdempotencyRejectsDifferentOperations(t *testing.T) {
	store := &inMemoryIdempotencyRepository{}
	executor := &idempotencyExecutor{service: service.NewIdempotencyService(store, time.Hour)}
	uid := int64(17)
	calls := 0
	for i, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/me/monitors/1/stop", 204},
		{http.MethodPost, "/me/monitors/2/stop", 409},
		{http.MethodPost, "/me/monitors/1/resume", 409},
		{http.MethodDelete, "/me/monitors/1/stop", 409},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Idempotency-Key", "shared-key")
		w := httptest.NewRecorder()
		executor.run(w, req, &uid, func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(204) })
		if w.Code != tc.want {
			t.Errorf("request %d status=%d want=%d", i, w.Code, tc.want)
		}
	}
	if calls != 1 {
		t.Fatalf("handler ran %d times", calls)
	}
}
