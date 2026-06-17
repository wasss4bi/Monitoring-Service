package repository

import (
	"context"
	"crypto/hmac"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
)

var ErrVerificationTooSoon = errors.New("verification requested too soon")
var ErrVerificationInvalid = errors.New("verification code invalid or expired")
var ErrVerificationBusy = errors.New("verification delivery in progress")
var ErrVerificationChannel = errors.New("unsupported verification channel")

type ContactVerificationRepository struct{ db *sql.DB }

func NewContactVerificationRepository(db *sql.DB) *ContactVerificationRepository {
	return &ContactVerificationRepository{db: db}
}
func (r *ContactVerificationRepository) Request(ctx context.Context, userID, contactID int64, id string, digest []byte) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	var value, channel string
	var verified bool
	err = tx.QueryRowContext(ctx, `SELECT uc.contact,c.name,uc.is_verified FROM user_contacts uc JOIN contacts c ON c.id=uc.contact_id WHERE uc.id=$1 AND uc.user_id=$2 FOR UPDATE OF uc`, contactID, userID).Scan(&value, &channel, &verified)
	if err != nil {
		return err
	}
	if channel != "email" && channel != "telegram" {
		return ErrVerificationChannel
	}
	if verified {
		return ErrVerificationInvalid
	}
	var tooSoon bool
	err = tx.QueryRowContext(ctx, `SELECT requested_at > NOW() - INTERVAL '1 minute' FROM contact_verifications WHERE contact_id=$1`, contactID).Scan(&tooSoon)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && tooSoon {
		return ErrVerificationTooSoon
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO contact_verifications(contact_id,challenge_id,contact_value,code_hash,expires_at,requested_at) VALUES($1,$2::uuid,$3,$4,NOW()+INTERVAL '10 minutes',NOW()) ON CONFLICT(contact_id) DO UPDATE SET challenge_id=EXCLUDED.challenge_id,contact_value=EXCLUDED.contact_value,code_hash=EXCLUDED.code_hash,attempts=0,expires_at=EXCLUDED.expires_at,requested_at=EXCLUDED.requested_at,sent_at=NULL,lease_token=NULL,lease_until=NULL`, contactID, id, value, digest)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"contact_verification_id": id})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload,next_attempt_at) VALUES('contact.verification.deliver','user_contact',$1,$2::jsonb,NOW())`, contactID, payload)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *ContactVerificationRepository) Verify(ctx context.Context, userID, contactID int64, digest func(string) []byte) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	var value string
	var verified bool
	if err = tx.QueryRowContext(ctx, `SELECT contact,is_verified FROM user_contacts WHERE id=$1 AND user_id=$2 FOR UPDATE`, contactID, userID).Scan(&value, &verified); err != nil {
		return err
	}
	var id, expectedValue string
	var hash []byte
	var attempts int
	var valid bool
	err = tx.QueryRowContext(ctx, `SELECT challenge_id::text,contact_value,code_hash,attempts,expires_at>NOW() FROM contact_verifications WHERE contact_id=$1 FOR UPDATE`, contactID).Scan(&id, &expectedValue, &hash, &attempts, &valid)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrVerificationInvalid
	}
	if err != nil {
		return err
	}
	if verified || value != expectedValue || !valid || attempts >= 5 {
		return ErrVerificationInvalid
	}
	if !hmac.Equal(hash, digest(id)) {
		_, err = tx.ExecContext(ctx, `UPDATE contact_verifications SET attempts=attempts+1 WHERE contact_id=$1`, contactID)
		if err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return ErrVerificationInvalid
	}
	if _, err = tx.ExecContext(ctx, `UPDATE user_contacts SET is_verified=TRUE,is_active=FALSE,updated_at=NOW() WHERE id=$1`, contactID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM contact_verifications WHERE contact_id=$1`, contactID); err != nil {
		return err
	}
	return tx.Commit()
}

type VerificationDelivery struct{ ID, Token, Channel, Recipient string }

func (r *ContactVerificationRepository) ClaimDelivery(ctx context.Context, id, token string) (VerificationDelivery, error) {
	var job VerificationDelivery
	job.ID = id
	job.Token = token
	err := r.db.QueryRowContext(ctx, `UPDATE contact_verifications v SET lease_token=$2::uuid,lease_until=NOW()+INTERVAL '30 seconds' FROM user_contacts u JOIN contacts c ON c.id=u.contact_id WHERE v.challenge_id=$1::uuid AND v.contact_id=u.id AND u.contact=v.contact_value AND NOT u.is_verified AND v.expires_at>NOW() AND v.sent_at IS NULL AND (v.lease_until IS NULL OR v.lease_until<NOW()) RETURNING c.name,v.contact_value`, id, token).Scan(&job.Channel, &job.Recipient)
	if !errors.Is(err, sql.ErrNoRows) {
		return job, err
	}
	var busy bool
	e := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM contact_verifications v JOIN user_contacts u ON u.id=v.contact_id WHERE v.challenge_id=$1::uuid AND v.expires_at>NOW() AND v.sent_at IS NULL AND v.contact_value=u.contact AND NOT u.is_verified AND v.lease_until>=NOW())`, id).Scan(&busy)
	if e != nil {
		return job, e
	}
	if busy {
		return job, ErrVerificationBusy
	}
	return job, sql.ErrNoRows
}
func (r *ContactVerificationRepository) FinishDelivery(ctx context.Context, job VerificationDelivery, sent bool) error {
	result, err := r.db.ExecContext(ctx, `UPDATE contact_verifications SET sent_at=CASE WHEN $3 THEN NOW() ELSE NULL END,lease_token=NULL,lease_until=NULL WHERE challenge_id=$1::uuid AND lease_token=$2::uuid`, job.ID, job.Token, sent)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("verification delivery lease lost")
	}
	return nil
}
