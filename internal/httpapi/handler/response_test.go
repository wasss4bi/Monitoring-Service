package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"monitoring_service/internal/service"
)

func TestDecodeJSONKeepsMalformedJSONSeparateFromValidation(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/me/monitors/1", strings.NewReader(`{"name":`))
	var target updateMonitorRequest

	if decodeJSON(recorder, request, &target) {
		t.Fatal("decodeJSON() succeeded")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
	var response errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != "Bad Request" || response.Error.Message != "invalid json" {
		t.Fatalf("response = %#v", response)
	}
}

func TestDecodeJSONReturnsAllUnknownFieldsAsValidationErrors(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/me/monitors/1", strings.NewReader(`{
		"url":"https://example.com",
		"type":"json",
		"path_type":"json",
		"name":"monitor",
		"rules":[]
	}`))
	var target updateMonitorRequest

	if decodeJSON(recorder, request, &target) {
		t.Fatal("decodeJSON() succeeded")
	}
	var response validationErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"url":       {"field is not allowed"},
		"type":      {"field is not allowed"},
		"path_type": {"field is not allowed"},
		"rules":     {"field is not allowed"},
	}
	if response.Error.Code != "Bad Request" || response.Error.Message != "validation error" || !reflect.DeepEqual(response.Error.Fields, want) {
		t.Fatalf("response = %#v", response)
	}
}

func TestDecodeJSONReturnsTypeErrorsAsValidationErrors(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/me/monitors/1", strings.NewReader(`{"rules_count_to_finish":"one"}`))
	var target updateMonitorRequest

	if decodeJSON(recorder, request, &target) {
		t.Fatal("decodeJSON() succeeded")
	}
	var response validationErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"rules_count_to_finish": {"must be an integer"}}
	if response.Error.Message != "validation error" || !reflect.DeepEqual(response.Error.Fields, want) {
		t.Fatalf("response = %#v", response)
	}
}

func TestWriteServiceErrorWritesGatewayTimeout(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeServiceError(recorder, context.DeadlineExceeded)

	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusGatewayTimeout)
	}
}

func TestWriteServiceErrorWritesServiceUnavailable(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeServiceError(recorder, service.ErrServiceUnavailable)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}

	var response errorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.Message != "service is temporarily unavailable; try again later" {
		t.Errorf("unexpected public message: %q", response.Error.Message)
	}
}
