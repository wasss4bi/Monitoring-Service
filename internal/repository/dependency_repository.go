package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

type DependencyRepository struct{ db *sql.DB }

func NewDependencyRepository(db *sql.DB) *DependencyRepository { return &DependencyRepository{db: db} }

func (r *DependencyRepository) IsAlertProcessed(ctx context.Context, messageID string) (bool, error) {
	return NewProcessedMessageRepository().IsProcessed(ctx, r.db, "operational-notification-worker", messageID)
}

func (r *DependencyRepository) AcquireProbe(ctx context.Context, name, token string) (bool, error) {
	if _, err := r.db.ExecContext(ctx, `INSERT INTO service_dependencies(name) VALUES($1) ON CONFLICT DO NOTHING`, name); err != nil {
		return false, err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE service_dependencies SET lease_token=$2::uuid,lease_until=NOW()+INTERVAL '5 seconds' WHERE name=$1 AND (lease_until IS NULL OR lease_until<NOW() OR lease_token=$2::uuid)`, name, token)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (r *DependencyRepository) RecordProbe(ctx context.Context, name, token string, healthy bool, recipients []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	var state string
	var failures, successes int
	var episode sql.NullString
	var since time.Time
	err = tx.QueryRowContext(ctx, `SELECT status,failures,successes,episode_id::text,transition_at FROM service_dependencies WHERE name=$1 AND lease_token=$2::uuid AND lease_until>NOW() FOR UPDATE`, name, token).Scan(&state, &failures, &successes, &episode, &since)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	next := state
	if healthy {
		successes++
		failures = 0
		if successes >= 2 {
			next = "up"
		}
	} else {
		failures++
		successes = 0
		if failures >= 3 {
			next = "down"
		}
	}
	transition := next != state
	if transition && next == "down" {
		episode = sql.NullString{String: uuid.NewString(), Valid: true}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE service_dependencies SET status=$3::text,failures=$4,successes=$5,episode_id=$6::uuid,
 transition_at=CASE WHEN status!=$3 THEN NOW() ELSE transition_at END,last_probe_at=NOW() WHERE name=$1 AND lease_token=$2::uuid`, name, token, next, failures, successes, episode); err != nil {
		return err
	}
	if transition && episode.Valid && (next == "down" || state == "down" && next == "up") {
		var backlog int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM monitors WHERE status='monitoring' AND deleted_at IS NULL AND next_check_at<NOW()`).Scan(&backlog); err != nil {
			return err
		}
		message := fmt.Sprintf("Fetcher %s: %s at %s; overdue monitors: %d", name, next, time.Now().UTC().Format(time.RFC3339), backlog)
		if next == "up" {
			message += fmt.Sprintf("; outage: %s", time.Since(since).Round(time.Second))
		}
		for _, recipient := range recipients {
			var id int64
			err = tx.QueryRowContext(ctx, `INSERT INTO operational_notifications(dependency_name,episode_id,transition,recipient,message) VALUES($1,$2::uuid,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING id`, name, episode.String, next, recipient, message).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string]int64{"operational_notification_id": id})
			if _, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload,next_attempt_at) VALUES('operational.notification.deliver','operational_notification',$1,$2,NOW())`, id, payload); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

type OperationalNotification struct {
	ID                        int64
	Recipient, Message, Token string
}

func (r *DependencyRepository) ClaimAlert(ctx context.Context, id int64) (OperationalNotification, error) {
	job := OperationalNotification{ID: id, Token: uuid.NewString()}
	err := r.db.QueryRowContext(ctx, `UPDATE operational_notifications SET status='sending',lease_token=$2::uuid,lease_until=NOW()+INTERVAL '30 seconds',attempts=attempts+1 WHERE id=$1 AND status!='sent' AND next_attempt_at<=NOW() AND (lease_until IS NULL OR lease_until<NOW()) RETURNING recipient,message`, id, job.Token).Scan(&job.Recipient, &job.Message)
	return job, err
}
func (r *DependencyRepository) FinishAlert(ctx context.Context, job OperationalNotification, messageID string, sent bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	state := "pending"
	if sent {
		state = "sent"
	}
	result, err := tx.ExecContext(ctx, `UPDATE operational_notifications SET status=$3::text,lease_token=NULL,lease_until=NULL,next_attempt_at=NOW()+INTERVAL '30 seconds',sent_at=CASE WHEN $3='sent' THEN NOW() ELSE sent_at END WHERE id=$1 AND lease_token=$2::uuid AND lease_until>NOW()`, job.ID, job.Token, state)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if !sent {
		payload, _ := json.Marshal(map[string]int64{"operational_notification_id": job.ID})
		if _, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload,next_attempt_at) VALUES('operational.notification.deliver','operational_notification',$1,$2,NOW()+INTERVAL '30 seconds')`, job.ID, payload); err != nil {
			return err
		}
	}
	if _, err = NewProcessedMessageRepository().RecordOnce(ctx, tx, "operational-notification-worker", messageID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *DependencyRepository) RecoverAlerts(ctx context.Context) error {
	// A fresh event is safe if a worker crashed after sending: delivery is explicitly
	// at-least-once. Retain pending work through arbitrarily long outages.
	_, err := r.db.ExecContext(ctx, `WITH due AS (
 SELECT id FROM operational_notifications WHERE status!='sent' AND next_attempt_at<NOW()-INTERVAL '30 seconds'
 AND (lease_until IS NULL OR lease_until<NOW()) ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT 100
 ), reset AS (UPDATE operational_notifications n SET status='pending',lease_token=NULL,lease_until=NULL,next_attempt_at=NOW()
 FROM due WHERE n.id=due.id RETURNING n.id)
 INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload,next_attempt_at)
 SELECT 'operational.notification.deliver','operational_notification',id,jsonb_build_object('operational_notification_id',id),NOW() FROM reset`)
	return err
}
