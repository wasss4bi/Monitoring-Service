package validator

import (
	"encoding/json"
	"net/url"
	"strings"

	"monitoring_service/internal/domain"
)

// NormalizeMonitorSession accepts the parser's exported v1 session. Browser
// profile identifiers are server-owned and cannot be supplied by another user.
func NormalizeMonitorSession(mode string, raw json.RawMessage, endpointURL string) (string, json.RawMessage, FieldErrors) {
	if mode == "" {
		if len(raw) > 0 && string(raw) != "null" {
			mode = "provided"
		} else {
			mode = "managed"
		}
	}
	if mode != "managed" && mode != "provided" {
		return "", nil, FieldErrors{"session_mode": {"session_mode must be managed or provided"}}
	}
	if mode == "managed" {
		if len(raw) > 0 && string(raw) != "null" {
			return "", nil, FieldErrors{"session": {"managed sessions cannot be supplied"}}
		}
		return mode, nil, nil
	}
	invalid := func() (string, json.RawMessage, FieldErrors) {
		return "", nil, FieldErrors{"session": {"session must be a valid v1 export for the monitor hostname, at most 256 KiB"}}
	}
	if len(raw) == 0 || len(raw) > 256*1024 {
		return invalid()
	}
	var s map[string]json.RawMessage
	if json.Unmarshal(raw, &s) != nil || s == nil {
		return invalid()
	}
	var version int
	var ua string
	if json.Unmarshal(s["version"], &version) != nil || version != 1 || json.Unmarshal(s["userAgent"], &ua) != nil || ua == "" || len(ua) > 1024 || strings.ContainsAny(ua, "\r\n") {
		return invalid()
	}
	u, err := url.Parse(endpointURL)
	if err != nil || u.Hostname() == "" {
		return invalid()
	}
	var jar struct {
		Cookies []struct {
			Domain string `json:"domain"`
		}
	}
	if json.Unmarshal(s["cookieJar"], &jar) != nil || jar.Cookies == nil {
		return invalid()
	}
	for _, c := range jar.Cookies {
		if !sessionCookieDomain(u.Hostname(), c.Domain) {
			return invalid()
		}
	}
	var storage struct {
		Cookies []struct {
			Domain string `json:"domain"`
		}
		Origins []struct {
			Origin string `json:"origin"`
		}
	}
	if state, ok := s["storageState"]; ok {
		if json.Unmarshal(state, &storage) != nil {
			return invalid()
		}
		for _, c := range storage.Cookies {
			if !sessionCookieDomain(u.Hostname(), c.Domain) {
				return invalid()
			}
		}
		for _, o := range storage.Origins {
			if o.Origin != u.Scheme+"://"+u.Host {
				return invalid()
			}
		}
	}
	delete(s, "browserProfile")
	normalized, err := json.Marshal(s)
	if err != nil {
		return invalid()
	}
	return mode, normalized, nil
}
func sessionCookieDomain(host, domain string) bool {
	domain = strings.ToLower(strings.TrimPrefix(domain, "."))
	// Reject unrelated hosts and public-suffix-only cookies before forwarding.
	return domain != "" && strings.Contains(domain, ".") && (host == domain || strings.HasSuffix(host, "."+domain))
}

func NormalizeRuleOptions(source *domain.RuleSource, format, pathType string) FieldErrors {
	var fields FieldErrors
	if format != "" && format != "number" && format != "money" {
		fields = addFieldError(fields, "number_format", "number_format must be number or money")
	}
	if source != nil {
		if pathType != "json" {
			fields = addFieldError(fields, "source", "source requires path_type=json")
		}
		if len(source.Path) > 1024 || !jsonPathPattern.MatchString(source.Path) {
			fields = addFieldError(fields, "source.path", "source.path must be a valid JSON path")
		}
		if source.KeyPrefix == "" || len(source.KeyPrefix) > 128 {
			fields = addFieldError(fields, "source.key_prefix", "key_prefix must contain 1 to 128 bytes")
		}
	}
	return fields
}
