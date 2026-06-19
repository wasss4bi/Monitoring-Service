// Package validator parses and validates data received through HTTP.
package validator

import (
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	"monitoring_service/internal/domain"
)

type RawUserInput struct {
	Name     string
	Email    string
	Password string
}

type RawCheckPasswordInput struct {
	Password string
}

// NormalizeCreateUserInput prepares a JSON request for the service layer.
func NormalizeCreateUserInput(input RawUserInput) (domain.CreateUserInput, FieldErrors) {
	var fields FieldErrors

	name, err := normalizeName(input.Name)
	if err != "" {
		fields = addFieldError(fields, "name", err)
	}

	email, err := normalizeEmail(input.Email)
	if err != "" {
		fields = addFieldError(fields, "email", err)
	}

	password, errors := normalizePassword(input.Password)
	for _, err := range errors {
		fields = addFieldError(fields, "password", err)
	}

	if len(fields) > 0 {
		return domain.CreateUserInput{}, fields
	}

	return domain.CreateUserInput{
		Name:     name,
		Email:    email,
		Password: password,
	}, nil
}

// NormalizeUpdateUserInput prepares a JSON request for the service layer.
func NormalizeUpdateUserInput(input RawUserInput) (domain.UpdateUserInput, FieldErrors) {
	var fields FieldErrors

	name, err := normalizeName(input.Name)
	if err != "" {
		fields = addFieldError(fields, "name", err)
	}

	email, err := normalizeEmail(input.Email)
	if err != "" {
		fields = addFieldError(fields, "email", err)
	}

	password, errors := normalizePassword(input.Password)
	for _, err := range errors {
		fields = addFieldError(fields, "password", err)
	}

	if len(fields) > 0 {
		return domain.UpdateUserInput{}, fields
	}

	return domain.UpdateUserInput{
		Name:     name,
		Email:    email,
		Password: password,
	}, nil
}

// NormalizeCheckPasswordInput prepares a JSON request for password checks.
func NormalizeCheckPasswordInput(input RawCheckPasswordInput) (domain.CheckPasswordInput, FieldErrors) {
	password, errors := normalizePassword(input.Password)
	if len(errors) > 0 {
		var fields FieldErrors
		for _, err := range errors {
			fields = addFieldError(fields, "password", err)
		}

		return domain.CheckPasswordInput{}, fields
	}

	return domain.CheckPasswordInput{Password: password}, nil
}

func normalizeName(name string) (string, string) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) <= 3 {
		return "", "name must contain more than 3 symbols and contain only letters"
	}
	for _, char := range name {
		if !unicode.IsLetter(char) {
			return "", "name must contain more than 3 symbols and contain only letters"
		}
	}

	return name, ""
}

func normalizeEmail(email string) (string, string) {
	email = strings.TrimSpace(email)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !isValidEmailDomain(email) {
		return "", "email must be of type user@mailhost.com"
	}

	return email, ""
}

func isValidEmailDomain(email string) bool {
	localPart, domain, ok := strings.Cut(email, "@")
	if !ok || localPart == "" || domain == "" || strings.Contains(domain, "@") {
		return false
	}

	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}

	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}

	return true
}

func normalizePassword(password string) (string, []string) {
	var errors []string
	if len(password) > 72 {
		errors = append(errors, "password must contain at most 72 bytes")
	}
	if utf8.RuneCountInString(password) <= 8 {
		errors = append(errors, "password must contain more than 8 symbols")
	}

	var hasUppercase, hasNumber, hasSpecial bool
	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUppercase = true
		case unicode.IsDigit(char):
			hasNumber = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	if !hasUppercase {
		errors = append(errors, "password must include an uppercase letter")
	}
	if !hasNumber {
		errors = append(errors, "password must include a number")
	}
	if !hasSpecial {
		errors = append(errors, "password must include a special symbol")
	}
	if len(errors) > 0 {
		return "", errors
	}

	return password, nil
}
