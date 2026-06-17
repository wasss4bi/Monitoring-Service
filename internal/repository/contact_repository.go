package repository

import (
	"context"
	"database/sql"
	"log/slog"

	"monitoring_service/internal/domain"
)

type ContactRepository struct {
	db *sql.DB
}

func NewContactRepository(db *sql.DB) *ContactRepository {
	return &ContactRepository{db: db}
}

func (r *ContactRepository) List(ctx context.Context) ([]domain.Contact, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name
		FROM contacts
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "close query rows", "error", err)
		}
	}()

	contacts := make([]domain.Contact, 0)
	for rows.Next() {
		var contact domain.Contact
		if err := rows.Scan(&contact.ID, &contact.Name); err != nil {
			return nil, err
		}
		contacts = append(contacts, contact)
	}
	return contacts, rows.Err()
}

func (r *ContactRepository) Get(ctx context.Context, id int64) (domain.Contact, error) {
	var contact domain.Contact
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name
		FROM contacts
		WHERE id = $1
	`, id).Scan(&contact.ID, &contact.Name)
	return contact, err
}

func (r *ContactRepository) Create(ctx context.Context, input domain.CreateContactInput) (domain.Contact, error) {
	var contact domain.Contact
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO contacts (name)
		VALUES ($1)
		RETURNING id, name
	`, input.Name).Scan(&contact.ID, &contact.Name)
	return contact, err
}

func (r *ContactRepository) Update(ctx context.Context, id int64, name string) (domain.Contact, error) {
	var contact domain.Contact
	err := r.db.QueryRowContext(ctx, `
		UPDATE contacts
		SET name = $1
		WHERE id = $2
		RETURNING id, name
	`, name, id).Scan(&contact.ID, &contact.Name)
	return contact, err
}

func (r *ContactRepository) Delete(ctx context.Context, id int64) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM contacts WHERE id = $1`, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
