package service

import (
	"context"
	"database/sql"
	"errors"
	"monitoring_service/internal/repository"
	"time"
)

type OperationalSender interface {
	Send(context.Context, string, string, string) error
}
type OperationalNotifications struct {
	Repository *repository.DependencyRepository
	Sender     OperationalSender
}

func (s *OperationalNotifications) Process(ctx context.Context, id int64, messageID string) error {
	processed, err := s.Repository.IsAlertProcessed(ctx, messageID)
	if err != nil || processed {
		return err
	}
	job, err := s.Repository.ClaimAlert(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = s.Sender.Send(sendCtx, job.Recipient, "Fetcher infrastructure", job.Message)
	cancel()
	finalize, done := context.WithTimeout(ctx, 2*time.Second)
	defer done()
	return s.Repository.FinishAlert(finalize, job, messageID, err == nil)
}
