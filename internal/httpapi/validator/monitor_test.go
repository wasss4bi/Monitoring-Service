package validator

import (
	"strings"
	"testing"
)

func TestCanonicalizeMonitorURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"normalizes host, port, path and query", "https://EXAMPLE.com:443/a///?b=2&a=z&a=a#fragment", "https://example.com/a?a=a&a=z&b=2"},
		{"keeps root", "https://Example.COM/", "https://example.com/"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, fields := CanonicalizeMonitorURL(test.url)
			if len(fields) != 0 {
				t.Fatalf("fields = %#v", fields)
			}
			if got != test.want {
				t.Fatalf("url = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCanonicalizeMonitorURLRejectsNonDefaultPort(t *testing.T) {
	if _, fields := CanonicalizeMonitorURL("https://example.com:8443/value"); len(fields) == 0 {
		t.Fatal("fields are empty")
	}
}

func TestCanonicalizeMonitorURLRejectsUnsafeInputs(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://127.0.0.1/", "https://user@example.com/"} {
		if _, fields := CanonicalizeMonitorURL(raw); len(fields) == 0 {
			t.Errorf("%q was accepted", raw)
		}
	}
}

func TestNormalizeMonitorRuleInput(t *testing.T) {
	_, fields := NormalizeMonitorRuleInput(RawMonitorRuleInput{Description: "Price condition", Path: "product.prices[0].value", Operator: "<=", Value: 1})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	_, fields = NormalizeMonitorRuleInput(RawMonitorRuleInput{Description: "Price condition", Path: "bad.key-name", Operator: "~", Value: 1})
	if len(fields) == 0 {
		t.Fatal("invalid rule was accepted")
	}
}

func TestNormalizeMonitorName(t *testing.T) {
	name, fields := NormalizeMonitorName("  Apple Watch  ")
	if len(fields) != 0 || name != "Apple Watch" {
		t.Fatalf("name=%q fields=%v", name, fields)
	}

	for _, value := range []string{"", " \t\n", strings.Repeat("x", 256)} {
		if _, fields := NormalizeMonitorName(value); len(fields) == 0 {
			t.Errorf("%q was accepted", value)
		}
	}
}

func TestNormalizeHTMLMonitorCreateInputDefersXPathSyntaxValidation(t *testing.T) {
	url, rules, fields := NormalizeHTMLMonitorCreateInput("https://example.com/product", []RawMonitorRuleInput{{
		Description: "Price condition",
		Path:        "//*[",
		Operator:    "<=",
		Value:       130000,
	}})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if url != "https://example.com/product" || len(rules) != 1 || rules[0].JSONPath != "//*[" {
		t.Fatalf("normalized input = url=%q rules=%#v", url, rules)
	}
}
func TestNormalizeHTMLMonitorCreateInputAcceptsCSSSelector(t *testing.T) {
	url, rules, fields := NormalizeHTMLMonitorCreateInput("https://example.com/product", []RawMonitorRuleInput{{
		Description: "Price condition",
		Path:        " span.price.current ",
		Operator:    "<=",
		Value:       130000,
	}})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if url != "https://example.com/product" || len(rules) != 1 || rules[0].JSONPath != "span.price.current" {
		t.Fatalf("normalized input = url=%q rules=%#v", url, rules)
	}
}
