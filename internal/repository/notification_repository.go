package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"monitoring_service/internal/domain"
)

type NotificationRepository struct {
	db *sql.DB
}

func NewNotificationRepository(db *sql.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) Create(ctx context.Context, input domain.Notification) (domain.Notification, error) {
	var contactID sql.NullInt64
	if input.UserContactID != nil {
		contactID = sql.NullInt64{Int64: *input.UserContactID, Valid: true}
	}
	notification, err := scanNotification(r.db.QueryRowContext(ctx, `
        INSERT INTO notifications (monitor_id, user_contact_id, event_type, message)
        VALUES ($1, $2, $3, $4)
        RETURNING id, monitor_id, user_contact_id, event_type, message, status,
                  attempts_count, last_error, next_attempt_at, created_at, sent_at,
                  idempotency_key::text
    `, input.MonitorID, contactID, input.EventType, input.Message))
	if err != nil {
		return domain.Notification{}, fmt.Errorf("create notification: %w", err)
	}
	return notification, nil
}

func (r *NotificationRepository) ListByMonitor(ctx context.Context, monitorID int64) ([]domain.Notification, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, monitor_id, user_contact_id, event_type, message, status,
               attempts_count, last_error, next_attempt_at, created_at, sent_at,
               idempotency_key::text
        FROM notifications
        WHERE monitor_id = $1
        ORDER BY created_at DESC, id DESC
    `, monitorID)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "close query rows", "error", err)
		}
	}()

	notifications := make([]domain.Notification, 0)
	for rows.Next() {
		notification, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return notifications, nil
}

type notificationScanner interface {
	Scan(...any) error
}

func scanNotification(scanner notificationScanner) (domain.Notification, error) {
	var (
		notification domain.Notification
		contactID    sql.NullInt64
		lastError    sql.NullString
		sentAt       sql.NullTime
	)
	err := scanner.Scan(
		&notification.ID,
		&notification.MonitorID,
		&contactID,
		&notification.EventType,
		&notification.Message,
		&notification.Status,
		&notification.AttemptsCount,
		&lastError,
		&notification.NextAttemptAt,
		&notification.CreatedAt,
		&sentAt,
		&notification.IdempotencyKey,
	)
	if err != nil {
		return domain.Notification{}, err
	}
	if contactID.Valid {
		contactIDValue := contactID.Int64
		notification.UserContactID = &contactIDValue
	}
	notification.LastError = nullStringPtr(lastError)
	notification.SentAt = nullTimePtr(sentAt)
	return notification, nil
}

func (r *NotificationRepository) ListByUser(ctx context.Context, userID int64, limit int, beforeCreatedAt *time.Time, beforeID int64) ([]domain.Notification, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT n.id,n.monitor_id,n.user_contact_id,n.event_type,n.message,n.status,n.attempts_count,n.last_error,n.next_attempt_at,n.created_at,n.sent_at,n.idempotency_key::text
        FROM notifications n JOIN monitors m ON m.id=n.monitor_id
        WHERE m.user_id=$1 AND m.deleted_at IS NULL
          AND ($2::timestamptz IS NULL OR (n.created_at,n.id)<($2,$3))
        ORDER BY n.created_at DESC,n.id DESC LIMIT $4`, userID, beforeCreatedAt, beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("list user notifications: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "close query rows", "error", err)
		}
	}()
	result := make([]domain.Notification, 0)
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}
