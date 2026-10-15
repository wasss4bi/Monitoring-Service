package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"monitoring_service/internal/repository"
	"time"
)

type ContactVerificationService struct {
	repo            *repository.ContactVerificationRepository
	secret          string
	email, telegram OperationalSender
}

func NewContactVerificationService(repo *repository.ContactVerificationRepository, secret string, email, telegram OperationalSender) *ContactVerificationService {
	return &ContactVerificationService{repo: repo, secret: secret, email: email, telegram: telegram}
}
func (s *ContactVerificationService) code(id string) string {
	m := hmac.New(sha256.New, []byte(s.secret))
	m.Write([]byte("contact-code:v1:" + id))
	return fmt.Sprintf("%06d", binary.BigEndian.Uint64(m.Sum(nil)[:8])%1000000)
}
func (s *ContactVerificationService) digest(id, code string) []byte {
	m := hmac.New(sha256.New, []byte(s.secret))
	m.Write([]byte("contact-verification:v1:" + id + ":" + code))
	return m.Sum(nil)
}
func (s *ContactVerificationService) Request(ctx context.Context, userID, id int64) error {
	if s.secret == "" {
		return NewPublicError(ErrServiceUnavailable, "contact verification is unavailable", nil)
	}
	challenge := uuid.NewString()
	err := s.repo.Request(ctx, userID, id, challenge, s.digest(challenge, s.code(challenge)))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return NewPublicError(ErrNotFound, "contact was not found", err)
	case errors.Is(err, repository.ErrVerificationTooSoon):
		return &RateLimitError{retryAfter: time.Minute}
	case errors.Is(err, repository.ErrVerificationChannel):
		return NewPublicError(ErrValidation, "contact channel does not support verification", err)
	case errors.Is(err, repository.ErrVerificationInvalid):
		return NewPublicError(ErrConflict, "contact is already verified", err)
	default:
		return operationError(err, "could not request verification; try again later")
	}
}
func (s *ContactVerificationService) Verify(ctx context.Context, userID, id int64, code string) error {
	if s.secret == "" {
		return NewPublicError(ErrServiceUnavailable, "contact verification is unavailable", nil)
	}
	if len(code) != 6 {
		return NewPublicError(ErrValidation, "verification code must contain six digits", nil)
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return NewPublicError(ErrValidation, "verification code must contain six digits", nil)
		}
	}
	err := s.repo.Verify(ctx, userID, id, func(challenge string) []byte { return s.digest(challenge, code) })
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return NewPublicError(ErrNotFound, "contact was not found", err)
	case errors.Is(err, repository.ErrVerificationInvalid):
		return NewPublicError(ErrValidation, "verification code is invalid or expired", err)
	default:
		return operationError(err, "could not verify contact; try again later")
	}
}
func (s *ContactVerificationService) Deliver(ctx context.Context, id string) error {
	if s.secret == "" {
		return errors.New("verification secret is not configured")
	}
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("invalid verification id")
	}
	job, err := s.repo.ClaimDelivery(ctx, id, uuid.NewString())
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var sender OperationalSender
	switch job.Channel {
	case "email":
		sender = s.email
	case "telegram":
		sender = s.telegram
	}
	if sender == nil {
		return errors.New("verification sender is not configured")
	}
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	sendErr := sender.Send(sendCtx, job.Recipient, "Contact verification", "Verification code: "+s.code(id))
	cancel()
	finishCtx, finishCancel := context.WithTimeout(ctx, 2*time.Second)
	defer finishCancel()
	if err := s.repo.FinishDelivery(finishCtx, job, sendErr == nil); err != nil {
		return err
	}
	return sendErr
}
