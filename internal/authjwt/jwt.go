package authjwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidToken = errors.New("invalid token")

type Claims struct {
	UserID      int64 `json:"user_id"`
	AuthVersion int64 `json:"auth_version"`
	ExpiresAt   int64 `json:"exp"`
}

func Generate(secret string, claims Claims) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("JWT secret is not configured")
	}
	if claims.UserID <= 0 || claims.AuthVersion <= 0 || claims.ExpiresAt <= time.Now().Unix() {
		return "", ErrInvalidToken
	}

	header, err := json.Marshal(struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}{Algorithm: "HS256", Type: "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signedPart := encodedHeader + "." + encodedPayload
	return signedPart + "." + sign(secret, signedPart), nil
}

func Parse(secret, token string) (Claims, error) {
	if secret == "" {
		return Claims{}, ErrInvalidToken
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Claims{}, ErrInvalidToken
	}
	if !hmac.Equal([]byte(sign(secret, parts[0]+"."+parts[1])), []byte(parts[2])) {
		return Claims{}, ErrInvalidToken
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var header struct {
		Algorithm string `json:"alg"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Algorithm != "HS256" {
		return Claims{}, ErrInvalidToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.UserID <= 0 || claims.AuthVersion <= 0 || claims.ExpiresAt <= time.Now().Unix() {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}

func sign(secret, value string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
