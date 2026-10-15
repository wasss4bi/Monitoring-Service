package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBootstrapPolicyRejectsMalformedRulesBeforeStorage(t *testing.T) {
	h := NewTargetHostnameHandler(nil)
	for _, raw := range []string{`null`, `{}`, `[{"origin":"http://example.com","path_prefix":"/","methods":["POST"]}]`, `[{"origin":"https://127.0.0.1","path_prefix":"/","methods":["POST"]}]`, `[{"origin":"https://example.com","path_prefix":"/","methods":["DELETE"]}]`} {
		req := httptest.NewRequest(http.MethodPost, "/target-hostnames", strings.NewReader(`{"hostname":"example.com","limit_per_minute":60,"bootstrap_rules":`+raw+`}`))
		w := httptest.NewRecorder()
		h.TargetHostnames(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
}
