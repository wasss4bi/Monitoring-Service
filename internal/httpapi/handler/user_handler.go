package handler

import (
	"errors"
	"net/http"
	"strconv"

	"monitoring_service/internal/httpapi/validator"
	"monitoring_service/internal/service"
)

type UserHandler struct {
	users       *service.UserService
	idempotency *idempotencyExecutor
}

func NewUserHandler(users *service.UserService, idempotency ...*service.IdempotencyService) *UserHandler {
	return &UserHandler{users: users, idempotency: newIdempotencyExecutor(idempotency)}
}

func (h *UserHandler) Users(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.list(w, r)
	case http.MethodPost:
		h.idempotency.run(w, r, nil, h.create)
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *UserHandler) UserByID(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "/users/")
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.get(w, r, id)
	case http.MethodPut:
		h.update(w, r, id)
	case http.MethodDelete:
		h.delete(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *UserHandler) CheckPassword(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	id, err := strconv.ParseInt(rawID, 10, 64)

	if err != nil {
		writeError(w, http.StatusBadRequest, "incorrect id")
		return
	}

	switch r.Method {
	case http.MethodPost:
		h.checkPassword(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *UserHandler) list(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.List(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toUserResponses(users))
}

func (h *UserHandler) create(w http.ResponseWriter, r *http.Request) {
	var input createUserRequest
	if !decodeJSON(w, r, &input) {
		return
	}

	userInput, fields := validator.NormalizeCreateUserInput(validator.RawUserInput{
		Name:     input.Name,
		Email:    input.Email,
		Password: input.Password,
	})
	if len(fields) > 0 {
		writeValidationError(w, http.StatusBadRequest, fields)
		return
	}

	u, err := h.users.Create(r.Context(), userInput)
	if err != nil {
		writeUserServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, toUserResponse(u))
}

func (h *UserHandler) get(w http.ResponseWriter, r *http.Request, id int64) {
	u, err := h.users.GetByID(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toUserResponse(u))
}

func (h *UserHandler) update(w http.ResponseWriter, r *http.Request, id int64) {
	var input updateUserRequest
	if !decodeJSON(w, r, &input) {
		return
	}

	userInput, fields := validator.NormalizeUpdateUserInput(validator.RawUserInput{
		Name:     input.Name,
		Email:    input.Email,
		Password: input.Password,
	})
	if len(fields) > 0 {
		writeValidationError(w, http.StatusBadRequest, fields)
		return
	}

	u, err := h.users.Update(r.Context(), id, userInput)
	if err != nil {
		writeUserServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toUserResponse(u))
}

func writeUserServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrConflict) {
		writeValidationError(w, http.StatusConflict, map[string][]string{
			"email": {"this email is already exists"},
		})
		return
	}

	writeServiceError(w, err)
}

func (h *UserHandler) delete(w http.ResponseWriter, r *http.Request, id int64) {
	if err := h.users.Delete(r.Context(), id); err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *UserHandler) checkPassword(w http.ResponseWriter, r *http.Request, id int64) {
	var input checkPasswordRequest
	if !decodeJSON(w, r, &input) {
		return
	}

	passwordInput, fields := validator.NormalizeCheckPasswordInput(validator.RawCheckPasswordInput{
		Password: input.Password,
	})
	if len(fields) > 0 {
		writeValidationError(w, http.StatusBadRequest, fields)
		return
	}

	cpo, err := h.users.IsValidPassword(r.Context(), id, passwordInput)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toCheckPasswordResponse(cpo))
}
