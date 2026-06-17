package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"monitoring_service/internal/domain"
)

type OutboxRepository struct{ db *sql.DB }

func NewOutboxRepository(db *sql.DB) *OutboxRepository { return &OutboxRepository{db: db} }

func (r *OutboxRepository) Claim(ctx context.Context, token string, lease time.Duration) (domain.OutboxEvent, error) {
	if token == "" || lease <= 0 {
		return domain.OutboxEvent{}, fmt.Errorf("outbox lease token and duration are required")
	}
	var event domain.OutboxEvent
	err := r.db.QueryRowContext(ctx, `
        WITH candidate AS (
            SELECT id FROM outbox_events
            WHERE published_at IS NULL
              AND next_attempt_at <= NOW()
              AND (lease_until IS NULL OR lease_until < NOW())
            ORDER BY next_attempt_at, id
            FOR UPDATE SKIP LOCKED
            LIMIT 1
        )
        UPDATE outbox_events event
        SET lease_token = $1::uuid,
            lease_until = NOW() + ($2 * INTERVAL '1 microsecond'),
            publish_attempts = publish_attempts + 1
        FROM candidate
        WHERE event.id = candidate.id
        RETURNING event.id, event.event_id::text, event.event_type,
                  event.aggregate_type, event.aggregate_id, event.payload
    `, token, lease.Microseconds()).Scan(
		&event.ID,
		&event.EventID,
		&event.EventType,
		&event.AggregateType,
		&event.AggregateID,
		&event.Payload,
	)
	if err != nil {
		return domain.OutboxEvent{}, err
	}
	return event, nil
}

func (r *OutboxRepository) MarkPublished(ctx context.Context, id int64, token string) error {
	result, err := r.db.ExecContext(ctx, `
        UPDATE outbox_events
        SET published_at = NOW(), lease_token = NULL, lease_until = NULL
        WHERE id = $1 AND lease_token = $2::uuid AND published_at IS NULL
    `, id, token)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("outbox event %d lease is no longer held", id)
	}
	return nil
}

func (r *OutboxRepository) Release(ctx context.Context, id int64, token string) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE outbox_events
        SET lease_token = NULL, lease_until = NULL
        WHERE id = $1 AND lease_token = $2::uuid AND published_at IS NULL
    `, id, token)
	return err
}

func (r *OutboxRepository) PendingStats(ctx context.Context) (int64, time.Duration, error) {
	var (
		count      int64
		ageSeconds sql.NullFloat64
	)
	if err := r.db.QueryRowContext(ctx, `
        SELECT COUNT(*), EXTRACT(EPOCH FROM NOW() - MIN(created_at))
        FROM outbox_events
        WHERE published_at IS NULL
    `).Scan(&count, &ageSeconds); err != nil {
		return 0, 0, fmt.Errorf("get pending outbox statistics: %w", err)
	}
	if !ageSeconds.Valid {
		return count, 0, nil
	}
	return count, time.Duration(ageSeconds.Float64 * float64(time.Second)), nil
}
