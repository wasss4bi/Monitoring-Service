package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/lib/pq"

	"monitoring_service/internal/domain"
	"monitoring_service/internal/service"
)

func TestUserHandlerCreateReturnsEmailFieldErrorOnConflict(t *testing.T) {
	handler := newConflictUserHandler()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{
		"name": "Alice",
		"email": "alice2@example.com",
		"password": "(Bambamtutu123)"
	}`))

	handler.create(recorder, request)

	assertEmailConflictResponse(t, recorder)
}

func TestUserHandlerUpdateReturnsEmailFieldErrorOnConflict(t *testing.T) {
	handler := newConflictUserHandler()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/users/1", strings.NewReader(`{
		"name": "Alice",
		"email": "alice2@example.com",
		"password": "(Bambamtutu123)"
	}`))

	handler.update(recorder, request, 1)

	assertEmailConflictResponse(t, recorder)
}

func newConflictUserHandler() *UserHandler {
	return NewUserHandler(service.NewUserService(conflictUserRepository{}))
}

func assertEmailConflictResponse(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}

	var got validationErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	want := validationErrorResponse{Error: validationErrorBody{
		Code:    "Conflict",
		Message: "validation error",
		Fields: map[string][]string{
			"email": {"this email is already exists"},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response = %#v, want %#v", got, want)
	}
}

type conflictUserRepository struct{}

func (conflictUserRepository) List(context.Context) ([]domain.User, error) {
	return nil, nil
}

func (conflictUserRepository) Create(context.Context, domain.User) (domain.User, error) {
	return domain.User{}, &pq.Error{Code: "23505"}
}

func (conflictUserRepository) GetByID(context.Context, int64) (domain.User, error) {
	return domain.User{}, nil
}

func (conflictUserRepository) GetPasswordHashByID(context.Context, int64) (domain.User, error) {
	return domain.User{}, nil
}

func (conflictUserRepository) GetByEmail(context.Context, string) (domain.User, error) {
	return domain.User{}, nil
}

func (conflictUserRepository) Update(context.Context, int64, domain.User) (domain.User, error) {
	return domain.User{}, &pq.Error{Code: "23505"}
}

func (conflictUserRepository) Delete(context.Context, int64) (int64, error) {
	return 0, nil
}
