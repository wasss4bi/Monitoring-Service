package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"monitoring_service/internal/domain"
)

type FetchObservation struct {
	Observed          bool
	HTTPStatus        *int
	Metadata          json.RawMessage
	Failure           bool
	Retry             bool
	RetryAfter        time.Duration
	Terminal          bool
	Succeeded         bool
	Message           string
	Matched           []domain.MonitorRule
	SessionCiphertext string
	SessionGeneration int64
	RejectSession     bool
	SessionError      string
	AccessDenied      bool
	NextCheck         time.Duration
}

// FinalizeObservation commits every business effect under the same owned lease.
func (r *MonitoringExecutionRepository) FinalizeObservation(ctx context.Context, fetchID, token, messageID string, o FetchObservation) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	owner, err := lockOwnedFetch(ctx, tx, fetchID, token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if o.Terminal {
		if err = finishMonitorTx(ctx, tx, fetchID, token, owner, o.Succeeded, o.Message, o.Matched); err != nil {
			return err
		}
	} else if !o.Failure {
		recorded, err := recordEvaluation(ctx, tx, fetchID, token, owner)
		if err != nil {
			return err
		}
		if !recorded {
			return nil
		}
		if _, err = tx.ExecContext(ctx, `UPDATE monitors SET last_checked_at=NOW(),last_error=NULL WHERE id=$1`, owner); err != nil {
			return err
		}
	}
	fetchStatus := "succeeded"
	checkStatus := "idle"
	delay := o.NextCheck
	if o.Failure {
		fetchStatus = "failed"
		checkStatus = "error"
		if o.Retry {
			checkStatus = "retry_wait"
			delay = o.RetryAfter
		} else if o.AccessDenied || o.HTTPStatus != nil && (*o.HTTPStatus == 401 || *o.HTTPStatus == 403) {
			checkStatus = "access_denied"
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE endpoint_fetches SET status=$2,finished_at=NOW(),lease_token=NULL,lease_until=NULL,
 response_status=$3,response_json=NULL,fetch_metadata=$4::jsonb,last_error=NULLIF($5,''),next_dispatch_at=NULL,defer_reason=NULL WHERE fetch_id=$1::uuid`, fetchID, fetchStatus, o.HTTPStatus, []byte(o.Metadata), o.Message); err != nil {
		return err
	}
	if o.Failure {
		_, err = tx.ExecContext(ctx, `UPDATE monitors SET check_status=$2,last_attempt_at=NOW(),last_checked_at=NOW(),attempts_count=attempts_count+1,
 consecutive_errors=consecutive_errors+1,retry_started_at=COALESCE(retry_started_at,NOW()),last_error=$3,
 next_check_at=CASE WHEN $4 THEN NOW()+($5*INTERVAL '1 microsecond') ELSE NULL END,updated_at=NOW() WHERE id=$1`, owner, checkStatus, o.Message, o.Retry, delay.Microseconds())
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE monitors SET check_status='idle',last_attempt_at=NOW(),last_success_at=CASE WHEN $3 THEN NOW() ELSE last_success_at END,
 attempts_count=0,consecutive_errors=0,retry_started_at=NULL,next_check_at=CASE WHEN status='monitoring' THEN NOW()+($2*INTERVAL '1 microsecond') ELSE NULL END,updated_at=NOW() WHERE id=$1`, owner, delay.Microseconds(), o.Observed)
	}
	if err != nil {
		return err
	}
	if o.SessionCiphertext != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE monitors SET session_ciphertext=$3,session_generation=session_generation+1,
 session_status='ready',session_updated_at=NOW(),last_session_error=NULL WHERE id=$1 AND session_generation=$2`, owner, o.SessionGeneration, o.SessionCiphertext); err != nil {
			return err
		}
	}
	if o.RejectSession {
		reason := o.SessionError
		if reason == "" {
			reason = "provided session rejected"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE monitors SET session_status='blocked',last_session_error=$3,refresh_lease_token=NULL,refresh_lease_until=NULL WHERE id=$1 AND session_generation=$2`, owner, o.SessionGeneration, reason); err != nil {
			return err
		}
	}
	if _, err = NewProcessedMessageRepository().RecordOnce(ctx, tx, EndpointFetchConsumer, messageID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *MonitoringExecutionRepository) BootstrapRules(ctx context.Context, targetID int64) (json.RawMessage, error) {
	var rules json.RawMessage
	err := r.db.QueryRowContext(ctx, `SELECT bootstrap_rules FROM target_hostnames WHERE id=$1 AND is_active`, targetID).Scan(&rules)
	return rules, err
}

func (r *MonitoringExecutionRepository) FetchHostname(ctx context.Context, fetchID string) (string, error) {
	var hostname string
	err := r.db.QueryRowContext(ctx, `SELECT t.hostname FROM endpoint_fetches f JOIN monitored_endpoints e ON e.id=f.endpoint_id JOIN target_hostnames t ON t.id=e.target_hostname_id WHERE f.fetch_id=$1::uuid`, fetchID).Scan(&hostname)
	return hostname, err
}

func (r *MonitoringExecutionRepository) SavePreparedSession(ctx context.Context, fetchID, token, refreshToken string, expected int64, ciphertext string) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	owner, err := lockOwnedFetch(ctx, tx, fetchID, token)
	if err != nil {
		return 0, err
	}
	var generation int64
	err = tx.QueryRowContext(ctx, `UPDATE monitors SET session_ciphertext=$4,session_generation=session_generation+1,
 session_status='ready',session_updated_at=NOW(),last_session_error=NULL,refresh_lease_token=NULL,refresh_lease_until=NULL
 WHERE id=$1 AND session_generation=$2 AND refresh_lease_token=$3::uuid AND refresh_lease_until>NOW() RETURNING session_generation`, owner, expected, refreshToken, ciphertext).Scan(&generation)
	if err != nil {
		return 0, err
	}
	return generation, tx.Commit()
}

func (r *MonitoringExecutionRepository) DeferInfrastructure(ctx context.Context, fetchID, token, messageID, reason string, delay time.Duration) error {
	if len(reason) > 64 {
		return fmt.Errorf("invalid infrastructure reason")
	}
	return r.deferFetch(ctx, fetchID, token, messageID, delay, reason)
}
