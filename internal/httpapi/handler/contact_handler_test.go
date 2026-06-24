package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"monitoring_service/internal/domain"
	"monitoring_service/internal/service"
)

type userContactRepositoryFake struct {
	listedUserID int64
}

func (f *userContactRepositoryFake) ListByUser(_ context.Context, userID int64) ([]domain.UserContact, error) {
	f.listedUserID = userID
	return []domain.UserContact{}, nil
}

func (f *userContactRepositoryFake) Get(context.Context, int64, int64) (domain.UserContact, error) {
	return domain.UserContact{}, nil
}

func (f *userContactRepositoryFake) ListActiveByUser(context.Context, int64) ([]domain.UserContact, error) {
	return nil, nil
}

func (f *userContactRepositoryFake) Create(context.Context, domain.CreateUserContactInput) (domain.UserContact, error) {
	return domain.UserContact{}, nil
}

func (f *userContactRepositoryFake) Update(context.Context, int64, int64, domain.UpdateUserContactInput) (domain.UserContact, error) {
	return domain.UserContact{}, nil
}

func (f *userContactRepositoryFake) Delete(context.Context, int64, int64) (int64, error) {
	return 0, nil
}

func TestUserContactHandlerUsesUserIDFromJWTContext(t *testing.T) {
	repository := &userContactRepositoryFake{}
	handler := NewUserContactHandler(service.NewUserContactService(repository))
	request := httptest.NewRequest(http.MethodGet, "/me/contacts", nil)
	request = request.WithContext(context.WithValue(request.Context(), authenticatedUserContextKey{}, int64(42)))
	recorder := httptest.NewRecorder()

	handler.UserContacts(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repository.listedUserID != 42 {
		t.Fatalf("listed user ID = %d, want 42", repository.listedUserID)
	}
}
