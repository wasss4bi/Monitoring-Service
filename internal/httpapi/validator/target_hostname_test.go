package validator

import (
	"testing"
)

func TestNormalizeTargetHostname(t *testing.T) {
	bootstrap := "https://API.Example.com/bootstrap#fragment"
	input, fields := NormalizeTargetHostname(" API.Example.com. ", &bootstrap, 30)
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if input.Hostname != "api.example.com" || input.BootstrapURL == nil || *input.BootstrapURL != "https://api.example.com/bootstrap" {
		value := "<nil>"
		if input.BootstrapURL != nil {
			value = *input.BootstrapURL
		}
		t.Fatalf("input = %#v bootstrap=%q", input, value)
	}
}

func TestNormalizeTargetHostnameRejectsNonDefaultPort(t *testing.T) {
	bootstrap := "https://api.example.com:8443/bootstrap"
	if _, fields := NormalizeTargetHostname("api.example.com", &bootstrap, 30); len(fields) == 0 {
		t.Fatal("fields are empty")
	}
}

func TestNormalizeTargetHostnameRejectsUnsafeOrInconsistentConfiguration(t *testing.T) {
	foreign := "https://other.example.com/bootstrap"
	for _, testCase := range []struct {
		name      string
		hostname  string
		bootstrap *string
		limit     int
	}{
		{name: "ip", hostname: "127.0.0.1", limit: 1},
		{name: "foreign bootstrap", hostname: "api.example.com", bootstrap: &foreign, limit: 1},
		{name: "invalid limit", hostname: "api.example.com", limit: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, fields := NormalizeTargetHostname(testCase.hostname, testCase.bootstrap, testCase.limit); len(fields) == 0 {
				t.Fatal("fields are empty")
			}
		})
	}
}
