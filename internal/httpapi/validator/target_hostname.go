package validator

import (
	"net"
	"net/url"
	"regexp"
	"strings"

	"monitoring_service/internal/domain"
)

var hostnameLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func NormalizeTargetHostname(hostname string, bootstrapURL *string, limitPerMinute int) (domain.CreateTargetHostnameInput, FieldErrors) {
	hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	var fields FieldErrors
	if !validTargetHostname(hostname) {
		fields = addFieldError(fields, "hostname", "hostname must be a valid DNS name")
	}
	if limitPerMinute < 1 || limitPerMinute > 60000 {
		fields = addFieldError(fields, "limit_per_minute", "limit_per_minute must be between 1 and 60000")
	}
	var normalizedBootstrap *string
	if bootstrapURL != nil {
		value := strings.TrimSpace(*bootstrapURL)
		if before, _, found := strings.Cut(value, "#"); found {
			value = before
		}
		u, err := url.ParseRequestURI(value)
		if err != nil || u.Scheme != "https" || u.User != nil || strings.ToLower(u.Hostname()) != hostname || (u.Port() != "" && u.Port() != "443") {
			fields = addFieldError(fields, "bootstrap_url", "bootstrap_url must be an absolute HTTPS URL for the configured hostname on port 443")
		} else {
			u.Scheme = "https"
			u.Host = hostname
			u.Fragment = ""
			value = u.String()
			normalizedBootstrap = &value
		}
	}
	if len(fields) > 0 {
		return domain.CreateTargetHostnameInput{}, fields
	}
	return domain.CreateTargetHostnameInput{
		Hostname: hostname, BootstrapURL: normalizedBootstrap, LimitPerMinute: limitPerMinute,
	}, nil
}

func validTargetHostname(value string) bool {
	if value == "" || len(value) > 253 || net.ParseIP(value) != nil || !strings.Contains(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !hostnameLabelPattern.MatchString(label) {
			return false
		}
	}
	return true
}
