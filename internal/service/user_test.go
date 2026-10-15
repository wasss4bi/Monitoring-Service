package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"monitoring_service/internal/domain"
)

type fakeUserRepository struct {
	passwordHash string
	err          error
}

func (r fakeUserRepository) List(context.Context) ([]domain.User, error) {
	return nil, nil
}

func (r fakeUserRepository) Create(context.Context, domain.User) (domain.User, error) {
	return domain.User{}, nil
}

func (r fakeUserRepository) GetByID(context.Context, int64) (domain.User, error) {
	return domain.User{}, nil
}

func (r fakeUserRepository) GetPasswordHashByID(context.Context, int64) (domain.User, error) {
	return domain.User{PasswordHash: r.passwordHash}, r.err
}

func (r fakeUserRepository) GetByEmail(context.Context, string) (domain.User, error) {
	return domain.User{PasswordHash: r.passwordHash}, r.err
}

func (r fakeUserRepository) Update(context.Context, int64, domain.User) (domain.User, error) {
	return domain.User{}, nil
}

func (r fakeUserRepository) Delete(context.Context, int64) (int64, error) {
	return 0, nil
}

func TestUserServiceIsValidPassword(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate password hash: %v", err)
	}

	tests := []struct {
		name      string
		repo      fakeUserRepository
		password  string
		wantValid bool
		wantErr   error
	}{
		{
			name:      "correct password",
			repo:      fakeUserRepository{passwordHash: string(passwordHash)},
			password:  "correct-password",
			wantValid: true,
		},
		{
			name:      "incorrect password",
			repo:      fakeUserRepository{passwordHash: string(passwordHash)},
			password:  "incorrect-password",
			wantValid: false,
		},
		{
			name:     "user not found",
			repo:     fakeUserRepository{err: sql.ErrNoRows},
			password: "correct-password",
			wantErr:  ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewUserService(tt.repo)

			result, err := service.IsValidPassword(t.Context(), 1, domain.CheckPasswordInput{
				Password: tt.password,
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("IsValidPassword() error = %v, want %v", err, tt.wantErr)
			}
			if result.Valid != tt.wantValid {
				t.Errorf("IsValidPassword() valid = %t, want %t", result.Valid, tt.wantValid)
			}
		})
	}
}
