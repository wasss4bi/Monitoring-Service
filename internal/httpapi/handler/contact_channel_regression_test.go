package handler

import (
	"context"
	"encoding/json"
	"github.com/lib/pq"
	"monitoring_service/internal/domain"
	"monitoring_service/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type missingChannelRepository struct{ userContactRepositoryFake }

func (*missingChannelRepository) Create(context.Context, domain.CreateUserContactInput) (domain.UserContact, error) {
	return domain.UserContact{}, &pq.Error{Code: "23503", Constraint: "user_contacts_contact_id_fkey"}
}
func TestUserContactHandlerRejectsMissingChannel(t *testing.T) {
	h := NewUserContactHandler(service.NewUserContactService(&missingChannelRepository{}))
	req := httptest.NewRequest(http.MethodPost, "/me/contacts", strings.NewReader(`{"contact_id":999,"contact":"test"}`))
	req = req.WithContext(context.WithValue(req.Context(), authenticatedUserContextKey{}, int64(42)))
	rec := httptest.NewRecorder()
	h.UserContacts(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Fields map[string][]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Error.Fields["contact_id"]) == 0 {
		t.Fatalf("invalid field error: %s", rec.Body.String())
	}
}
