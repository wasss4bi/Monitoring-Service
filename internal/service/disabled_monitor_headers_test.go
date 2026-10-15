package service

import (
	"monitoring_service/internal/domain"
	"testing"
)

func TestDisabledMonitorHeadersAreNotStoredOrForwarded(t *testing.T) {
	stored := map[string]string{"Authorization": "invalid-legacy-ciphertext", "Cookie": "fixture", "X-Custom": "fixture"}
	headers, fingerprint, err := (&MonitorService{}).protectHeaders(stored)
	if err != nil || len(headers) != 0 || fingerprint != "" {
		t.Fatal("disabled caller headers must not be persisted")
	}
	request := makeExtractionRequest(domain.FetchJob{Headers: stored}, nil)
	if request.ProtoReflect().Descriptor().Fields().ByName("headers") != nil {
		t.Fatal("caller headers must not cross the gRPC boundary")
	}
	if len(stored) != 3 || stored["X-Custom"] != "fixture" {
		t.Fatal("disabling headers must not mutate the stored map")
	}
}
