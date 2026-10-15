package service

import (
	"context"
	"monitoring_service/internal/domain"
	"time"
)

type notificationRepository interface {
	ListByUser(context.Context, int64, int, *time.Time, int64) ([]domain.Notification, error)
}
type NotificationService struct{ repository notificationRepository }

func NewNotificationService(repository notificationRepository) *NotificationService {
	return &NotificationService{repository}
}
func (s *NotificationService) List(ctx context.Context, userID int64, limit int, before *time.Time, beforeID int64) ([]domain.Notification, error) {
	notifications, err := s.repository.ListByUser(ctx, userID, limit, before, beforeID)
	return notifications, operationError(err, "could not list notifications; try again later")
}
