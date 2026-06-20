package handler

import (
	"errors"
	"net/http"
	"strconv"

	"monitoring_service/internal/httpapi/validator"
	"monitoring_service/internal/service"
)

type ContactHandler struct {
	contacts *service.ContactService
}

func NewContactHandler(contacts *service.ContactService) *ContactHandler {
	return &ContactHandler{contacts: contacts}
}

func (h *ContactHandler) Contacts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		contacts, err := h.contacts.List(r.Context())
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toContactResponses(contacts))
	case http.MethodPost:
		var input contactRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		contactInput, fields := validator.NormalizeContactInput(validator.RawContactInput{Name: input.Name})
		if len(fields) > 0 {
			writeValidationError(w, http.StatusBadRequest, fields)
			return
		}
		contact, err := h.contacts.Create(r.Context(), contactInput)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toContactResponse(contact))
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *ContactHandler) ContactByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	switch r.Method {
	case http.MethodGet:
		contact, err := h.contacts.Get(r.Context(), id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toContactResponse(contact))
	case http.MethodPut:
		var input contactUpdateRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		name, fields := validator.NormalizeContactName(input.Name)
		if len(fields) > 0 {
			writeValidationError(w, http.StatusBadRequest, fields)
			return
		}
		contact, err := h.contacts.Update(r.Context(), id, name)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toContactResponse(contact))
	case http.MethodDelete:
		if err := h.contacts.Delete(r.Context(), id); err != nil {
			writeServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

type UserContactHandler struct {
	contacts *service.UserContactService
}

func NewUserContactHandler(contacts *service.UserContactService) *UserContactHandler {
	return &UserContactHandler{contacts: contacts}
}

func (h *UserContactHandler) UserContacts(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authorizedUser(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		contacts, err := h.contacts.List(r.Context(), userID)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toUserContactResponses(contacts))
	case http.MethodPost:
		var input userContactRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		contactInput, fields := validator.NormalizeUserContactInput(validator.RawUserContactInput{
			ContactID: input.ContactID,
			Contact:   input.Contact,
		})
		if len(fields) > 0 {
			writeValidationError(w, http.StatusBadRequest, fields)
			return
		}
		contactInput.UserID = userID
		contact, err := h.contacts.Create(r.Context(), contactInput)
		if err != nil {
			if errors.Is(err, service.ErrContactChannelNotFound) {
				writeValidationError(w, http.StatusBadRequest, map[string][]string{"contact_id": {"notification channel was not found"}})
				return
			}
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toUserContactResponse(contact))
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *UserContactHandler) UserContactByID(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authorizedUser(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("contact_id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid contact id")
		return
	}

	switch r.Method {
	case http.MethodGet:
		contact, err := h.contacts.Get(r.Context(), userID, id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toUserContactResponse(contact))
	case http.MethodPut:
		var input userContactUpdateRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		contactInput, fields := validator.NormalizeUserContactUpdateInput(validator.RawUserContactInput{
			Contact:  input.Contact,
			IsActive: input.IsActive,
		})
		if len(fields) > 0 {
			writeValidationError(w, http.StatusBadRequest, fields)
			return
		}
		contact, err := h.contacts.Update(r.Context(), userID, id, contactInput)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toUserContactResponse(contact))
	case http.MethodDelete:
		if err := h.contacts.Delete(r.Context(), userID, id); err != nil {
			writeServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *UserContactHandler) authorizedUser(w http.ResponseWriter, r *http.Request) (int64, bool) {
	authenticatedID, ok := authenticatedUserID(r.Context())
	if !ok {
		writeUnauthorized(w)
		return 0, false
	}
	return authenticatedID, true
}
