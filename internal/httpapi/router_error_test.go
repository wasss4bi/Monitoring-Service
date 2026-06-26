package httpapi

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failingAuthorizationWriter struct{ *httptest.ResponseRecorder }

func (w *failingAuthorizationWriter) Write([]byte) (int, error) {
	return 0, errors.New("fixture write failure")
}

func TestAuthorizationUnavailableLogsResponseWriteFailure(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	router := NewRouter(RouterConfig{Logger: logger})
	w := &failingAuthorizationWriter{httptest.NewRecorder()}
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/contacts", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(logs.String(), "fixture write failure") {
		t.Fatalf("response write error was not logged: %s", logs.String())
	}
}
