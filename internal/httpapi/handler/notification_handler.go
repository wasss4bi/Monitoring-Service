package handler

import (
	"encoding/base64"
	"encoding/json"
	"monitoring_service/internal/domain"
	"monitoring_service/internal/httpapi/validator"
	"monitoring_service/internal/service"
	"net/http"
	"strconv"
	"time"
)

type NotificationHandler struct{ notifications *service.NotificationService }

func NewNotificationHandler(s *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{notifications: s}
}

type notificationResponse struct {
	ID            int64      `json:"id"`
	MonitorID     int64      `json:"monitor_id"`
	EventType     string     `json:"event_type"`
	Message       string     `json:"message"`
	Status        string     `json:"status"`
	AttemptsCount int        `json:"attempts_count"`
	LastError     *string    `json:"last_error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	SentAt        *time.Time `json:"sent_at,omitempty"`
}
type notificationListResponse struct {
	Items      []notificationResponse `json:"items"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}
type notificationCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

func (h *NotificationHandler) Notifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	userID, ok := authenticatedUserID(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil || v < 1 || v > 100 {
			writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"limit": {"limit must be between 1 and 100"}})
			return
		}
		limit = v
	}
	var before *time.Time
	var beforeID int64
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		body, e := base64.RawURLEncoding.DecodeString(raw)
		var c notificationCursor
		if e != nil || json.Unmarshal(body, &c) != nil || c.ID < 1 || c.CreatedAt.IsZero() {
			writeValidationError(w, http.StatusBadRequest, validator.FieldErrors{"cursor": {"cursor is invalid"}})
			return
		}
		before = &c.CreatedAt
		beforeID = c.ID
	}
	items, e := h.notifications.List(r.Context(), userID, limit+1, before, beforeID)
	if e != nil {
		writeServiceError(w, e)
		return
	}
	n := len(items)
	if n > limit {
		n = limit
	}
	out := notificationListResponse{Items: make([]notificationResponse, 0, n)}
	for _, item := range items[:n] {
		out.Items = append(out.Items, toNotificationResponse(item))
	}
	if len(items) > limit {
		body, _ := json.Marshal(notificationCursor{items[n-1].CreatedAt, items[n-1].ID})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(body)
	}
	writeJSON(w, http.StatusOK, out)
}
func toNotificationResponse(n domain.Notification) notificationResponse {
	return notificationResponse{n.ID, n.MonitorID, n.EventType, n.Message, n.Status, n.AttemptsCount, n.LastError, n.CreatedAt, n.SentAt}
}
