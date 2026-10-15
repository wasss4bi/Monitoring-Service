package httpapi

import (
	"context"
	"monitoring_service/internal/authjwt"
	"monitoring_service/internal/domain"
	"monitoring_service/internal/httpapi/handler"
	"monitoring_service/internal/service"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type channelRouteRoles struct{}

func (channelRouteRoles) AccessState(context.Context, int64) (string, int64, error) {
	return "admin", 1, nil
}

type channelRouteRepo struct{}

func (channelRouteRepo) List(context.Context) ([]domain.Contact, error) { return nil, nil }
func (channelRouteRepo) Get(_ context.Context, id int64) (domain.Contact, error) {
	return domain.Contact{ID: id, Name: "email"}, nil
}
func (channelRouteRepo) Create(context.Context, domain.CreateContactInput) (domain.Contact, error) {
	return domain.Contact{}, nil
}
func (channelRouteRepo) Update(_ context.Context, id int64, name string) (domain.Contact, error) {
	return domain.Contact{ID: id, Name: name}, nil
}
func (channelRouteRepo) Delete(context.Context, int64) (int64, error) { return 1, nil }
func TestChannelByIDRoute(t *testing.T) {
	ac, err := handler.NewAccessControl(channelRouteRoles{})
	if err != nil {
		t.Fatal(err)
	}
	token, err := authjwt.Generate("secret", authjwt.Claims{UserID: 1, AuthVersion: 1, ExpiresAt: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(RouterConfig{AccessControl: ac, JWTSecret: "secret", ContactService: service.NewContactService(channelRouteRepo{}), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	for _, tc := range []struct {
		method, body string
		want         int
	}{{http.MethodGet, "", 200}, {http.MethodPut, `{"name":"telegram"}`, 200}, {http.MethodDelete, "", 204}} {
		r := httptest.NewRequest(tc.method, "/contacts/7", strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s status=%d want=%d body=%s", tc.method, w.Code, tc.want, w.Body.String())
		}
	}
}
