package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"monitoring_service/internal/logging"
	"monitoring_service/internal/service"
)

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer func() {
		if err := r.Body.Close(); err != nil {
			logging.Error(r.Context(), slog.Default(), "failed to close request body", nil, err)
		}
	}()

	decoder := json.NewDecoder(r.Body)
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	if fields := unknownJSONFields(raw, reflect.TypeOf(dst)); len(fields) > 0 {
		writeValidationError(w, http.StatusBadRequest, fields)
		return false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		writeValidationError(w, http.StatusBadRequest, jsonDecodeFields(err))
		return false
	}
	return true
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func unknownJSONFields(raw json.RawMessage, destination reflect.Type) map[string][]string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	fields := map[string][]string{}
	collectUnknownJSONFields(value, destination, "", fields)
	return fields
}

func collectUnknownJSONFields(value any, destination reflect.Type, prefix string, fields map[string][]string) {
	for destination.Kind() == reflect.Pointer {
		destination = destination.Elem()
	}
	switch destination.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return
		}
		known := make(map[string]reflect.Type, destination.NumField())
		for index := 0; index < destination.NumField(); index++ {
			field := destination.Field(index)
			if field.PkgPath != "" {
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" {
				name = field.Name
			}
			if name != "-" {
				known[name] = field.Type
			}
		}
		for name, child := range object {
			childType, ok := known[name]
			childPath := joinJSONFieldPath(prefix, name)
			if !ok {
				fields[childPath] = append(fields[childPath], "field is not allowed")
				continue
			}
			collectUnknownJSONFields(child, childType, childPath, fields)
		}
	case reflect.Slice, reflect.Array:
		items, ok := value.([]any)
		if !ok {
			return
		}
		for index, child := range items {
			collectUnknownJSONFields(child, destination.Elem(), fmt.Sprintf("%s[%d]", prefix, index), fields)
		}
	}
}

func joinJSONFieldPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func jsonDecodeFields(err error) map[string][]string {
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		field := jsonFieldPath(typeError.Field)
		if field == "" {
			field = "body"
		}
		return map[string][]string{field: {"must be " + jsonTypeDescription(typeError.Type)}}
	}
	return map[string][]string{"body": {"must be a JSON object matching the request schema"}}
}

func jsonTypeDescription(value reflect.Type) string {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "an integer"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "a boolean"
	case reflect.Array, reflect.Slice:
		return "an array"
	case reflect.Struct, reflect.Map:
		return "an object"
	default:
		return "a valid JSON value"
	}
}

func jsonFieldPath(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ".")
	for index, part := range parts {
		parts[index] = strings.ToLower(part[:1]) + part[1:]
	}
	return strings.Join(parts, ".")
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logging.Error(context.Background(), slog.Default(), "failed to write JSON response", nil, err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	if status == http.StatusMethodNotAllowed && message == http.StatusText(status) {
		message = "HTTP method is not supported for this resource"
	}
	err := errorBody{Code: http.StatusText(status), Message: message}
	writeJSON(w, status, errorResponse{Error: err})
}

func writeValidationError(w http.ResponseWriter, status int, fields map[string][]string) {
	err := validationErrorBody{Code: http.StatusText(status), Message: "validation error", Fields: fields}
	writeJSON(w, status, validationErrorResponse{Error: err})
}

func writeServiceError(w http.ResponseWriter, err error) {
	setRequestError(w, err)
	var rateLimitErr *service.RateLimitError
	message := service.PublicMessage(err)

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "request timed out; try again later")
	case errors.As(err, &rateLimitErr):
		retryAfter := max(int64(1), int64(math.Ceil(rateLimitErr.RetryAfter().Seconds())))
		w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
		writeError(w, http.StatusTooManyRequests, message)
	case errors.Is(err, service.ErrValidation):
		writeError(w, http.StatusBadRequest, message)
	case errors.Is(err, service.ErrNotFound):
		writeError(w, http.StatusNotFound, message)
	case errors.Is(err, service.ErrConflict):
		writeError(w, http.StatusConflict, message)
	case errors.Is(err, service.ErrServiceUnavailable):
		writeError(w, http.StatusServiceUnavailable, message)
	case errors.Is(err, service.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, message)
	default:
		writeError(w, http.StatusInternalServerError, message)
	}
}

type requestErrorRecorder interface{ SetRequestError(error) }

func setRequestError(w http.ResponseWriter, err error) {
	if recorder, ok := w.(requestErrorRecorder); ok {
		recorder.SetRequestError(err)
	}
}
