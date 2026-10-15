package repository

import (
	"context"
	"database/sql"

	"monitoring_service/internal/domain"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) AccessState(ctx context.Context, id int64) (string, int64, error) {
	var role string
	var version int64
	err := r.db.QueryRowContext(ctx, `SELECT role, auth_version FROM users WHERE id=$1`, id).Scan(&role, &version)
	return role, version, err
}

func (r *UserRepository) List(ctx context.Context) ([]domain.User, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, email
		FROM users
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]domain.User, 0)
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email); err != nil {
			return nil, err
		}
		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

func (r *UserRepository) Create(ctx context.Context, input domain.User) (domain.User, error) {
	var u domain.User
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO users (name, email, password)
		VALUES ($1, $2, $3)
		RETURNING id, name, email
	`, input.Name, input.Email, input.PasswordHash).Scan(&u.ID, &u.Name, &u.Email)
	return u, err
}

func (r *UserRepository) GetByID(ctx context.Context, id int64) (domain.User, error) {
	var u domain.User
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, email
		FROM users
		WHERE id = $1
	`, id).Scan(&u.ID, &u.Name, &u.Email)
	return u, err
}
func (r *UserRepository) GetPasswordHashByID(ctx context.Context, id int64) (domain.User, error) {
	var u domain.User
	err := r.db.QueryRowContext(ctx, `
		SELECT password
		FROM users
		WHERE id = $1
	`, id).Scan(&u.PasswordHash)
	return u, err
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	var u domain.User
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, email, password, auth_version
		FROM users
		WHERE email = $1
	`, email).Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.AuthVersion)
	return u, err
}

func (r *UserRepository) Update(ctx context.Context, id int64, input domain.User) (domain.User, error) {
	var u domain.User
	err := r.db.QueryRowContext(ctx, `
		UPDATE users
		SET name = $1, email = $2, auth_version = auth_version + CASE WHEN password IS DISTINCT FROM $3 THEN 1 ELSE 0 END, password = $3
		WHERE id = $4
		RETURNING id, name, email
	`, input.Name, input.Email, input.PasswordHash, id).Scan(&u.ID, &u.Name, &u.Email)
	return u, err
}

func (r *UserRepository) Delete(ctx context.Context, id int64) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}
