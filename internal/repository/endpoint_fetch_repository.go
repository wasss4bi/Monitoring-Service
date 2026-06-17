package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"monitoring_service/internal/domain"
	"time"
)

type EndpointFetchRepository struct {
	db *sql.DB
}

func NewEndpointFetchRepository(db *sql.DB) *EndpointFetchRepository {
	return &EndpointFetchRepository{db: db}
}

func (r *EndpointFetchRepository) Create(ctx context.Context, endpointID, generation int64, scheduledAt time.Time) (domain.EndpointFetch, error) {
	if endpointID <= 0 || generation <= 0 || scheduledAt.IsZero() {
		return domain.EndpointFetch{}, fmt.Errorf("endpoint id, generation and scheduled time are required")
	}
	var fetch domain.EndpointFetch
	err := r.db.QueryRowContext(ctx, `
        INSERT INTO endpoint_fetches (endpoint_id, generation, scheduled_at)
        VALUES ($1, $2, $3)
        RETURNING fetch_id::text, endpoint_id, generation, scheduled_at, status, created_at
    `, endpointID, generation, scheduledAt).Scan(
		&fetch.FetchID,
		&fetch.EndpointID,
		&fetch.Generation,
		&fetch.ScheduledAt,
		&fetch.Status,
		&fetch.CreatedAt,
	)
	if err != nil {
		return domain.EndpointFetch{}, fmt.Errorf("create endpoint fetch: %w", err)
	}
	return fetch, nil
}

func (r *EndpointFetchRepository) Get(ctx context.Context, fetchID string) (domain.EndpointFetch, error) {
	var (
		fetch          domain.EndpointFetch
		startedAt      sql.NullTime
		finishedAt     sql.NullTime
		leaseToken     sql.NullString
		leaseUntil     sql.NullTime
		responseStatus sql.NullInt64
		responseJSON   []byte
		lastError      sql.NullString
	)
	err := r.db.QueryRowContext(ctx, `
        SELECT fetch_id::text, endpoint_id, generation, scheduled_at, started_at,
               finished_at, status, lease_token::text, lease_until, response_status,
               response_json, last_error, created_at
        FROM endpoint_fetches
        WHERE fetch_id = $1::uuid
    `, fetchID).Scan(
		&fetch.FetchID,
		&fetch.EndpointID,
		&fetch.Generation,
		&fetch.ScheduledAt,
		&startedAt,
		&finishedAt,
		&fetch.Status,
		&leaseToken,
		&leaseUntil,
		&responseStatus,
		&responseJSON,
		&lastError,
		&fetch.CreatedAt,
	)
	if err != nil {
		return domain.EndpointFetch{}, err
	}
	fetch.StartedAt = nullTimePtr(startedAt)
	fetch.FinishedAt = nullTimePtr(finishedAt)
	fetch.LeaseToken = nullStringPtr(leaseToken)
	fetch.LeaseUntil = nullTimePtr(leaseUntil)
	if responseStatus.Valid {
		status := int(responseStatus.Int64)
		fetch.ResponseStatus = &status
	}
	if responseJSON != nil {
		fetch.ResponseJSON = append(json.RawMessage(nil), responseJSON...)
	}
	fetch.LastError = nullStringPtr(lastError)
	return fetch, nil
}
