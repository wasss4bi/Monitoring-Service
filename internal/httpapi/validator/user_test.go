package validator

import (
	"reflect"
	"strings"
	"testing"

	"monitoring_service/internal/domain"
)

func TestNormalizeCreateUserRejectsPasswordOverBcryptLimit(t *testing.T) {
	_, fields := NormalizeCreateUserInput(RawUserInput{Name: "Alice", Email: "alice@example.com", Password: "Aa1!" + strings.Repeat("a", 69)})
	if len(fields["password"]) == 0 {
		t.Fatal("password longer than 72 bytes must be rejected")
	}
}

func TestNormalizePasswordBcryptByteLimit(t *testing.T) {
	for _, tt := range []struct {
		name, password string
		valid          bool
	}{
		{"72 ASCII bytes", "Aa1!" + strings.Repeat("a", 68), true},
		{"73 ASCII bytes", "Aa1!" + strings.Repeat("a", 69), false},
		{"72 UTF-8 bytes", "Aa1!" + strings.Repeat("\u044f", 34), true},
		{"74 UTF-8 bytes", "Aa1!" + strings.Repeat("\u044f", 35), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := normalizePassword(tt.password)
			if (len(errs) == 0) != tt.valid {
				t.Fatalf("errors = %v, valid = %v", errs, tt.valid)
			}
		})
	}
}
func TestNormalizeCreateUserInput(t *testing.T) {
	tests := []struct {
		name       string
		input      RawUserInput
		want       domain.CreateUserInput
		wantFields FieldErrors
	}{
		{
			name: "normalizes a valid input",
			input: RawUserInput{
				Name:     "  Alice  ",
				Email:    "  alice+news@example.co.uk  ",
				Password: "(Bambamtutu123)",
			},
			want: domain.CreateUserInput{
				Name:     "Alice",
				Email:    "alice+news@example.co.uk",
				Password: "(Bambamtutu123)",
			},
		},
		{
			name: "rejects an email with a trailing domain dot",
			input: RawUserInput{
				Name:     "Alice",
				Email:    "alice2@example.",
				Password: "(Bambamtutu123)",
			},
			wantFields: FieldErrors{
				"email": {"email must be of type user@mailhost.com"},
			},
		},
		{
			name: "collects validation errors for every invalid field",
			input: RawUserInput{
				Name:     "Alice1",
				Email:    "alice@example..com",
				Password: "password",
			},
			wantFields: FieldErrors{
				"name": {
					"name must contain more than 3 symbols and contain only letters",
				},
				"email": {
					"email must be of type user@mailhost.com",
				},
				"password": {
					"password must contain more than 8 symbols",
					"password must include an uppercase letter",
					"password must include a number",
					"password must include a special symbol",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotFields := NormalizeCreateUserInput(tt.input)

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("NormalizeCreateUserInput() input = %#v, want %#v", got, tt.want)
			}
			if !reflect.DeepEqual(gotFields, tt.wantFields) {
				t.Fatalf("NormalizeCreateUserInput() fields = %#v, want %#v", gotFields, tt.wantFields)
			}
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		email string
		valid bool
	}{
		{email: "alice@example.com", valid: true},
		{email: "alice@example", valid: false},
		{email: "alice@example.", valid: false},
		{email: "alice@-example.com", valid: false},
		{email: "alice@example-.com", valid: false},
		{email: "alice@example..com", valid: false},
		{email: "alice@@example.com", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			_, err := normalizeEmail(tt.email)
			if (err == "") != tt.valid {
				t.Fatalf("normalizeEmail(%q) error = %q, valid = %t", tt.email, err, tt.valid)
			}
		})
	}
}
