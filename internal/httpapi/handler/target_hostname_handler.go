package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"

	"monitoring_service/internal/domain"
	"monitoring_service/internal/httpapi/validator"
	"monitoring_service/internal/service"
)

type TargetHostnameHandler struct {
	targets *service.TargetHostnameService
}

func NewTargetHostnameHandler(targets *service.TargetHostnameService) *TargetHostnameHandler {
	return &TargetHostnameHandler{targets: targets}
}

func (h *TargetHostnameHandler) TargetHostnames(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		targets, err := h.targets.List(r.Context())
		if err != nil {
			writeServiceError(w, err)
			return
		}
		response := make([]targetHostnameResponse, 0, len(targets))
		for _, target := range targets {
			response = append(response, toTargetHostnameResponse(target))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": response})
	case http.MethodPost:
		var request createTargetHostnameRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		rules, ruleErrors := validator.NormalizeBootstrapRules(request.BootstrapRules)
		if len(ruleErrors) > 0 {
			writeValidationError(w, http.StatusBadRequest, ruleErrors)
			return
		}
		input, fields := validator.NormalizeTargetHostname(request.Hostname, request.BootstrapURL, request.LimitPerMinute)
		if len(fields) > 0 {
			writeValidationError(w, http.StatusBadRequest, fields)
			return
		}
		input.IsActive = request.IsActive == nil || *request.IsActive
		input.BootstrapRules = rules
		target, err := h.targets.Create(r.Context(), input)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toTargetHostnameResponse(target))
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *TargetHostnameHandler) TargetHostnameByID(w http.ResponseWriter, r *http.Request) {
	id, ok := targetHostnameID(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		target, err := h.targets.Get(r.Context(), id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toTargetHostnameResponse(target))
	case http.MethodPatch:
		h.update(w, r, id)
	case http.MethodDelete:
		if err := h.targets.Delete(r.Context(), id); err != nil {
			writeServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *TargetHostnameHandler) ResetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	id, ok := targetHostnameID(w, r)
	if !ok {
		return
	}
	target, err := h.targets.ResetSession(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTargetHostnameResponse(target))
}

func (h *TargetHostnameHandler) update(w http.ResponseWriter, r *http.Request, id int64) {
	var request updateTargetHostnameRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.BootstrapURL) == 0 && request.LimitPerMinute == nil && request.IsActive == nil && len(request.BootstrapRules) == 0 {
		writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"body": {"at least one field must be provided"}})
		return
	}
	current, err := h.targets.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	bootstrap := current.BootstrapURL
	clearBootstrap := false
	if len(request.BootstrapURL) > 0 {
		if bytes.Equal(bytes.TrimSpace(request.BootstrapURL), []byte("null")) {
			bootstrap = nil
			clearBootstrap = true
		} else {
			var value string
			if err := json.Unmarshal(request.BootstrapURL, &value); err != nil {
				writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"bootstrap_url": {"bootstrap_url must be a string or null"}})
				return
			}
			bootstrap = &value
		}
	}
	limit := current.LimitPerMinute
	if request.LimitPerMinute != nil {
		limit = *request.LimitPerMinute
	}
	normalized, fields := validator.NormalizeTargetHostname(current.Hostname, bootstrap, limit)
	if len(fields) > 0 {
		writeValidationError(w, http.StatusBadRequest, fields)
		return
	}
	input := domain.UpdateTargetHostnameInput{
		ClearBootstrap: clearBootstrap, LimitPerMinute: request.LimitPerMinute, IsActive: request.IsActive,
	}
	if len(request.BootstrapRules) > 0 {
		var fields validator.FieldErrors
		input.BootstrapRules, fields = validator.NormalizeBootstrapRules(request.BootstrapRules)
		if len(fields) > 0 {
			writeValidationError(w, http.StatusBadRequest, fields)
			return
		}
	}
	if len(request.BootstrapURL) > 0 && !clearBootstrap {
		input.BootstrapURL = normalized.BootstrapURL
	}
	target, err := h.targets.Update(r.Context(), id, input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTargetHostnameResponse(target))
}

func targetHostnameID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"id": {"id must be a positive integer"}})
		return 0, false
	}
	return id, true
}
