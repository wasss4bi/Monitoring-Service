package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lib/pq"
	"monitoring_service/internal/domain"
)

func TestContactServiceAcceptsUnicodeNameWithinCharacterLimit(t *testing.T) {
	_, err := NewContactService(&contactRepositoryFake{}).Create(context.Background(), domain.CreateContactInput{Name: strings.Repeat("\u044f", 26)})
	if err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
}

func TestUserContactServiceReportsUnknownChannel(t *testing.T) {
	repo := &contactValidationFake{err: &pq.Error{Code: "23503", Constraint: "user_contacts_contact_id_fkey"}}
	_, err := NewUserContactService(repo).Create(context.Background(), domain.CreateUserContactInput{UserID: 1, ContactID: 999, Contact: "test"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("got %v; want validation error", err)
	}
}

func TestUserContactServiceAcceptsUnicodeContactWithinCharacterLimit(t *testing.T) {
	_, err := NewUserContactService(&contactValidationFake{}).Create(context.Background(), domain.CreateUserContactInput{UserID: 1, ContactID: 1, Contact: strings.Repeat("\u044f", 128)})
	if err != nil {
		t.Fatalf("valid contact rejected: %v", err)
	}
}

type contactValidationFake struct{ err error }

func (f *contactValidationFake) ListByUser(context.Context, int64) ([]domain.UserContact, error) {
	return nil, nil
}
func (f *contactValidationFake) ListActiveByUser(context.Context, int64) ([]domain.UserContact, error) {
	return nil, nil
}
func (f *contactValidationFake) Get(context.Context, int64, int64) (domain.UserContact, error) {
	return domain.UserContact{}, nil
}
func (f *contactValidationFake) Create(context.Context, domain.CreateUserContactInput) (domain.UserContact, error) {
	return domain.UserContact{}, f.err
}
func (f *contactValidationFake) Update(context.Context, int64, int64, domain.UpdateUserContactInput) (domain.UserContact, error) {
	return domain.UserContact{}, f.err
}
func (f *contactValidationFake) Delete(context.Context, int64, int64) (int64, error) { return 0, nil }
