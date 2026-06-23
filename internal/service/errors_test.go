package service

import (
	"errors"
	"fmt"
	"testing"

	"monitoring_service/internal/repository"

	"github.com/lib/pq"
)

func TestPublicErrorPreservesKindAndDiagnosticCause(t *testing.T) {
	cause := &pq.Error{Code: "40001", Message: "private database detail"}
	err := NewPublicError(ErrConflict, "settings changed; retry the request", cause)
	if !errors.Is(err, ErrConflict) || !errors.Is(err, cause) {
		t.Fatal("business classification or diagnostic cause was lost")
	}
	var diagnostic *pq.Error
	if !errors.As(err, &diagnostic) || diagnostic != cause {
		t.Fatal("typed diagnostic cause was lost")
	}
	if got := PublicMessage(fmt.Errorf("private outer context: %w", err)); got != "settings changed; retry the request" {
		t.Fatalf("unsafe message: %q", got)
	}
}

func TestPublicMessageDoesNotExposeWrappedSentinelDetails(t *testing.T) {
	for _, kind := range []error{ErrConflict, ErrNotFound, ErrValidation, ErrUnauthorized, ErrServiceUnavailable} {
		wrapped := fmt.Errorf("SQL private_detail password=fixture: %w", kind)
		if PublicMessage(wrapped) != PublicMessage(kind) {
			t.Fatalf("unreviewed details were exposed for %v", kind)
		}
	}
	if got := PublicMessage(errors.New("SQL private_detail")); got != "failed to process request; try again later" {
		t.Fatalf("unknown diagnostic was exposed: %q", got)
	}
	if operationError(nil, "operation failed") != nil {
		t.Fatal("successful operation became an error")
	}
}

func TestMonitorCreationErrorsHaveSpecificSafeReasons(t *testing.T) {
	for _, tc := range []struct {
		cause error
		kind  error
		want  string
	}{
		{repository.ErrEndpointTypeConflict, ErrConflict, "this URL is already configured with a different type"},
		{repository.ErrEndpointPageConflict, ErrConflict, "this URL is already configured with a different browser_page_url"},
		{repository.ErrEndpointJSONSourceConflict, ErrConflict, "this URL is already configured with a different json_source"},
		{repository.ErrTargetHostnameUnavailable, ErrTargetHostnameUnavailable, "hostname is not configured, inactive, or blocked"},
		{&pq.Error{Code: "40001", Message: "private relation"}, ErrConflict, "resource settings changed concurrently; retry the request"},
		{&pq.Error{Code: "XX000", Message: "SELECT private_column"}, nil, "could not create monitor; try again later"},
	} {
		err := monitorCreationError(tc.cause)
		if PublicMessage(err) != tc.want || !errors.Is(err, tc.cause) || (tc.kind != nil && !errors.Is(err, tc.kind)) {
			t.Fatalf("incorrect mapping for %T: %v", tc.cause, err)
		}
	}
	if monitorCreationError(nil) != nil {
		t.Fatal("successful creation became an error")
	}
}
