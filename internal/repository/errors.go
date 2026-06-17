package repository

import (
	"database/sql"
	"errors"

	"github.com/lib/pq"
)

var ErrNotFound = sql.ErrNoRows

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func IsUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func IsMissingContactChannel(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23503" && pqErr.Constraint == "user_contacts_contact_id_fkey"
}
