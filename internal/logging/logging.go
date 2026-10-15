package logging

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
)

const RedactedValue = "[REDACTED]"

type requestIDKey struct{}

func New(output io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

func RequestID(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}

func Info(ctx context.Context, logger *slog.Logger, message string, payload any) {
	write(ctx, logger, slog.LevelInfo, message, payload, nil)
}

func Error(ctx context.Context, logger *slog.Logger, message string, payload any, err error) {
	write(ctx, logger, slog.LevelError, message, payload, err)
}

func write(ctx context.Context, logger *slog.Logger, level slog.Level, message string, payload any, err error) {
	if logger == nil {
		logger = slog.Default()
	}

	errorMessage := ""
	if err != nil {
		errorMessage = err.Error()
	}

	logger.LogAttrs(ctx, level, message,
		slog.String("request_id", RequestID(ctx)),
		slog.Any("payload", sanitizePayload(payload)),
		slog.String("error", errorMessage),
	)
}

func sanitizePayload(payload any) any {
	if payload == nil {
		return map[string]any{}
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return map[string]any{"value": "[UNSERIALIZABLE]"}
	}

	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return map[string]any{"value": "[UNSERIALIZABLE]"}
	}

	return redact(value)
}

func redact(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if isSensitiveKey(key) {
				typed[key] = RedactedValue
				continue
			}
			typed[key] = redact(item)
		}
	case []any:
		for index, item := range typed {
			typed[index] = redact(item)
		}
	}

	return value
}

func isSensitiveKey(key string) bool {
	normalized := strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(strings.ToLower(key))
	compact := strings.ReplaceAll(normalized, "_", "")
	if strings.Contains(compact, "password") || strings.Contains(compact, "passwd") {
		return true
	}

	switch compact {
	case "secret", "clientsecret", "token", "accesstoken", "refreshtoken", "apikey", "authorization", "cookie", "setcookie", "privatekey":
		return true
	default:
		return false
	}
}
