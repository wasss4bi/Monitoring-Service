package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"monitoring_service/internal/authorization"
	"monitoring_service/internal/domain"
	"monitoring_service/internal/metrics"
	"monitoring_service/internal/ratelimit"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/sender"
	rand "math/rand/v2"
	"net/http"
	"strings"
	"time"
)

const (
	monitorAttemptsTotal = 6
	monitorRetryInitial  = 10 * time.Second
	monitorRetryWindow   = 10 * time.Minute
)

type MonitoringExecutionService struct {
	repo    *repository.MonitoringExecutionRepository
	limiter interface {
		AcquireKey(context.Context, string, int, time.Duration) (time.Duration, error)
	}
	interval, lease        time.Duration
	email                  *sender.EmailSender
	telegram               *sender.TelegramSender
	targetSessionProtector *authorization.Protector
	fetcher                ExtractionClient
}

func NewMonitoringExecutionService(repo *repository.MonitoringExecutionRepository, limiter *ratelimit.RedisSlidingWindowLimiter, interval, lease time.Duration, email *sender.EmailSender, telegram *sender.TelegramSender) *MonitoringExecutionService {
	return &MonitoringExecutionService{repo: repo, limiter: ratelimit.NewMonitoringLimiter(limiter), interval: interval, lease: lease, email: email, telegram: telegram}
}
func (s *MonitoringExecutionService) ProcessFetch(ctx context.Context, fetchID, messageID string) error {
	if s.fetcher == nil {
		return fmt.Errorf("Fetcher client is not configured")
	}
	return s.processRemoteFetch(ctx, fetchID, messageID)
}
func (s *MonitoringExecutionService) WithTargetSessionProtector(protector *authorization.Protector) *MonitoringExecutionService {
	s.targetSessionProtector = protector
	return s
}
func (s *MonitoringExecutionService) Schedule(ctx context.Context) (int, error) {
	if _, err := s.repo.RecoverPendingFetches(ctx, 100); err != nil {
		return 0, fmt.Errorf("recover pending fetches: %w", err)
	}
	if _, err := s.repo.RecoverExpiredFetches(ctx, 100); err != nil {
		return 0, fmt.Errorf("recover expired fetches: %w", err)
	}
	if _, err := s.repo.RecoverExpiredNotifications(ctx, 100); err != nil {
		return 0, fmt.Errorf("recover expired notifications: %w", err)
	}
	return s.repo.ScheduleDue(ctx, s.interval, 100)
}
func retryableFetchStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
func compare(actual float64, op string, expected float64) bool {
	switch op {
	case "<":
		return actual < expected
	case "<=":
		return actual <= expected
	case "=":
		return actual == expected
	case "!=":
		return actual != expected
	case ">=":
		return actual >= expected
	case ">":
		return actual > expected
	}
	return false
}
func (s *MonitoringExecutionService) ProcessNotification(ctx context.Context, id int64, messageID string) error {
	processed, err := s.repo.IsProcessed(ctx, repository.NotificationConsumer, messageID)
	if err != nil {
		return err
	}
	if processed {
		return nil
	}
	job, err := s.repo.ClaimNotification(ctx, id, s.lease)
	if errors.Is(err, sql.ErrNoRows) {
		return s.repo.SkipNotification(ctx, id, messageID)
	}
	if err != nil {
		return err
	}
	var sendErr error
	sendCtx, cancelSend := context.WithTimeout(ctx, 10*time.Second)
	switch strings.ToLower(job.Channel) {
	case "email":
		sendErr = s.email.Send(sendCtx, job.Contact, "Monitor result", job.Notification.Message)
	case "telegram":
		sendErr = s.telegram.Send(sendCtx, job.Contact, "", job.Notification.Message)
	default:
		sendErr = fmt.Errorf("unsupported notification channel %q", job.Channel)
	}
	cancelSend()
	if sendErr == nil {
		metrics.ObserveMonitorNotification("sent")
		return s.repo.CompleteNotification(ctx, id, job.LeaseToken, messageID, "", 0, false)
	}
	metrics.ObserveMonitorNotification("failed")
	if isPermanentNotificationError(sendErr) {
		metrics.ObserveMonitorNotification("permanent_failure")
		return s.repo.DisableContactForPermanentNotificationFailure(ctx, id, job.LeaseToken, job.Contact, job.ContactUpdatedAt, messageID, sendErr.Error())
	}
	retry := job.Notification.AttemptsCount < monitorAttemptsTotal && time.Since(job.Notification.CreatedAt) < monitorRetryWindow
	delay := time.Duration(0)
	if retry {
		delay = fullJitterRetryDelay(int64(job.Notification.AttemptsCount))
	}
	return s.repo.CompleteNotification(ctx, id, job.LeaseToken, messageID, sendErr.Error(), delay, retry)
}
func fullJitterRetryDelay(attempt int64) time.Duration {
	if attempt < 1 {
		return 0
	}
	cap := monitorRetryInitial << (attempt - 1)
	if cap > monitorRetryWindow {
		cap = monitorRetryWindow
	}
	return time.Duration(rand.Int64N(int64(cap) + 1))
}
func isPermanentNotificationError(err error) bool {
	var statusError *sender.HTTPStatusError
	if !errors.As(err, &statusError) {
		return false
	}
	switch statusError.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusMethodNotAllowed, http.StatusConflict,
		http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}
func monitorProfileScope(job domain.FetchJob) string {
	return fmt.Sprintf("monitor-%d-%d", job.MonitorID, job.ProfileGeneration)
}
func statusPointer(status int) *int {
	if status < 100 || status > 599 {
		return nil
	}
	return &status
}
