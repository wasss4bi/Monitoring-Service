package service

import (
	"context"
	"errors"
	"testing"

	"github.com/lib/pq"

	"monitoring_service/internal/domain"
)

type contactRepositoryFake struct {
	created domain.CreateContactInput
	contact domain.Contact
	err     error
}

func (f *contactRepositoryFake) List(context.Context) ([]domain.Contact, error) {
	return nil, nil
}

func (f *contactRepositoryFake) Get(context.Context, int64) (domain.Contact, error) {
	return domain.Contact{}, nil
}

func (f *contactRepositoryFake) Create(_ context.Context, input domain.CreateContactInput) (domain.Contact, error) {
	f.created = input
	return f.contact, f.err
}

func (f *contactRepositoryFake) Update(context.Context, int64, string) (domain.Contact, error) {
	return domain.Contact{}, nil
}

func (f *contactRepositoryFake) Delete(context.Context, int64) (int64, error) {
	return 0, nil
}

func TestContactServiceCreateTrimsNameAndUsesGeneratedID(t *testing.T) {
	repository := &contactRepositoryFake{contact: domain.Contact{ID: 7, Name: "telegram"}}
	service := NewContactService(repository)

	contact, err := service.Create(context.Background(), domain.CreateContactInput{Name: " telegram "})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if contact.ID != 7 {
		t.Fatalf("contact ID = %d, want generated ID 7", contact.ID)
	}
	if repository.created.Name != "telegram" {
		t.Fatalf("created name = %q, want trimmed name", repository.created.Name)
	}
}

func TestContactServiceCreateReturnsConflictForDuplicateName(t *testing.T) {
	repository := &contactRepositoryFake{err: &pq.Error{Code: "23505"}}
	service := NewContactService(repository)

	_, err := service.Create(context.Background(), domain.CreateContactInput{Name: "telegram"})
	if err == nil || !errors.Is(err, ErrConflict) {
		t.Fatalf("Create() error = %v, want conflict", err)
	}
}
