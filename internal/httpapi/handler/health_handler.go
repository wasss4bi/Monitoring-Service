package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"monitoring_service/internal/service"
)

type HealthHandler struct {
	db *sql.DB
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.db.PingContext(ctx); err != nil {
		setRequestError(w, err)
		writeError(w, http.StatusServiceUnavailable, service.PublicMessage(service.ErrServiceUnavailable))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
