package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// PublicError separates a reviewed client-facing reason from its diagnostic cause.
// Messages must be application-owned text, never an upstream error or user input.
type PublicError struct {
	kind    error
	message string
	cause   error
}

func NewPublicError(kind error, message string, cause error) error {
	return &PublicError{kind: kind, message: message, cause: cause}
}

func (e *PublicError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.message, e.cause)
	}
	return e.message
}

func (e *PublicError) Unwrap() []error {
	var causes []error
	if e.kind != nil {
		causes = append(causes, e.kind)
	}
	if e.cause != nil {
		causes = append(causes, e.cause)
	}
	return causes
}

// PublicMessage deliberately never falls back to err.Error(). Even an error
// wrapping a known sentinel can contain SQL, credentials or transport details.
func PublicMessage(err error) string {
	var public *PublicError
	if errors.As(err, &public) && public != nil && public.message != "" {
		return public.message
	}
	switch {
	case errors.Is(err, ErrValidation):
		return "request parameters are invalid"
	case errors.Is(err, ErrNotFound):
		return "requested resource was not found"
	case errors.Is(err, ErrConflict):
		return "request conflicts with the current resource state"
	case errors.Is(err, ErrServiceUnavailable):
		return "service is temporarily unavailable; try again later"
	case errors.Is(err, ErrUnauthorized):
		return "authentication failed"
	case errors.Is(err, ErrTargetHostnameUnavailable):
		return "hostname is not configured, inactive, or blocked"
	case errors.Is(err, ErrRateLimited):
		return "request rate limit exceeded; try again later"
	case errors.Is(err, context.Canceled):
		return "request was cancelled"
	default:
		return "failed to process request; try again later"
	}
}

func operationError(err error, message string) error {
	if err == nil {
		return nil
	}
	var public *PublicError
	if errors.As(err, &public) {
		return err
	}
	return NewPublicError(nil, message, err)
}

var (
	ErrConflict                  = errors.New("conflict")
	ErrNotFound                  = errors.New("not found")
	ErrServiceUnavailable        = errors.New("service unavailable")
	ErrRateLimited               = errors.New("rate limit exceeded")
	ErrUnauthorized              = errors.New("unauthorized")
	ErrValidation                = errors.New("validation failed")
	ErrContactChannelNotFound    = fmt.Errorf("contact channel not found: %w", ErrValidation)
	ErrTargetHostnameUnavailable = errors.New("target hostname is not configured, inactive, or blocked")
)

type RateLimitError struct {
	retryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return ErrRateLimited.Error()
}

func (e *RateLimitError) Unwrap() error {
	return ErrRateLimited
}

func (e *RateLimitError) RetryAfter() time.Duration {
	return e.retryAfter
}
