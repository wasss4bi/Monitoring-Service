package validator

import (
	"regexp"
	"strings"

	"monitoring_service/internal/domain"
)

var jsonObjectKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func NormalizeJSONSource(source *domain.JSONSource, endpointType, pageURL string) (*domain.JSONSource, FieldErrors) {
	if source == nil {
		return nil, nil
	}
	var fields FieldErrors
	if endpointType != domain.EndpointTypeJSON || pageURL == "" {
		fields = addFieldError(fields, "json_source", "json_source requires type=json and page_url")
	}
	if strings.TrimSpace(source.ScriptContains) == "" || len(source.ScriptContains) > 256 {
		fields = addFieldError(fields, "json_source.script_contains", "script_contains must contain from 1 to 256 bytes")
	}
	if len(source.ObjectKey) > 128 || !jsonObjectKeyPattern.MatchString(source.ObjectKey) {
		fields = addFieldError(fields, "json_source.object_key", "object_key must be an identifier of at most 128 bytes")
	}
	if len(source.Match.Path) > 1024 || !jsonPathPattern.MatchString(source.Match.Path) {
		fields = addFieldError(fields, "json_source.match.path", "path must be a valid JSON path of at most 1024 bytes")
	}
	if source.Match.Value == "" || len(source.Match.Value) > 1024 {
		fields = addFieldError(fields, "json_source.match.value", "value must contain from 1 to 1024 bytes")
	}
	if len(fields) > 0 {
		return nil, fields
	}
	return source, nil
}
