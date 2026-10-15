package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"

	"monitoring_service/internal/domain"
	"monitoring_service/internal/repository"
)

type TargetHostnameService struct {
	repository *repository.TargetHostnameRepository
}

func NewTargetHostnameService(repository *repository.TargetHostnameRepository) *TargetHostnameService {
	return &TargetHostnameService{repository: repository}
}

func (s *TargetHostnameService) Create(ctx context.Context, input domain.CreateTargetHostnameInput) (domain.TargetHostname, error) {
	target, err := s.repository.Create(ctx, input)
	if isUniqueViolation(err) {
		return domain.TargetHostname{}, NewPublicError(ErrConflict, "hostname is already configured", err)
	}
	return target, operationError(err, "could not create hostname configuration; try again later")
}

func (s *TargetHostnameService) List(ctx context.Context) ([]domain.TargetHostname, error) {
	targets, err := s.repository.List(ctx)
	return targets, operationError(err, "could not list hostnames; try again later")
}

func (s *TargetHostnameService) Get(ctx context.Context, id int64) (domain.TargetHostname, error) {
	target, err := s.repository.Get(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TargetHostname{}, NewPublicError(ErrNotFound, "hostname configuration was not found", err)
	}
	return target, operationError(err, "could not read hostname configuration; try again later")
}

func (s *TargetHostnameService) Update(ctx context.Context, id int64, input domain.UpdateTargetHostnameInput) (domain.TargetHostname, error) {
	target, err := s.repository.Update(ctx, id, input)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TargetHostname{}, NewPublicError(ErrNotFound, "hostname configuration was not found", err)
	}
	return target, operationError(err, "could not update hostname configuration; try again later")
}

func (s *TargetHostnameService) Delete(ctx context.Context, id int64) error {
	err := s.repository.Delete(ctx, id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return NewPublicError(ErrNotFound, "hostname configuration was not found", err)
	case errors.Is(err, repository.ErrTargetHostnameReferenced):
		return NewPublicError(ErrConflict, "hostname cannot be deleted while monitors use it", err)
	default:
		return operationError(err, "could not delete hostname configuration; try again later")
	}
}

func (s *TargetHostnameService) ResetSession(ctx context.Context, id int64) (domain.TargetHostname, error) {
	target, err := s.repository.ResetSession(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TargetHostname{}, NewPublicError(ErrNotFound, "hostname configuration was not found", err)
	}
	return target, operationError(err, "could not reset session for hostname configuration; try again later")
}

func isUniqueViolation(err error) bool {
	var postgresError *pq.Error
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}
