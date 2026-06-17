package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/lib/pq"

	"monitoring_service/internal/domain"
)

type TargetHostnameRepository struct {
	db *sql.DB
}

func NewTargetHostnameRepository(db *sql.DB) *TargetHostnameRepository {
	return &TargetHostnameRepository{db: db}
}

const targetHostnameSelect = `
	SELECT id, hostname, bootstrap_url, limit_per_minute, is_active,
	       session_status, session_generation, session_updated_at, last_session_error,
	       created_at, updated_at, bootstrap_rules
	FROM target_hostnames
`

func (r *TargetHostnameRepository) Create(ctx context.Context, input domain.CreateTargetHostnameInput) (domain.TargetHostname, error) {
	return scanTargetHostname(r.db.QueryRowContext(ctx, `
		INSERT INTO target_hostnames(hostname, bootstrap_url, limit_per_minute, is_active, bootstrap_rules)
		VALUES ($1, $2, $3, $4, COALESCE($5::jsonb,'[]'::jsonb))
		RETURNING id, hostname, bootstrap_url, limit_per_minute, is_active,
		          session_status, session_generation, session_updated_at, last_session_error,
		          created_at, updated_at, bootstrap_rules
	`, input.Hostname, input.BootstrapURL, input.LimitPerMinute, input.IsActive, []byte(input.BootstrapRules)))
}

