package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"monitoring_service/internal/domain"
)

var (
	ErrIdempotencyProcessing = errors.New("idempotency key is processing")
	ErrIdempotencyConflict   = errors.New("idempotency key conflicts with request")
)

type idempotencyRepository interface {
	CreateProcessing(context.Context, domain.IdempotencyEntry) (bool, error)
	Get(context.Context, string) (domain.IdempotencyEntry, error)
	MarkProcessingResponse(context.Context, string, int, []byte) error
	MarkCompleted(context.Context, string, int, []byte) error
}

type IdempotencyService struct {
	repository idempotencyRepository
	ttl        time.Duration
	now        func() time.Time
}

func NewIdempotencyService(repository idempotencyRepository, ttl time.Duration) *IdempotencyService {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &IdempotencyService{repository: repository, ttl: ttl, now: time.Now}
}

func HashRequestBody(body []byte) string {
	hash := sha256.Sum256(body)
	return fmt.Sprintf("%x", hash)
}

func (s *IdempotencyService) Begin(ctx context.Context, entry domain.IdempotencyEntry) (domain.IdempotencyEntry, bool, error) {
	if s == nil || s.repository == nil {
		return domain.IdempotencyEntry{}, false, NewPublicError(nil, "request result tracking is unavailable; try again later", errors.New("idempotency service is not configured"))
	}
	entry.Key = strings.TrimSpace(entry.Key)
	if entry.Key == "" || len(entry.Key) > 255 {
		return domain.IdempotencyEntry{}, false, NewPublicError(ErrValidation, "Idempotency-Key is invalid", nil)
	}
	if entry.Status == "" {
		entry.Status = domain.IdempotencyStatusProcessing
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = s.now().UTC()
	}
	if entry.ExpiresAt.IsZero() {
		entry.ExpiresAt = entry.CreatedAt.Add(s.ttl)
	}
	created, err := s.repository.CreateProcessing(ctx, entry)
	if err != nil {
		return domain.IdempotencyEntry{}, false, operationError(err, "could not prepare request result tracking; retry with the same Idempotency-Key")
	}
	if created {
		return domain.IdempotencyEntry{}, true, nil
	}

	existing, err := s.repository.Get(ctx, entry.Key)
	if err != nil {
		return domain.IdempotencyEntry{}, false, operationError(err, "could not prepare request result tracking; retry with the same Idempotency-Key")
	}
	return existing, false, nil
}

func (s *IdempotencyService) Complete(ctx context.Context, key string, responseCode int, responseBody []byte) error {
	if s == nil || s.repository == nil {
		return NewPublicError(nil, "request result tracking is unavailable; try again later", errors.New("idempotency service is not configured"))
	}
	return s.repository.MarkCompleted(ctx, key, responseCode, responseBody)
}

func (s *IdempotencyService) SaveProcessingResponse(ctx context.Context, key string, responseCode int, responseBody []byte) error {
	if s == nil || s.repository == nil {
		return NewPublicError(nil, "request result tracking is unavailable; try again later", errors.New("idempotency service is not configured"))
	}
	return s.repository.MarkProcessingResponse(ctx, key, responseCode, responseBody)
}
