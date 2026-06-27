package authorization

import (
	"encoding/base64"
	"strings"
	"testing"
)

func testKey() string { return base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))) }

func TestProtectorEncryptDecrypt(t *testing.T) {
	protector, err := NewForPurpose(testKey(), "target-session:v1")
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := protector.Encrypt("Bearer secret")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, "Bearer secret") {
		t.Fatalf("ciphertext exposes plaintext: %q", ciphertext)
	}
	plaintext, err := protector.Decrypt(ciphertext)
	if err != nil || plaintext != "Bearer secret" {
		t.Fatalf("Decrypt() = %q, %v", plaintext, err)
	}
}

func TestProtectorRejectsInvalidKeyAndCiphertext(t *testing.T) {
	if _, err := NewForPurpose("not-a-key", "target-session:v1"); err == nil {
		t.Fatal("New() error = nil")
	}
	protector, _ := NewForPurpose(testKey(), "target-session:v1")
	if _, err := protector.Decrypt("Bearer plaintext"); err == nil {
		t.Fatal("Decrypt() error = nil")
	}
}
