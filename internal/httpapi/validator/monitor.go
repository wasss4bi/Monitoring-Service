package validator

import (
	"math"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"monitoring_service/internal/domain"
)

const maxMonitorRules = 3

var jsonPathPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*|\[[0-9]+\])*$`)

type RawMonitorRuleInput struct {
	Description string
	Path        string
	Operator    string
	Value       float64
}

func NormalizeMonitorName(raw string) (string, FieldErrors) {
	name := strings.TrimSpace(raw)
	if length := utf8.RuneCountInString(name); length < 1 || length > 255 {
		return "", FieldErrors{"name": {"name must contain from 1 to 255 characters"}}
	}
	return name, nil
}

func NormalizeMonitorType(raw string) (string, FieldErrors) {
	typeName := strings.ToLower(strings.TrimSpace(raw))
	switch typeName {
	case domain.EndpointTypeJSON, domain.EndpointTypeHTML:
		return typeName, nil
	default:
		return "", FieldErrors{"type": {"type must be json or html"}}
	}
}

func NormalizeMonitorPathType(endpointType, raw string) (string, FieldErrors) {
	pathType := strings.ToLower(strings.TrimSpace(raw))
	valid := (endpointType == domain.EndpointTypeJSON && (pathType == "json" || pathType == "regex")) ||
		(endpointType == domain.EndpointTypeHTML && (pathType == "xpath" || pathType == "selector"))
	if !valid {
		return "", FieldErrors{"path_type": {"path_type is not supported for the monitor type"}}
	}
	return pathType, nil
}

func NormalizeRulesCountToFinish(raw *int, ruleCount int) (int, FieldErrors) {
	value := ruleCount
	if raw != nil {
		value = *raw
	}
	if value < 1 || value > ruleCount {
		return 0, FieldErrors{"rules_count_to_finish": {"rules_count_to_finish must be between 1 and the number of rules"}}
	}
	return value, nil
}

func NormalizeGenericMonitorRuleInput(input RawMonitorRuleInput) (domain.CreateMonitorRuleInput, FieldErrors) {
	path := strings.TrimSpace(input.Path)
	var fields FieldErrors
	description := strings.TrimSpace(input.Description)
	if length := utf8.RuneCountInString(description); length < 1 || length > 255 {
		fields = addFieldError(fields, "description", "description must contain from 1 to 255 characters")
	}
	if strings.ContainsRune(description, 0) {
		fields = addFieldError(fields, "description", "description must not contain NUL")
	}
	if path == "" {
		fields = addFieldError(fields, "path", "path must be non-empty")
	}
	operator := strings.TrimSpace(input.Operator)
	switch operator {
	case "<", ">", "=", "<=", ">=", "!=":
	default:
		fields = addFieldError(fields, "operator", "operator must be one of <, >, =, <=, >=, !=")
	}
	if math.IsNaN(input.Value) || math.IsInf(input.Value, 0) {
		fields = addFieldError(fields, "value", "value must be a finite number")
	}
	if len(fields) > 0 {
		return domain.CreateMonitorRuleInput{}, fields
	}
	return domain.CreateMonitorRuleInput{Description: description, JSONPath: path, ComparisonOperator: operator, ExpectedValue: input.Value}, nil
}
func NormalizeHTMLMonitorCreateInput(rawURL string, rules []RawMonitorRuleInput) (string, []domain.CreateMonitorRuleInput, FieldErrors) {
	canonicalURL, fields := CanonicalizeMonitorURL(rawURL)
	if len(rules) == 0 || len(rules) > maxMonitorRules {
		fields = addFieldError(fields, "rules", "rules must contain from 1 to 3 items")
	}
	normalizedRules := make([]domain.CreateMonitorRuleInput, 0, len(rules))
	for index, rule := range rules {
		path := strings.TrimSpace(rule.Path)
		if path == "" {
			fields = addFieldError(fields, "rules["+strconv.Itoa(index)+"].path", "path must be a non-empty CSS selector or XPath")
		}
		normalized, ruleFields := NormalizeHTMLMonitorRuleInput(rule)
		for field, messages := range ruleFields {
			if field == "path" {
				continue
			}
			for _, message := range messages {
				fields = addFieldError(fields, "rules["+strconv.Itoa(index)+"]."+field, message)
			}
		}
		normalized.JSONPath = path
		normalizedRules = append(normalizedRules, normalized)
	}
	if len(fields) > 0 {
		return "", nil, fields
	}
	return canonicalURL, normalizedRules, nil
}

func NormalizeHTMLMonitorRuleInput(input RawMonitorRuleInput) (domain.CreateMonitorRuleInput, FieldErrors) {
	path := strings.TrimSpace(input.Path)
	var fields FieldErrors
	if path == "" {
		fields = addFieldError(fields, "path", "path must be a non-empty CSS selector or XPath")
	}
	normalized, ruleFields := NormalizeMonitorRuleInput(RawMonitorRuleInput{Description: input.Description, Path: "value", Operator: input.Operator, Value: input.Value})
	for field, messages := range ruleFields {
		if field == "path" {
			continue
		}
		for _, message := range messages {
			fields = addFieldError(fields, field, message)
		}
	}
	if len(fields) > 0 {
		return domain.CreateMonitorRuleInput{}, fields
	}
	normalized.JSONPath = path
	return normalized, nil
}

func NormalizeMonitorCreateInput(rawURL string, rules []RawMonitorRuleInput) (string, []domain.CreateMonitorRuleInput, FieldErrors) {
	canonicalURL, fields := CanonicalizeMonitorURL(rawURL)
	if len(rules) == 0 || len(rules) > maxMonitorRules {
		fields = addFieldError(fields, "rules", "rules must contain from 1 to 3 items")
	}
	normalizedRules := make([]domain.CreateMonitorRuleInput, 0, len(rules))
	for index, rule := range rules {
		normalized, ruleFields := NormalizeMonitorRuleInput(rule)
		for field, messages := range ruleFields {
			for _, message := range messages {
				fields = addFieldError(fields, "rules["+strconv.Itoa(index)+"]."+field, message)
			}
		}
		normalizedRules = append(normalizedRules, normalized)
	}
	if len(fields) > 0 {
		return "", nil, fields
	}
	return canonicalURL, normalizedRules, nil
}

func NormalizeMonitorRuleInput(input RawMonitorRuleInput) (domain.CreateMonitorRuleInput, FieldErrors) {
	return NormalizeGenericMonitorRuleInput(input)
}

func CanonicalizeMonitorURL(raw string) (string, FieldErrors) {
	raw = strings.TrimSpace(raw)
	if before, _, found := strings.Cut(raw, "#"); found {
		raw = before
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", FieldErrors{"url": {"url must be an absolute HTTPS URL without userinfo"}}
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || net.ParseIP(host) != nil {
		return "", FieldErrors{"url": {"url host must be a domain name"}}
	}
	port := u.Port()
	if port != "" && port != "443" {
		return "", FieldErrors{"url": {"url must use the standard HTTPS port 443"}}
	}
	u.Host = host
	u.Scheme = "https"
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	} else if u.Path != "/" {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	if u.RawQuery != "" {
		parts := strings.Split(u.RawQuery, "&")
		sort.SliceStable(parts, func(i, j int) bool {
			ik, iv := queryPart(parts[i])
			jk, jv := queryPart(parts[j])
			if ik != jk {
				return ik < jk
			}
			if iv != jv {
				return iv < jv
			}
			return parts[i] < parts[j]
		})
		u.RawQuery = strings.Join(parts, "&")
	}
	return u.String(), nil
}

// The product link is monitor-owned metadata and does not select a transport.
func NormalizeMonitorPageURL(raw, endpointURL string) (string, FieldErrors) {
	if strings.TrimSpace(raw) == "" {
		return "", FieldErrors{"page_url": {"page_url is required"}}
	}
	return normalizeSourcePageURL(raw, endpointURL, "page_url")
}

// Browser source configuration remains shared by the canonical JSON endpoint.
func NormalizeBrowserPageURL(raw, endpointURL, endpointType string, headers map[string]string) (string, FieldErrors) {
	if raw == "" {
		return "", nil
	}
	if endpointType != domain.EndpointTypeJSON {
		return "", FieldErrors{"browser_page_url": {"browser_page_url is supported only for json monitors"}}
	}
	if len(headers) != 0 {
		return "", FieldErrors{"headers": {"headers must be empty in native browser mode"}}
	}
	return normalizeSourcePageURL(raw, endpointURL, "browser_page_url")
}

func normalizeSourcePageURL(raw, endpointURL, field string) (string, FieldErrors) {
	if len(raw) > 8192 || strings.Contains(raw, "#") {
		return "", FieldErrors{field: {field + " must contain at most 8192 bytes and no fragment"}}
	}
	pageURL, fields := CanonicalizeMonitorURL(raw)
	if len(fields) > 0 {
		return "", FieldErrors{field: fields["url"]}
	}
	if len(pageURL) > 8192 {
		return "", FieldErrors{field: {field + " must contain at most 8192 bytes after URL normalization"}}
	}
	page, _ := url.Parse(pageURL)
	endpoint, err := url.Parse(endpointURL)
	if err != nil || page.Host != endpoint.Host {
		return "", FieldErrors{field: {field + " must use the monitored URL hostname"}}
	}
	return pageURL, nil
}

func queryPart(value string) (string, string) {
	key, rest, found := strings.Cut(value, "=")
	if !found {
		return key, ""
	}
	return key, rest
}
