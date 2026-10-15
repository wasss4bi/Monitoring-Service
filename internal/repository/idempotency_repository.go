package repository

import (
	"context"
	"database/sql"

	"monitoring_service/internal/domain"
)

type IdempotencyRepository struct {
	db *sql.DB
}

func NewIdempotencyRepository(db *sql.DB) *IdempotencyRepository {
	return &IdempotencyRepository{db: db}
}

// CreateProcessing atomically creates a processing entry. An expired entry for
// the same key can be replaced, so key reuse does not depend on the cleanup job.
func (r *IdempotencyRepository) CreateProcessing(ctx context.Context, entry domain.IdempotencyEntry) (bool, error) {
	var key string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO idempotency_keys (key, status, request_hash, response_body, user_id, ip, device, browser, created_at, expires_at)
		VALUES ($1, $2, $3, NULL, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (key) DO UPDATE
		SET status = EXCLUDED.status,
			request_hash = EXCLUDED.request_hash,
			monitor_id = NULL,
			response_code = NULL,
			response_body = NULL,
			user_id = EXCLUDED.user_id,
			ip = EXCLUDED.ip,
			device = EXCLUDED.device,
			browser = EXCLUDED.browser,
			created_at = EXCLUDED.created_at,
			expires_at = EXCLUDED.expires_at
		WHERE idempotency_keys.expires_at <= NOW()
		RETURNING key
	`, entry.Key, entry.Status, entry.RequestHash, entry.UserID, entry.IP, entry.Device, entry.Browser, entry.CreatedAt, entry.ExpiresAt).Scan(&key)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *IdempotencyRepository) Get(ctx context.Context, key string) (domain.IdempotencyEntry, error) {
	var entry domain.IdempotencyEntry
	var responseCode sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT key, status, request_hash, response_code, response_body, user_id, ip, device, browser, created_at, expires_at, monitor_id
		FROM idempotency_keys
		WHERE key = $1 AND expires_at > NOW()
	`, key).Scan(
		&entry.Key,
		&entry.Status,
		&entry.RequestHash,
		&responseCode,
		&entry.ResponseBody,
		&entry.UserID,
		&entry.IP,
		&entry.Device,
		&entry.Browser,
		&entry.CreatedAt,
		&entry.ExpiresAt,
		&entry.MonitorID,
	)
	if responseCode.Valid {
		entry.ResponseCode = int(responseCode.Int64)
	}
	return entry, err
}

func (r *IdempotencyRepository) MarkProcessingResponse(ctx context.Context, key string, responseCode int, responseBody []byte) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE idempotency_keys
		SET response_code = $2, response_body = NULLIF($3, '')::jsonb
		WHERE key = $1 AND status = $4
	`, key, responseCode, string(responseBody), domain.IdempotencyStatusProcessing)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *IdempotencyRepository) MarkCompleted(ctx context.Context, key string, responseCode int, responseBody []byte) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE idempotency_keys
		SET status = $2, response_code = $3, response_body = NULLIF($4, '')::jsonb
		WHERE key = $1
	`, key, domain.IdempotencyStatusCompleted, responseCode, string(responseBody))
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *IdempotencyRepository) DeleteExpired(ctx context.Context) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM idempotency_keys
		WHERE expires_at <= NOW()
	`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
