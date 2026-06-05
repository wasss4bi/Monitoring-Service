package domain

type User struct {
	ID           int64
	Name         string
	Email        string
	PasswordHash string
	AuthVersion  int64
}

type CreateUserInput struct {
	Name     string
	Email    string
	Password string
}

type UpdateUserInput struct {
	Name     string
	Email    string
	Password string
}
type CheckPasswordInput struct {
	Password string
}
type CheckPasswordOutput struct {
	Valid bool
}

type LoginInput struct {
	Email    string
	Password string
}
