package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"monitoring_service/internal/domain"
	"monitoring_service/internal/httpapi/validator"
	"monitoring_service/internal/service"
)

const defaultMonitorPageSize = 20
const maxMonitorPageSize = 100

type MonitorHandler struct {
	monitors    *service.MonitorService
	idempotency *idempotencyExecutor
}

func NewMonitorHandler(monitors *service.MonitorService, idempotency *service.IdempotencyService) *MonitorHandler {
	return &MonitorHandler{monitors: monitors, idempotency: newIdempotencyExecutor([]*service.IdempotencyService{idempotency})}
}

func (h *MonitorHandler) Monitors(w http.ResponseWriter, r *http.Request) {
	userID, ok := authenticatedUserID(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	switch r.Method {
	case http.MethodPost:
		h.idempotency.runWithRecovery(w, r, &userID, nil, func(ctx context.Context, entry domain.IdempotencyEntry) (int, []byte, error) {
			monitor, err := h.monitors.Get(ctx, userID, *entry.MonitorID)
			if err != nil {
				if errors.Is(err, service.ErrNotFound) {
					w := newIdempotencyResponseWriter()
					writeServiceError(w, err)
					return w.status, w.body.Bytes(), nil
				}
				return 0, nil, err
			}
			body, err := json.Marshal(toMonitorResponse(monitor))
			return http.StatusCreated, body, err
		}, h.create)
	case http.MethodGet:
		h.list(w, r, userID)
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *MonitorHandler) MonitorByID(w http.ResponseWriter, r *http.Request) {
	userID, ok := authenticatedUserID(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	id, ok := monitorID(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.get(w, r, userID, id)
	case http.MethodPatch:
		h.update(w, r, userID, id)
	case http.MethodDelete:
		h.idempotency.run(w, r, &userID, func(w http.ResponseWriter, r *http.Request) {
			if err := h.monitors.Delete(r.Context(), userID, id); err != nil {
				writeServiceError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *MonitorHandler) AddRule(w http.ResponseWriter, r *http.Request) {
	userID, ok := authenticatedUserID(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	id, ok := monitorID(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	h.idempotency.run(w, r, &userID, func(w http.ResponseWriter, r *http.Request) {
		var request monitorRuleRequest
		if !decodeJSON(w, r, &request) {
			return
		}

		if request.Value == nil {
			writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"value": {"value is required"}})
			return
		}
		rawRule := validator.RawMonitorRuleInput{Description: request.Description, Path: request.Path, Operator: request.Operator, Value: *request.Value}
		rule, fields := validator.NormalizeGenericMonitorRuleInput(rawRule)
		if len(fields) > 0 {
			writeValidationError(w, http.StatusBadRequest, fields)
			return
		}

		current, err := h.monitors.Get(r.Context(), userID, id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		if fields := validator.NormalizeRuleOptions(request.Source, request.NumberFormat, current.PathType); len(fields) > 0 {
			writeValidationError(w, http.StatusBadRequest, fields)
			return
		}
		rule.Source = request.Source
		rule.NumberFormat = request.NumberFormat
		created, err := h.monitors.AddRule(r.Context(), userID, id, rule)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toMonitorRuleResponse(created))
	})
}

func (h *MonitorHandler) Stop(w http.ResponseWriter, r *http.Request)   { h.changeStatus(w, r, true) }
func (h *MonitorHandler) Resume(w http.ResponseWriter, r *http.Request) { h.changeStatus(w, r, false) }

func (h *MonitorHandler) changeStatus(w http.ResponseWriter, r *http.Request, stop bool) {
	userID, ok := authenticatedUserID(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	id, ok := monitorID(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	h.idempotency.run(w, r, &userID, func(w http.ResponseWriter, r *http.Request) {
		var err error
		if stop {
			err = h.monitors.Stop(r.Context(), userID, id)
		} else {
			err = h.monitors.Resume(r.Context(), userID, id)
		}
		if err != nil {
			writeServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *MonitorHandler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := authenticatedUserID(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	var request createMonitorRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	rules := make([]validator.RawMonitorRuleInput, 0, len(request.Rules))
	for i, rule := range request.Rules {
		if rule.Value == nil {
			writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"rules[" + strconv.Itoa(i) + "].value": {"value is required"}})
			return
		}
		rules = append(rules, validator.RawMonitorRuleInput{Description: rule.Description, Path: rule.Path, Operator: rule.Operator, Value: *rule.Value})
	}
	name, nameFields := validator.NormalizeMonitorName(request.Name)
	if len(nameFields) > 0 {
		writeValidationError(w, http.StatusBadRequest, nameFields)
		return
	}
	endpointType, typeFields := validator.NormalizeMonitorType(request.Type)
	if len(typeFields) > 0 {
		writeValidationError(w, http.StatusBadRequest, typeFields)
		return
	}
	pathType, pathTypeFields := validator.NormalizeMonitorPathType(endpointType, request.PathType)
	if len(pathTypeFields) > 0 {
		writeValidationError(w, http.StatusBadRequest, pathTypeFields)
		return
	}
	// Custom upstream headers are disabled at the API boundary.
	var headers map[string]string
	rulesCountToFinish, rulesCountFields := validator.NormalizeRulesCountToFinish(request.RulesCountToFinish, len(rules))
	if len(rulesCountFields) > 0 {
		writeValidationError(w, http.StatusBadRequest, rulesCountFields)
		return
	}
	var url string
	var normalizedRules []domain.CreateMonitorRuleInput
	var fields validator.FieldErrors
	if endpointType == domain.EndpointTypeHTML {
		url, normalizedRules, fields = validator.NormalizeHTMLMonitorCreateInput(request.URL, rules)
	} else {
		url, normalizedRules, fields = validator.NormalizeMonitorCreateInput(request.URL, rules)
	}
	if len(fields) > 0 {
		writeValidationError(w, http.StatusBadRequest, fields)
		return
	}
	pageURL, pageFields := validator.NormalizeMonitorPageURL(request.PageURL, url)
	if len(pageFields) > 0 {
		writeValidationError(w, http.StatusBadRequest, pageFields)
		return
	}
	browserPageURL, browserFields := validator.NormalizeBrowserPageURL(request.BrowserPageURL, url, endpointType, headers)
	if len(browserFields) > 0 {
		writeValidationError(w, http.StatusBadRequest, browserFields)
		return
	}
	if request.JSONSource != nil && browserPageURL != "" {
		writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"browser_page_url": {"browser_page_url cannot be combined with json_source"}})
		return
	}
	jsonSource, sourceFields := validator.NormalizeJSONSource(request.JSONSource, endpointType, pageURL)
	if len(sourceFields) > 0 {
		writeValidationError(w, http.StatusBadRequest, sourceFields)
		return
	}

	mode, session, sessionFields := validator.NormalizeMonitorSession(request.SessionMode, request.Session, url)
	if len(sessionFields) > 0 {
		writeValidationError(w, http.StatusBadRequest, sessionFields)
		return
	}
	for i, rule := range request.Rules {
		if fields := validator.NormalizeRuleOptions(rule.Source, rule.NumberFormat, pathType); len(fields) > 0 {
			prefixed := validator.FieldErrors{}
			for key, messages := range fields {
				prefixed["rules["+strconv.Itoa(i)+"]."+key] = messages
			}
			writeValidationError(w, http.StatusBadRequest, prefixed)
			return
		}
		normalizedRules[i].Source = rule.Source
		normalizedRules[i].NumberFormat = rule.NumberFormat
	}
	entry, _ := r.Context().Value(idempotencyContextKey{}).(domain.IdempotencyEntry)
	monitor, err := h.monitors.Create(r.Context(), domain.CreateMonitorInput{IdempotencyKey: entry.Key, IdempotencyHash: entry.RequestHash, BrowserPageURL: browserPageURL, SessionMode: mode, Session: session, UserID: userID, JSONSource: jsonSource, PageURL: pageURL, Name: name, CanonicalURL: url, EndpointType: endpointType, PathType: pathType, RulesCountToFinish: rulesCountToFinish, Rules: normalizedRules})
	if err != nil {
		if errors.Is(err, service.ErrTargetHostnameUnavailable) {
			writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"url": {service.PublicMessage(err)}})
			return
		}
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMonitorResponse(monitor))
}

func (h *MonitorHandler) update(w http.ResponseWriter, r *http.Request, userID, id int64) {
	var request updateMonitorRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	if request.SessionMode != "" || len(request.Session) > 0 {
		if request.Name != nil || request.RulesCountToFinish != nil {
			writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"body": {"session replacement cannot be combined with other updates"}})
			return
		}
		current, err := h.monitors.Get(r.Context(), userID, id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		mode, session, fields := validator.NormalizeMonitorSession(request.SessionMode, request.Session, current.URL)
		if len(fields) > 0 {
			writeValidationError(w, http.StatusBadRequest, fields)
			return
		}
		updated, err := h.monitors.ReplaceSession(r.Context(), userID, id, mode, session)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toMonitorResponse(updated))
		return
	}
	if (request.Name == nil && request.RulesCountToFinish == nil) || (request.Name != nil && request.RulesCountToFinish != nil) {
		writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"body": {"exactly one of name or rules_count_to_finish must be provided"}})
		return
	}
	if request.Name != nil {
		name, fields := validator.NormalizeMonitorName(*request.Name)
		if len(fields) > 0 {
			writeValidationError(w, http.StatusBadRequest, fields)
			return
		}
		monitor, err := h.monitors.UpdateName(r.Context(), userID, id, name)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toMonitorResponse(monitor))
		return
	}
	current, err := h.monitors.Get(r.Context(), userID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	rulesCountToFinish, fields := validator.NormalizeRulesCountToFinish(request.RulesCountToFinish, len(current.Rules))
	if len(fields) > 0 {
		writeValidationError(w, http.StatusBadRequest, fields)
		return
	}
	monitor, err := h.monitors.UpdateRulesCountToFinish(r.Context(), userID, id, rulesCountToFinish)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMonitorResponse(monitor))
}

func (h *MonitorHandler) get(w http.ResponseWriter, r *http.Request, userID, id int64) {
	monitor, err := h.monitors.Get(r.Context(), userID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMonitorResponse(monitor))
}

func (h *MonitorHandler) list(w http.ResponseWriter, r *http.Request, userID int64) {
	limit := defaultMonitorPageSize
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		value, err := strconv.Atoi(rawLimit)
		if err != nil || value < 1 || value > maxMonitorPageSize {
			writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"limit": {"limit must be between 1 and 100"}})
			return
		}
		limit = value
	}
	cursor, err := decodeMonitorCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"cursor": {"cursor is invalid"}})
		return
	}
	monitors, err := h.monitors.List(r.Context(), userID, limit+1, cursor)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	count := len(monitors)
	if count > limit {
		count = limit
	}
	response := monitorListResponse{Items: make([]monitorResponse, 0, count)}
	for _, monitor := range monitors[:count] {
		response.Items = append(response.Items, toMonitorResponse(monitor))
	}
	if len(monitors) > limit {
		response.NextCursor = encodeMonitorCursor(monitors[limit-1])
	}
	writeJSON(w, http.StatusOK, response)
}

type monitorCursorPayload struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

func encodeMonitorCursor(monitor domain.Monitor) string {
	body, _ := json.Marshal(monitorCursorPayload{CreatedAt: monitor.CreatedAt, ID: monitor.ID})
	return base64.RawURLEncoding.EncodeToString(body)
}
func decodeMonitorCursor(raw string) (*domain.MonitorCursor, error) {
	if raw == "" {
		return nil, nil
	}
	body, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	var payload monitorCursorPayload
	if err = json.Unmarshal(body, &payload); err != nil || payload.ID <= 0 || payload.CreatedAt.IsZero() {
		return nil, http.ErrNoCookie
	}
	return &domain.MonitorCursor{CreatedAt: payload.CreatedAt, ID: payload.ID}, nil
}
func monitorID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid monitor id")
		return 0, false
	}
	return id, true
}