func (r *TargetHostnameRepository) List(ctx context.Context) ([]domain.TargetHostname, error) {
	rows, err := r.db.QueryContext(ctx, targetHostnameSelect+` ORDER BY hostname`)
	if err != nil {
		return nil, fmt.Errorf("list target hostnames: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "close query rows", "error", err)
		}
	}()
	result := make([]domain.TargetHostname, 0)
	for rows.Next() {
		target, err := scanTargetHostname(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list target hostnames: %w", err)
	}
	return result, nil
}

func (r *TargetHostnameRepository) Get(ctx context.Context, id int64) (domain.TargetHostname, error) {
	return scanTargetHostname(r.db.QueryRowContext(ctx, targetHostnameSelect+` WHERE id = $1`, id))
}

func (r *TargetHostnameRepository) FindAvailable(ctx context.Context, hostname string) (domain.TargetHostname, error) {
	return scanTargetHostname(r.db.QueryRowContext(ctx, targetHostnameSelect+`
		WHERE hostname = $1 AND is_active
	`, hostname))
}

func (r *TargetHostnameRepository) Update(ctx context.Context, id int64, input domain.UpdateTargetHostnameInput) (domain.TargetHostname, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TargetHostname{}, fmt.Errorf("begin target hostname update: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()

	target, err := scanTargetHostname(tx.QueryRowContext(ctx, `
		UPDATE target_hostnames
		SET bootstrap_url = CASE WHEN $3 THEN NULL ELSE COALESCE($2, bootstrap_url) END,
		    limit_per_minute = COALESCE($4, limit_per_minute),
		    is_active = COALESCE($5, is_active),
            bootstrap_rules = COALESCE($6::jsonb, bootstrap_rules),
		    session_ciphertext = CASE
		        WHEN (CASE WHEN $3 THEN NULL ELSE COALESCE($2, bootstrap_url) END) IS DISTINCT FROM bootstrap_url
		        THEN NULL ELSE session_ciphertext END,
		    session_status = CASE
		        WHEN (CASE WHEN $3 THEN NULL ELSE COALESCE($2, bootstrap_url) END) IS DISTINCT FROM bootstrap_url
		        THEN 'missing' ELSE session_status END,
		    session_generation = CASE
		        WHEN (CASE WHEN $3 THEN NULL ELSE COALESCE($2, bootstrap_url) END) IS DISTINCT FROM bootstrap_url
		        THEN session_generation + 1 ELSE session_generation END,
		    refresh_lease_token = CASE
		        WHEN (CASE WHEN $3 THEN NULL ELSE COALESCE($2, bootstrap_url) END) IS DISTINCT FROM bootstrap_url
		        THEN NULL ELSE refresh_lease_token END,
		    refresh_lease_until = CASE
		        WHEN (CASE WHEN $3 THEN NULL ELSE COALESCE($2, bootstrap_url) END) IS DISTINCT FROM bootstrap_url
		        THEN NULL ELSE refresh_lease_until END,
		    session_updated_at = CASE
		        WHEN (CASE WHEN $3 THEN NULL ELSE COALESCE($2, bootstrap_url) END) IS DISTINCT FROM bootstrap_url
		        THEN NULL ELSE session_updated_at END,
		    last_session_error = CASE
		        WHEN (CASE WHEN $3 THEN NULL ELSE COALESCE($2, bootstrap_url) END) IS DISTINCT FROM bootstrap_url
		        THEN NULL ELSE last_session_error END,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, hostname, bootstrap_url, limit_per_minute, is_active,
		          session_status, session_generation, session_updated_at, last_session_error,
		          created_at, updated_at, bootstrap_rules
	`, id, input.BootstrapURL, input.ClearBootstrap, input.LimitPerMinute, input.IsActive, []byte(input.BootstrapRules)))
	if err != nil {
		return domain.TargetHostname{}, err
	}

	if input.BootstrapURL != nil || input.ClearBootstrap || input.IsActive != nil || len(input.BootstrapRules) > 0 {
		// Fence monitors before jobs, matching finalization's lock order.
		if _, err := tx.ExecContext(ctx, `UPDATE monitors m SET generation=m.generation+1,
         check_status='idle',next_check_at=CASE WHEN m.status='monitoring' THEN NOW() ELSE NULL END,
         attempts_count=0,consecutive_errors=0,last_error=NULL,
         session_ciphertext=CASE WHEN m.session_mode='managed' THEN NULL ELSE m.session_ciphertext END,
         session_status=CASE WHEN m.session_mode='managed' THEN 'missing' ELSE m.session_status END,
         session_updated_at=CASE WHEN m.session_mode='managed' THEN NULL ELSE m.session_updated_at END,
         last_session_error=CASE WHEN m.session_mode='managed' THEN NULL ELSE m.last_session_error END,
         session_generation=m.session_generation+1,profile_generation=m.profile_generation+1,
         refresh_lease_token=NULL,refresh_lease_until=NULL,updated_at=NOW()
         FROM monitored_endpoints e WHERE m.endpoint_id=e.id AND e.target_hostname_id=$1 AND m.deleted_at IS NULL`, id); err != nil {
			return domain.TargetHostname{}, fmt.Errorf("invalidate target monitors: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE endpoint_fetches f SET status='failed',finished_at=NOW(),lease_token=NULL,lease_until=NULL,last_error='target hostname configuration changed'
         FROM monitored_endpoints e WHERE f.endpoint_id=e.id AND e.target_hostname_id=$1 AND f.status IN ('pending','fetching')`, id); err != nil {
			return domain.TargetHostname{}, fmt.Errorf("cancel target fetches: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.TargetHostname{}, fmt.Errorf("commit target hostname update: %w", err)
	}
	return target, nil
}

func (r *TargetHostnameRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM target_hostnames WHERE id = $1`, id)
	if err != nil {
		var postgresError *pq.Error
		if errors.As(err, &postgresError) && postgresError.Code == "23503" {
			return ErrTargetHostnameReferenced
		}
		return fmt.Errorf("delete target hostname: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete target hostname: %w", err)
	}
	if affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *TargetHostnameRepository) ResetSession(ctx context.Context, id int64) (domain.TargetHostname, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TargetHostname{}, fmt.Errorf("begin target session reset: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	target, err := scanTargetHostname(tx.QueryRowContext(ctx, `
		UPDATE target_hostnames
		SET session_ciphertext = NULL, session_status = 'missing',
		    session_generation = session_generation + 1,
		    refresh_lease_token = NULL, refresh_lease_until = NULL,
		    session_updated_at = NULL, last_session_error = NULL, updated_at = NOW()
		WHERE id = $1
		RETURNING id, hostname, bootstrap_url, limit_per_minute, is_active,
		          session_status, session_generation, session_updated_at, last_session_error,
		          created_at, updated_at, bootstrap_rules
	`, id))
	if err != nil {
		return domain.TargetHostname{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE monitors m SET generation=m.generation+1,
         profile_generation=m.profile_generation+1,session_generation=m.session_generation+1,
         session_ciphertext=NULL,session_status='missing',session_updated_at=NULL,last_session_error=NULL,
         refresh_lease_token=NULL,refresh_lease_until=NULL,check_status='idle',
         next_check_at=CASE WHEN m.status='monitoring' THEN NOW() ELSE NULL END,
         attempts_count=0,consecutive_errors=0,last_error=NULL,updated_at=NOW()
         FROM monitored_endpoints e WHERE m.endpoint_id=e.id AND e.target_hostname_id=$1
         AND m.session_mode='managed' AND m.deleted_at IS NULL`, id); err != nil {
		return domain.TargetHostname{}, fmt.Errorf("reset managed target sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE endpoint_fetches f SET status='failed',finished_at=NOW(),
         lease_token=NULL,lease_until=NULL,last_error='target session reset'
         FROM monitors m,monitored_endpoints e WHERE f.monitor_id=m.id AND m.endpoint_id=e.id
         AND e.target_hostname_id=$1 AND m.session_mode='managed' AND m.deleted_at IS NULL
         AND f.status IN ('pending','fetching')`, id); err != nil {
		return domain.TargetHostname{}, fmt.Errorf("cancel reset target fetches: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.TargetHostname{}, fmt.Errorf("commit target session reset: %w", err)
	}
	return target, nil
}

var ErrTargetHostnameReferenced = errors.New("target hostname is referenced by monitored endpoints")

type targetHostnameScanner interface {
	Scan(...any) error
}

func scanTargetHostname(scanner targetHostnameScanner) (domain.TargetHostname, error) {
	var (
		target           domain.TargetHostname
		bootstrapURL     sql.NullString
		sessionUpdatedAt sql.NullTime
		lastSessionError sql.NullString
	)
	if err := scanner.Scan(
		&target.ID, &target.Hostname, &bootstrapURL, &target.LimitPerMinute,
		&target.IsActive, &target.SessionStatus,
		&target.SessionGeneration, &sessionUpdatedAt, &lastSessionError,
		&target.CreatedAt, &target.UpdatedAt, &target.BootstrapRules,
	); err != nil {
		return domain.TargetHostname{}, err
	}
	if bootstrapURL.Valid {
		target.BootstrapURL = &bootstrapURL.String
	}
	target.SessionUpdatedAt = nullTimePtr(sessionUpdatedAt)
	target.LastSessionError = nullStringPtr(lastSessionError)
	return target, nil
}
