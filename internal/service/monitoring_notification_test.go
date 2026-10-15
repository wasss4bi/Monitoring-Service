package service

import (
	"errors"
	"net/http"
	"testing"

	"monitoring_service/internal/sender"
)

func TestPermanentNotificationHTTPStatuses(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusConflict, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		if !isPermanentNotificationError(&sender.HTTPStatusError{StatusCode: status}) {
			t.Fatalf("HTTP %d must be permanent", status)
		}
	}
	if isPermanentNotificationError(&sender.HTTPStatusError{StatusCode: http.StatusTooManyRequests}) {
		t.Fatal("HTTP 429 must remain retryable")
	}
	if isPermanentNotificationError(errors.New("network error")) {
		t.Fatal("network error must remain retryable")
	}
}
