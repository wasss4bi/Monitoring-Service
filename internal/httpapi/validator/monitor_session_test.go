package validator

import (
	"encoding/json"
	"testing"
)

func TestMonitorSession(t *testing.T) {
	good := json.RawMessage(`{"version":1,"userAgent":"test","cookieJar":{"cookies":[{"domain":"example.com"}]},"browserProfile":"another-monitor"}`)
	mode, raw, fields := NormalizeMonitorSession("", good, "https://www.example.com/value")
	if len(fields) > 0 || mode != "provided" {
		t.Fatalf("mode=%s fields=%v", mode, fields)
	}
	var exported map[string]any
	_ = json.Unmarshal(raw, &exported)
	if _, ok := exported["browserProfile"]; ok {
		t.Fatal("client controlled profile identifier")
	}
	for _, bad := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`{"version":1,"userAgent":"test","cookieJar":{"cookies":[{"domain":"other.com"}]}}`)} {
		if _, _, fields := NormalizeMonitorSession("provided", bad, "https://www.example.com/value"); len(fields) == 0 {
			t.Fatal("invalid session accepted")
		}
	}
	if _, _, f := NormalizeMonitorSession("managed", good, "https://www.example.com/"); len(f) == 0 {
		t.Fatal("managed session accepted supplied credentials")
	}
	if mode, raw, f := NormalizeMonitorSession("", nil, "https://www.example.com"); mode != "managed" || raw != nil || len(f) > 0 {
		t.Fatal("managed default")
	}
}
