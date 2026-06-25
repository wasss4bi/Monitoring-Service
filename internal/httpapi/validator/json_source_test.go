package validator

import (
	"monitoring_service/internal/domain"
	"testing"
)

func TestJSONSourceValidation(t *testing.T) {
	valid := domain.JSONSource{ScriptContains: "__STATE__", ObjectKey: "item", Match: domain.JSONSourceMatch{Path: "sku", Value: "one"}}
	if _, fields := NormalizeJSONSource(&valid, "json", "https://example.com/item"); len(fields) != 0 {
		t.Fatal(fields)
	}
	for _, change := range []func(*domain.JSONSource){
		func(s *domain.JSONSource) { s.ObjectKey = ".*" },
		func(s *domain.JSONSource) { s.Match.Path = "items[*]" },
		func(s *domain.JSONSource) { s.ScriptContains = "" },
		func(s *domain.JSONSource) { s.Match.Value = "" },
	} {
		value := valid
		change(&value)
		if _, fields := NormalizeJSONSource(&value, "json", "https://example.com/item"); len(fields) == 0 {
			t.Fatal("invalid source accepted")
		}
	}
	if _, fields := NormalizeJSONSource(&valid, "json", ""); len(fields) == 0 {
		t.Fatal("missing page accepted")
	}
	if _, fields := NormalizeJSONSource(&valid, "html", "https://example.com/item"); len(fields) == 0 {
		t.Fatal("html type accepted")
	}
}
