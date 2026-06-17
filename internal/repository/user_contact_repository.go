package repository

import (
	"context"
	"database/sql"
	"log/slog"

	"monitoring_service/internal/domain"
)

type UserContactRepository struct {
	db *sql.DB
}

func NewUserContactRepository(db *sql.DB) *UserContactRepository {
	return &UserContactRepository{db: db}
}

const userContactSelect = `
	SELECT uc.id, uc.user_id, uc.contact_id, uc.contact, c.name,
	       uc.is_verified, uc.is_active, uc.created_at, uc.updated_at
	FROM user_contacts uc
	JOIN contacts c ON c.id = uc.contact_id
`

func scanUserContact(row interface{ Scan(...any) error }) (domain.UserContact, error) {
	var contact domain.UserContact
	err := row.Scan(
		&contact.ID,
		&contact.UserID,
		&contact.ContactID,
		&contact.Contact,
		&contact.Channel,
		&contact.IsVerified,
		&contact.IsActive,
		&contact.CreatedAt,
		&contact.UpdatedAt,
	)
	return contact, err
}

func (r *UserContactRepository) ListByUser(ctx context.Context, userID int64) ([]domain.UserContact, error) {
	rows, err := r.db.QueryContext(ctx, userContactSelect+` WHERE uc.user_id = $1 ORDER BY uc.id`, userID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "close query rows", "error", err)
		}
	}()

	contacts := make([]domain.UserContact, 0)
	for rows.Next() {
		contact, err := scanUserContact(rows)
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, contact)
	}
	return contacts, rows.Err()
}

func (r *UserContactRepository) Get(ctx context.Context, userID, id int64) (domain.UserContact, error) {
	return scanUserContact(r.db.QueryRowContext(ctx, userContactSelect+` WHERE uc.user_id = $1 AND uc.id = $2`, userID, id))
}

func (r *UserContactRepository) ListActiveByUser(ctx context.Context, userID int64) ([]domain.UserContact, error) {
	rows, err := r.db.QueryContext(ctx, userContactSelect+` WHERE uc.user_id = $1 AND uc.is_active = TRUE AND uc.is_verified = TRUE ORDER BY uc.id`, userID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "close query rows", "error", err)
		}
	}()

	contacts := make([]domain.UserContact, 0)
	for rows.Next() {
		contact, err := scanUserContact(rows)
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, contact)
	}
	return contacts, rows.Err()
}

func (r *UserContactRepository) Create(ctx context.Context, input domain.CreateUserContactInput) (domain.UserContact, error) {
	return scanUserContact(r.db.QueryRowContext(ctx, `
		INSERT INTO user_contacts (user_id, contact_id, contact)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, contact_id, contact,
			(SELECT c.name FROM contacts c WHERE c.id = user_contacts.contact_id),
			is_verified, is_active, created_at, updated_at
	`, input.UserID, input.ContactID, input.Contact))
}

func (r *UserContactRepository) Update(ctx context.Context, userID, id int64, input domain.UpdateUserContactInput) (domain.UserContact, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.UserContact{}, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	var old string
	if err = tx.QueryRowContext(ctx, `SELECT contact FROM user_contacts WHERE user_id=$1 AND id=$2 FOR UPDATE`, userID, id).Scan(&old); err != nil {
		return domain.UserContact{}, err
	}
	contact, err := scanUserContact(tx.QueryRowContext(ctx, `
  UPDATE user_contacts SET contact=$1::text, is_verified=CASE WHEN contact IS DISTINCT FROM $1::text THEN FALSE ELSE is_verified END,
  is_active=CASE WHEN contact IS DISTINCT FROM $1::text OR NOT is_verified THEN FALSE ELSE $2 END,updated_at=NOW()
  WHERE user_id=$3 AND id=$4
  RETURNING id,user_id,contact_id,contact,(SELECT c.name FROM contacts c WHERE c.id=user_contacts.contact_id),is_verified,is_active,created_at,updated_at
 `, input.Contact, input.IsActive, userID, id))
	if err != nil {
		return domain.UserContact{}, err
	}
	if old != input.Contact {
		if _, err = tx.ExecContext(ctx, `UPDATE contact_verifications SET expires_at=NOW(), lease_token=NULL, lease_until=NULL WHERE contact_id=$1`, id); err != nil {
			return domain.UserContact{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return domain.UserContact{}, err
	}
	return contact, nil
}

func (r *UserContactRepository) Delete(ctx context.Context, userID, id int64) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM user_contacts WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
