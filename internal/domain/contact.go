package domain

import "time"

type Contact struct {
	ID   int64
	Name string
}

type CreateContactInput struct {
	Name string
}

type UserContact struct {
	ID         int64
	UserID     int64
	ContactID  int64
	Contact    string
	Channel    string
	IsVerified bool
	IsActive   bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type CreateUserContactInput struct {
	UserID    int64
	ContactID int64
	Contact   string
}

type UpdateUserContactInput struct {
	Contact  string
	IsActive bool
}
