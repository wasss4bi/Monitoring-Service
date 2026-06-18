package service

import (
	"context"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"monitoring_service/internal/domain"
	"monitoring_service/internal/repository"
)

type userRepository interface {
	List(ctx context.Context) ([]domain.User, error)
	Create(ctx context.Context, input domain.User) (domain.User, error)
	GetByID(ctx context.Context, id int64) (domain.User, error)
	GetPasswordHashByID(ctx context.Context, id int64) (domain.User, error)
	GetByEmail(ctx context.Context, email string) (domain.User, error)
	Update(ctx context.Context, id int64, input domain.User) (domain.User, error)
	Delete(ctx context.Context, id int64) (int64, error)
}

func (s *UserService) Authenticate(ctx context.Context, input domain.LoginInput) (domain.User, error) {
	input.Email = strings.TrimSpace(input.Email)
	if input.Email == "" || input.Password == "" {
		return domain.User{}, NewPublicError(ErrValidation, "email and password are required", nil)
	}

	user, err := s.users.GetByEmail(ctx, input.Email)
	if repository.IsNotFound(err) {
		return domain.User{}, NewPublicError(ErrUnauthorized, "email or password is incorrect", err)
	}
	if err != nil {
		return domain.User{}, operationError(err, "could not sign in; try again later")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		return domain.User{}, NewPublicError(ErrUnauthorized, "email or password is incorrect", err)
	}
	return user, nil
}

type UserService struct {
	users userRepository
}

func NewUserService(users userRepository) *UserService {
	return &UserService{users: users}
}

func (s *UserService) List(ctx context.Context) ([]domain.User, error) {
	users, err := s.users.List(ctx)
	return users, operationError(err, "could not list users; try again later")
}

func (s *UserService) Create(ctx context.Context, input domain.CreateUserInput) (domain.User, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.TrimSpace(input.Email)

	if input.Name == "" || input.Email == "" || input.Password == "" {
		return domain.User{}, NewPublicError(ErrValidation, "name, email and password are required", nil)
	}

	passwordHash, err := hashPassword(input.Password)
	if err != nil {
		return domain.User{}, operationError(err, "could not create user; try again later")
	}

	u, err := s.users.Create(ctx, domain.User{
		Name:         input.Name,
		Email:        input.Email,
		PasswordHash: passwordHash,
	})
	if repository.IsUniqueViolation(err) {
		return domain.User{}, NewPublicError(ErrConflict, "email is already registered", err)
	}
	return u, operationError(err, "could not create user; try again later")
}

func (s *UserService) GetByID(ctx context.Context, id int64) (domain.User, error) {
	u, err := s.users.GetByID(ctx, id)
	if repository.IsNotFound(err) {
		return domain.User{}, NewPublicError(ErrNotFound, "user was not found", err)
	}
	return u, operationError(err, "could not read user; try again later")
}

func (s *UserService) Update(ctx context.Context, id int64, input domain.UpdateUserInput) (domain.User, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.TrimSpace(input.Email)

	if input.Name == "" || input.Email == "" || input.Password == "" {
		return domain.User{}, NewPublicError(ErrValidation, "name, email and password are required", nil)
	}

	current, err := s.users.GetPasswordHashByID(ctx, id)
	if repository.IsNotFound(err) {
		return domain.User{}, NewPublicError(ErrNotFound, "user was not found", err)
	}
	if err != nil {
		return domain.User{}, operationError(err, "could not update user; try again later")
	}
	passwordHash := current.PasswordHash
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.Password)) != nil {
		passwordHash, err = hashPassword(input.Password)
		if err != nil {
			return domain.User{}, operationError(err, "could not update user; try again later")
		}
	}

	u, err := s.users.Update(ctx, id, domain.User{
		Name:         input.Name,
		Email:        input.Email,
		PasswordHash: passwordHash,
	})
	if repository.IsNotFound(err) {
		return domain.User{}, NewPublicError(ErrNotFound, "user was not found", err)
	}
	if repository.IsUniqueViolation(err) {
		return domain.User{}, NewPublicError(ErrConflict, "email is already registered", err)
	}
	return u, operationError(err, "could not update user; try again later")
}

func (s *UserService) Delete(ctx context.Context, id int64) error {
	affected, err := s.users.Delete(ctx, id)
	if err != nil {
		return operationError(err, "could not delete user; try again later")
	}
	if affected == 0 {
		return NewPublicError(ErrNotFound, "user was not found", nil)
	}
	return nil
}
func (s *UserService) IsValidPassword(ctx context.Context, id int64, input domain.CheckPasswordInput) (domain.CheckPasswordOutput, error) {
	u, err := s.users.GetPasswordHashByID(ctx, id)
	passwordHash := u.PasswordHash
	if repository.IsNotFound(err) {
		return domain.CheckPasswordOutput{}, NewPublicError(ErrNotFound, "user was not found", err)
	}
	if err != nil {
		return domain.CheckPasswordOutput{}, operationError(err, "could not verify password; try again later")
	}
	err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.Password))

	cpo := domain.CheckPasswordOutput{
		Valid: err == nil,
	}
	return cpo, nil
}
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
