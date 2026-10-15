package handler

import (
	"encoding/json"
	"monitoring_service/internal/domain"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpaqueMonitorSessionContract(t *testing.T) {
	body := `{"name":"private","url":"https://example.com/api","type":"json","path_type":"json","session_mode":"provided","session":{"version":1,"userAgent":"test","cookieJar":{"cookies":[{"key":"technical","value":"not-for-response","domain":"example.com"}]},"storageState":{"cookies":[],"origins":[]}},"rules":[{"path":"price","operator":"<=","value":75000,"source":{"path":"widgets","key_prefix":"price-","decode_json":true},"number_format":"money"}]}`
	request := httptest.NewRequest("POST", "/me/monitors", strings.NewReader(body))
	response := httptest.NewRecorder()
	var input createMonitorRequest
	if !decodeJSON(response, request, &input) {
		t.Fatalf("opaque export rejected: %s", response.Body.String())
	}
	if len(input.Session) == 0 || input.Rules[0].Source == nil {
		t.Fatal("session/rule options lost")
	}
	result := toMonitorResponse(domain.Monitor{ID: 1, SessionMode: "provided", SessionStatus: "ready", HasSession: true, Headers: map[string]string{"Authorization": "secret"}})
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "not-for-response") || strings.Contains(string(raw), "\"session\":") {
		t.Fatal("sensitive session included in response")
	}
	if !strings.Contains(string(raw), "\"has_session\":true") {
		t.Fatal("missing session metadata")
	}
	if strings.Contains(string(raw), "\"headers\":") {
		t.Fatal("disabled headers included in response")
	}
}

func TestMonitorRejectsDisabledCustomHeaders(t *testing.T) {
	for _, value := range []string{`{}`, `null`, `{"Authorization":"fixture"}`} {
		request := httptest.NewRequest("POST", "/me/monitors", strings.NewReader(`{"headers":`+value+`}`))
		response := httptest.NewRecorder()
		var input createMonitorRequest
		if decodeJSON(response, request, &input) || response.Code != 400 {
			t.Fatal("disabled headers field must be rejected")
		}
		var result validationErrorResponse
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Error.Fields["headers"]) == 0 {
			t.Fatal("missing headers field error")
		}
	}
}
