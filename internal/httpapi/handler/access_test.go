package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"monitoring_service/internal/authjwt"
)

type accessRoles struct {
	role string
	err  error
}

func (r *accessRoles) AccessState(context.Context, int64) (string, int64, error) {
	return r.role, 1, r.err
}

func TestAccessPolicyAndImmediateRoleRevocation(t *testing.T) {
	roles := &accessRoles{}
	access, err := NewAccessControl(roles)
	if err != nil {
		t.Fatal(err)
	}
	token, err := authjwt.Generate("fixture-secret", authjwt.Claims{UserID: 7, AuthVersion: 1, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		resource, method, path, role string
		want                         int
	}{
		{"target-hostnames", "GET", "/target-hostnames", "user", 403},
		{"target-hostnames", "PATCH", "/target-hostnames/1", "admin", 204},
		{"users", "GET", "/users", "user", 403},
		{"profile", "GET", "/users/7", "user", 204},
		{"profile", "PUT", "/users/7", "user", 204},
		{"profile", "DELETE", "/users/7", "user", 403},
		{"profile", "GET", "/users/8", "user", 403},
		{"profile", "POST", "/users/8/check-password", "user", 403},
		{"profile", "POST", "/users/7/check-password", "user", 204},
		{"profile", "DELETE", "/users/8", "admin", 204},
		{"contacts", "GET", "/contacts", "user", 204},
		{"contacts", "POST", "/contacts", "user", 403},
		{"contacts", "POST", "/contacts", "admin", 204},
		{"me", "GET", "/me/monitors", "user", 204},
		{"target-hostnames", "PATCH", "/target-hostnames/1", "user", 403},
		{"me", "GET", "/me/monitors", "unknown", 403},
	} {
		t.Run(tc.role+tc.method+tc.path, func(t *testing.T) {
			roles.role = tc.role
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			access.Require("fixture-secret", tc.resource, next).ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	for _, tc := range []struct {
		err   error
		token bool
		want  int
	}{
		{nil, false, 401}, {sql.ErrNoRows, true, 401}, {errors.New("private SQL diagnostic"), true, 500},
	} {
		roles.err = tc.err
		req := httptest.NewRequest("GET", "/target-hostnames", nil)
		if tc.token {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		access.Require("fixture-secret", "target-hostnames", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected authorization") })).ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("status=%d want=%d", w.Code, tc.want)
		}
		if tc.want == 500 && w.Body.String() == tc.err.Error() {
			t.Fatal("database diagnostic leaked")
		}
	}
}
