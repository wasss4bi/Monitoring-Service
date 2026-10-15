package handler

import (
	"context"
	"monitoring_service/internal/authjwt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type changingAuthState struct{ version int64 }

func (s *changingAuthState) AccessState(context.Context, int64) (string, int64, error) {
	return "user", s.version, nil
}
func TestRevokedVersionCannotAccess(t *testing.T) {
	state := &changingAuthState{version: 1}
	ac, err := NewAccessControl(state)
	if err != nil {
		t.Fatal(err)
	}
	token, err := authjwt.Generate("test", authjwt.Claims{UserID: 1, AuthVersion: 1, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	handler := ac.Require("test", "me", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	call := func() int {
		r := httptest.NewRequest("GET", "/me/monitors", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	if got := call(); got != 204 {
		t.Fatalf("before revocation: %d", got)
	}
	state.version++
	if got := call(); got != 401 {
		t.Fatalf("after revocation: %d", got)
	}
}
