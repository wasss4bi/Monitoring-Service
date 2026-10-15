package handler

import (
	"errors"
	"monitoring_service/internal/service"
	"net/http"
	"strconv"
)

type ContactVerificationHandler struct {
	service *service.ContactVerificationService
}

func NewContactVerificationHandler(s *service.ContactVerificationService) *ContactVerificationHandler {
	return &ContactVerificationHandler{service: s}
}
func (h *ContactVerificationHandler) Request(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUserID(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("contact_id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, 400, "invalid contact id")
		return
	}
	if h.service == nil {
		writeError(w, 503, "contact verification is unavailable")
		return
	}
	if err = h.service.Request(r.Context(), user, id); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
func (h *ContactVerificationHandler) Verify(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUserID(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("contact_id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, 400, "invalid contact id")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if h.service == nil {
		writeError(w, 503, "contact verification is unavailable")
		return
	}
	if err = h.service.Verify(r.Context(), user, id, body.Code); err != nil {
		if errors.Is(err, service.ErrValidation) {
			writeValidationError(w, http.StatusBadRequest, map[string][]string{"code": {service.PublicMessage(err)}})
			return
		}
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
