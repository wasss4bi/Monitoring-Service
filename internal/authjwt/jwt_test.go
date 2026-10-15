package authjwt

import (
	"errors"
	"testing"
	"time"
)

func TestParseAcceptsValidToken(t *testing.T) {
	token, err := Generate("test-secret", Claims{
		UserID:      42,
		AuthVersion: 1,
		ExpiresAt:   time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	claims, err := Parse("test-secret", token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if claims.UserID != 42 {
		t.Errorf("user ID = %d, want 42", claims.UserID)
	}
}

func TestParseRejectsInvalidSignature(t *testing.T) {
	token, err := Generate("test-secret", Claims{
		UserID:      42,
		AuthVersion: 1,
		ExpiresAt:   time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	_, err = Parse("other-secret", token)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
	}
}
