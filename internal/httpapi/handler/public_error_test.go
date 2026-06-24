package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"monitoring_service/internal/service"
)

type errorResponseRecorder struct {
	*httptest.ResponseRecorder
	cause error
}

func (r *errorResponseRecorder) SetRequestError(err error) { r.cause = err }

func TestServiceErrorsExposeOnlyReviewedMessages(t *testing.T) {
	private := errors.New("SELECT private_column password=fixture")
	for _, tc := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"business", service.NewPublicError(service.ErrConflict, "different browser_page_url", private), 409, "different browser_page_url"},
		{"wrapped business", fmt.Errorf("private transport detail: %w", service.NewPublicError(service.ErrNotFound, "monitor was not found", private)), 404, "monitor was not found"},
		{"raw conflict", fmt.Errorf("%w: %v", service.ErrConflict, private), 409, "request conflicts with the current resource state"},
		{"raw validation", fmt.Errorf("%w: %v", service.ErrValidation, private), 400, "request parameters are invalid"},
		{"raw unauthorized", fmt.Errorf("%w: %v", service.ErrUnauthorized, private), 401, "authentication failed"},
		{"raw unavailable", fmt.Errorf("%w: %v", service.ErrServiceUnavailable, private), 503, "service is temporarily unavailable; try again later"},
		{"unexpected", private, 500, "failed to process request; try again later"},
		{"operation", service.NewPublicError(nil, "could not create monitor; try again later", private), 500, "could not create monitor; try again later"},
		{"deadline", fmt.Errorf("private network: %w", context.DeadlineExceeded), 504, "request timed out; try again later"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &errorResponseRecorder{ResponseRecorder: httptest.NewRecorder()}
			writeServiceError(recorder, tc.err)
			var response errorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != tc.status || response.Error.Code != http.StatusText(tc.status) || response.Error.Message != tc.message {
				t.Fatalf("incorrect public response: %d %s", recorder.Code, recorder.Body.String())
			}
			if !errors.Is(recorder.cause, tc.err) {
				t.Fatal("original error did not reach the request logger")
			}
			for _, sensitive := range []string{"private_", "SELECT", "password=", "transport detail", "network:"} {
				if strings.Contains(recorder.Body.String(), sensitive) {
					t.Fatalf("diagnostic leaked: %s", recorder.Body.String())
				}
			}
		})
	}
}

func TestJSONTypeErrorDoesNotExposeGoTypeNames(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/me/monitors", strings.NewReader(`{"rules":[{"source":42}]}`))
	var input createMonitorRequest
	if decodeJSON(response, request, &input) || response.Code != http.StatusBadRequest {
		t.Fatal("incorrect source type was accepted")
	}
	if strings.Contains(response.Body.String(), "domain.") || !strings.Contains(response.Body.String(), "must be an object") {
		t.Fatalf("incorrect type description: %s", response.Body.String())
	}
}
