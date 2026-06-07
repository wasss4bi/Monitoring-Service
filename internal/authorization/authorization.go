package authorization

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const encryptedPrefix = "v1:"

var ErrInvalidCiphertext = errors.New("invalid encrypted authorization value")

type Protector struct {
	key     []byte
	purpose []byte
}

func NewForPurpose(encodedKey, purpose string) (*Protector, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
	if err != nil || len(key) != 32 {
		return nil, errors.New("encryption key must be a base64-encoded 32-byte key")
	}
	if purpose == "" {
		return nil, errors.New("encryption purpose is empty")
	}
	return &Protector{key: key, purpose: []byte(purpose)}, nil
}

func (p *Protector) Encrypt(value string) (string, error) {
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate authorization nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(value), p.purpose)
	return encryptedPrefix + base64.RawURLEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

func (p *Protector) Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return "", ErrInvalidCiphertext
	}
	encoded := strings.TrimPrefix(value, encryptedPrefix)
	body, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(body) < gcm.NonceSize() {
		return "", ErrInvalidCiphertext
	}
	plaintext, err := gcm.Open(nil, body[:gcm.NonceSize()], body[gcm.NonceSize():], p.purpose)
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	return string(plaintext), nil
}
