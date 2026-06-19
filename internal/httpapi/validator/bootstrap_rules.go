package validator

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"strings"
)

func NormalizeBootstrapRules(raw json.RawMessage) (json.RawMessage, FieldErrors) {
	if len(raw) == 0 {
		return json.RawMessage("[]"), nil
	}
	var rules []struct {
		Origin     string   `json:"origin"`
		PathPrefix string   `json:"path_prefix"`
		Methods    []string `json:"methods"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	invalid := func() (json.RawMessage, FieldErrors) {
		return nil, FieldErrors{"bootstrap_rules": {"must contain up to 32 HTTPS origin/path-prefix/method rules; methods: GET, HEAD, POST, OPTIONS"}}
	}
	if decoder.Decode(&rules) != nil || rules == nil || len(rules) > 32 {
		return invalid()
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return invalid()
	}
	for _, r := range rules {
		u, err := url.ParseRequestURI(r.Origin)
		if err != nil || u.Scheme != "https" || u.User != nil || !validTargetHostname(u.Hostname()) || (u.Port() != "" && u.Port() != "443") || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !strings.HasPrefix(r.PathPrefix, "/") || len(r.PathPrefix) > 1024 || len(r.Methods) < 1 || len(r.Methods) > 8 {
			return invalid()
		}
		for _, method := range r.Methods {
			if method != "GET" && method != "HEAD" && method != "POST" && method != "OPTIONS" {
				return invalid()
			}
		}
	}
	result, err := json.Marshal(rules)
	if err != nil {
		return invalid()
	}
	return result, nil
}
