package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"monitoring_service/internal/domain"
	"monitoring_service/internal/repository"
)

type contactRepository interface {
	List(context.Context) ([]domain.Contact, error)
	Get(context.Context, int64) (domain.Contact, error)
	Create(context.Context, domain.CreateContactInput) (domain.Contact, error)
	Update(context.Context, int64, string) (domain.Contact, error)
	Delete(context.Context, int64) (int64, error)
}

type ContactService struct {
	contacts contactRepository
}

func NewContactService(contacts contactRepository) *ContactService {
	return &ContactService{contacts: contacts}
}

func (s *ContactService) List(ctx context.Context) ([]domain.Contact, error) {
	contacts, err := s.contacts.List(ctx)
	return contacts, operationError(err, "could not list contacts; try again later")
}

func (s *ContactService) Get(ctx context.Context, id int64) (domain.Contact, error) {
	contact, err := s.contacts.Get(ctx, id)
	if repository.IsNotFound(err) {
		return domain.Contact{}, NewPublicError(ErrNotFound, "notification channel was not found", err)
	}
	return contact, operationError(err, "could not get contact; try again later")
}

func (s *ContactService) Create(ctx context.Context, input domain.CreateContactInput) (domain.Contact, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 50 {
		return domain.Contact{}, NewPublicError(ErrValidation, "name up to 50 characters is required", nil)
	}
	contact, err := s.contacts.Create(ctx, input)
	if repository.IsUniqueViolation(err) {
		return domain.Contact{}, NewPublicError(ErrConflict, "contact name already exists", err)
	}
	return contact, operationError(err, "could not create contact; try again later")
}

func (s *ContactService) Update(ctx context.Context, id int64, name string) (domain.Contact, error) {
	name = strings.TrimSpace(name)
	if id <= 0 || name == "" || utf8.RuneCountInString(name) > 50 {
		return domain.Contact{}, NewPublicError(ErrValidation, "positive id and name up to 50 characters are required", nil)
	}
	contact, err := s.contacts.Update(ctx, id, name)
	if repository.IsNotFound(err) {
		return domain.Contact{}, NewPublicError(ErrNotFound, "notification channel was not found", err)
	}
	if repository.IsUniqueViolation(err) {
		return domain.Contact{}, NewPublicError(ErrConflict, "contact name already exists", err)
	}
	return contact, operationError(err, "could not update contact; try again later")
}

func (s *ContactService) Delete(ctx context.Context, id int64) error {
	deleted, err := s.contacts.Delete(ctx, id)
	if err != nil {
		return operationError(err, "could not delete contact; try again later")
	}
	if deleted == 0 {
		return NewPublicError(ErrNotFound, "notification channel was not found", nil)
	}
	return nil
}

type userContactRepository interface {
	ListByUser(context.Context, int64) ([]domain.UserContact, error)
	Get(context.Context, int64, int64) (domain.UserContact, error)
	ListActiveByUser(context.Context, int64) ([]domain.UserContact, error)
	Create(context.Context, domain.CreateUserContactInput) (domain.UserContact, error)
	Update(context.Context, int64, int64, domain.UpdateUserContactInput) (domain.UserContact, error)
	Delete(context.Context, int64, int64) (int64, error)
}

type UserContactService struct {
	contacts userContactRepository
}

func NewUserContactService(contacts userContactRepository) *UserContactService {
	return &UserContactService{contacts: contacts}
}

func (s *UserContactService) List(ctx context.Context, userID int64) ([]domain.UserContact, error) {
	if userID <= 0 {
		return nil, NewPublicError(ErrValidation, "user id is required", nil)
	}
	contacts, err := s.contacts.ListByUser(ctx, userID)
	return contacts, operationError(err, "could not list contacts; try again later")
}

func (s *UserContactService) Get(ctx context.Context, userID, id int64) (domain.UserContact, error) {
	if userID <= 0 || id <= 0 {
		return domain.UserContact{}, NewPublicError(ErrValidation, "user and contact ids are required", nil)
	}
	contact, err := s.contacts.Get(ctx, userID, id)
	if repository.IsNotFound(err) {
		return domain.UserContact{}, NewPublicError(ErrNotFound, "user contact was not found", err)
	}
	return contact, operationError(err, "could not get contact; try again later")
}

func (s *UserContactService) Create(ctx context.Context, input domain.CreateUserContactInput) (domain.UserContact, error) {
	input.Contact = strings.TrimSpace(input.Contact)
	if input.UserID <= 0 || input.ContactID <= 0 || input.Contact == "" || utf8.RuneCountInString(input.Contact) > 255 {
		return domain.UserContact{}, NewPublicError(ErrValidation, "user id, contact id and contact are required", nil)
	}
	contact, err := s.contacts.Create(ctx, input)
	if repository.IsMissingContactChannel(err) {
		return domain.UserContact{}, NewPublicError(ErrContactChannelNotFound, "notification channel was not found", err)
	}
	if repository.IsUniqueViolation(err) {
		return domain.UserContact{}, NewPublicError(ErrConflict, "this user contact already exists", err)
	}
	return contact, operationError(err, "could not create contact; try again later")
}

func (s *UserContactService) Update(ctx context.Context, userID, id int64, input domain.UpdateUserContactInput) (domain.UserContact, error) {
	input.Contact = strings.TrimSpace(input.Contact)
	if userID <= 0 || id <= 0 || input.Contact == "" || utf8.RuneCountInString(input.Contact) > 255 {
		return domain.UserContact{}, NewPublicError(ErrValidation, "contact is required", nil)
	}
	contact, err := s.contacts.Update(ctx, userID, id, input)
	if repository.IsNotFound(err) {
		return domain.UserContact{}, NewPublicError(ErrNotFound, "user contact was not found", err)
	}
	if repository.IsUniqueViolation(err) {
		return domain.UserContact{}, NewPublicError(ErrConflict, "this user contact already exists", err)
	}
	return contact, operationError(err, "could not update contact; try again later")
}

func (s *UserContactService) Delete(ctx context.Context, userID, id int64) error {
	deleted, err := s.contacts.Delete(ctx, userID, id)
	if err != nil {
		return operationError(err, "could not delete contact; try again later")
	}
	if deleted == 0 {
		return NewPublicError(ErrNotFound, "user contact was not found", nil)
	}
	return nil
}
