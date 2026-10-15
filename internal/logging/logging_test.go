package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestErrorWritesStandardFieldsAndRedactsSensitivePayload(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output)
	ctx := WithRequestID(context.Background(), "request-123")

	Error(ctx, logger, "failed to get user", map[string]any{
		"email":    "alice@example.com",
		"password": "plain-text-password",
		"nested": map[string]any{
			"access_token": "secret-token",
		},
		"items": []any{map[string]any{"clientSecret": "nested-secret"}},
	}, errors.New("database timeout"))

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log entry: %v", err)
	}

	for _, field := range []string{"time", "level", "msg", "request_id", "payload", "error"} {
		if _, ok := entry[field]; !ok {
			t.Errorf("log entry does not contain %q: %#v", field, entry)
		}
	}
	if entry["level"] != "ERROR" || entry["msg"] != "failed to get user" || entry["request_id"] != "request-123" {
		t.Errorf("unexpected standard fields: %#v", entry)
	}
	if entry["error"] != "database timeout" {
		t.Errorf("error = %#v, want database timeout", entry["error"])
	}

	payload := entry["payload"].(map[string]any)
	if payload["password"] != RedactedValue {
		t.Errorf("password was not redacted: %#v", payload)
	}
	nested := payload["nested"].(map[string]any)
	if nested["access_token"] != RedactedValue {
		t.Errorf("access token was not redacted: %#v", nested)
	}
	items := payload["items"].([]any)
	if items[0].(map[string]any)["clientSecret"] != RedactedValue {
		t.Errorf("secret in an array was not redacted: %#v", items)
	}
	if payload["email"] != "alice@example.com" {
		t.Errorf("non-sensitive field was changed: %#v", payload)
	}
}
