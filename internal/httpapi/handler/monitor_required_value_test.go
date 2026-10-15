package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMonitorRuleRequiresValue(t *testing.T) {
	for _, tc := range []struct{ name, value string }{{"missing", ""}, {"null", `,"value":null`}} {
		for _, operation := range []string{"create", "add"} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				rule := `{"description":"price","path":"price","operator":">="` + tc.value + `}`
				body := rule
				if operation == "create" {
					body = `{"name":"test","url":"https://example.com/a","page_url":"https://example.com/p","type":"json","path_type":"json","rules":[` + rule + `]}`
				}
				r := httptest.NewRequest(http.MethodPost, "/me/monitors/1/rule", strings.NewReader(body))
				r = r.WithContext(context.WithValue(r.Context(), authenticatedUserContextKey{}, int64(1)))
				r.SetPathValue("id", "1")
				w := httptest.NewRecorder()
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Errorf("invalid rule reached monitor service: %v", recovered)
					}
				}()
				h := &MonitorHandler{}
				if operation == "create" {
					h.create(w, r)
				} else {
					h.AddRule(w, r)
				}
				if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "value") {
					t.Fatalf("expected value validation error, got %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestMonitorRuleExplicitZeroIsPresent(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"description":"price","path":"price","operator":">=","value":0}`))
	w := httptest.NewRecorder()
	var input monitorRuleRequest
	if !decodeJSON(w, r, &input) || input.Value == nil || *input.Value != 0 {
		t.Fatalf("explicit zero lost: %+v, response %s", input, w.Body.String())
	}
}
