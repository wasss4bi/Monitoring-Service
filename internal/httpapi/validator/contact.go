package validator

import (
	"strings"
	"unicode/utf8"

	"monitoring_service/internal/domain"
)

type RawContactInput struct {
	Name string
}

type RawUserContactInput struct {
	ContactID int64
	Contact   string
	IsActive  bool
}

func NormalizeContactInput(input RawContactInput) (domain.CreateContactInput, FieldErrors) {
	name := strings.TrimSpace(input.Name)
	var fields FieldErrors
	if name == "" || utf8.RuneCountInString(name) > 50 {
		fields = addFieldError(fields, "name", "name is required and must contain at most 50 symbols")
	}
	if len(fields) > 0 {
		return domain.CreateContactInput{}, fields
	}
	return domain.CreateContactInput{Name: name}, nil
}

func NormalizeContactName(name string) (string, FieldErrors) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 50 {
		return "", FieldErrors{"name": {"name is required and must contain at most 50 symbols"}}
	}
	return name, nil
}

func NormalizeUserContactInput(input RawUserContactInput) (domain.CreateUserContactInput, FieldErrors) {
	contact := strings.TrimSpace(input.Contact)
	var fields FieldErrors
	if input.ContactID <= 0 {
		fields = addFieldError(fields, "contact_id", "contact_id must be a positive integer")
	}
	if contact == "" || utf8.RuneCountInString(contact) > 255 {
		fields = addFieldError(fields, "contact", "contact is required and must contain at most 255 symbols")
	}
	if len(fields) > 0 {
		return domain.CreateUserContactInput{}, fields
	}
	return domain.CreateUserContactInput{ContactID: input.ContactID, Contact: contact}, nil
}

func NormalizeUserContactUpdateInput(input RawUserContactInput) (domain.UpdateUserContactInput, FieldErrors) {
	contact := strings.TrimSpace(input.Contact)
	if contact == "" || utf8.RuneCountInString(contact) > 255 {
		return domain.UpdateUserContactInput{}, FieldErrors{"contact": {"contact is required and must contain at most 255 symbols"}}
	}
	return domain.UpdateUserContactInput{Contact: contact, IsActive: input.IsActive}, nil
}
