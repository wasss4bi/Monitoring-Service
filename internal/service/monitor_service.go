package service

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"

	"monitoring_service/internal/authorization"
	"monitoring_service/internal/domain"
	"monitoring_service/internal/repository"

	"github.com/lib/pq"
)

type MonitorService struct {
	repository       *repository.MonitorRepository
	sessionProtector *authorization.Protector
	targetHostnames  *repository.TargetHostnameRepository
}

func (s *MonitorService) WithTargetHostnameRepository(repository *repository.TargetHostnameRepository) *MonitorService {
	s.targetHostnames = repository
	return s
}

func NewMonitorService(repository *repository.MonitorRepository) *MonitorService {
	return &MonitorService{repository: repository}
}

func (s *MonitorService) WithSessionProtector(p *authorization.Protector) *MonitorService {
	s.sessionProtector = p
	return s
}
func (s *MonitorService) Create(ctx context.Context, input domain.CreateMonitorInput) (domain.Monitor, error) {
	if input.SessionMode == "" {
		input.SessionMode = "managed"
	}
	if len(input.Session) > 0 {
		if input.SessionMode != "provided" || s.sessionProtector == nil {
			return domain.Monitor{}, NewPublicError(nil, "monitor session management is unavailable; try again later", errors.New("monitor session encryption is not configured"))
		}
		ciphertext, err := s.sessionProtector.Encrypt(string(input.Session))
		if err != nil {
			return domain.Monitor{}, NewPublicError(nil, "could not prepare monitor session; try again later", err)
		}
		input.SessionCiphertext = ciphertext
	}
	if input.SessionMode == "provided" && input.SessionCiphertext == "" {
		return domain.Monitor{}, NewPublicError(ErrValidation, "provided session mode requires a valid session", nil)
	}

	if s.targetHostnames != nil {
		u, err := url.Parse(input.CanonicalURL)
		if err != nil {
			return domain.Monitor{}, NewPublicError(ErrValidation, "url must be a valid HTTPS URL", err)
		}
		target, err := s.targetHostnames.FindAvailable(ctx, strings.ToLower(u.Hostname()))
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Monitor{}, NewPublicError(ErrTargetHostnameUnavailable, "hostname is not configured, inactive, or blocked", err)
		}
		if err != nil {
			return domain.Monitor{}, NewPublicError(nil, "could not check hostname availability; try again later", err)
		}
		input.TargetHostnameID = target.ID
	}
	headers, fingerprint, err := s.protectHeaders(input.Headers)
	if err != nil {
		return domain.Monitor{}, err
	}
	input.Headers = headers
	input.HeadersFingerprint = fingerprint
	monitor, err := s.repository.Create(ctx, input)
	return monitor, monitorCreationError(err)
}

func (s *MonitorService) protectHeaders(headers map[string]string) (map[string]string, string, error) {
	// Ignore custom headers even for callers outside the HTTP handler. Keep the
	// legacy columns for now; no stored credentials are deleted implicitly.
	return map[string]string{}, "", nil
}

func (s *MonitorService) List(ctx context.Context, userID int64, limit int, cursor *domain.MonitorCursor) ([]domain.Monitor, error) {
	monitors, err := s.repository.ListByUser(ctx, userID, limit, cursor)
	return monitors, operationError(err, "could not list monitors; try again later")
}

func (s *MonitorService) Get(ctx context.Context, userID, monitorID int64) (domain.Monitor, error) {
	monitor, err := s.repository.GetByUser(ctx, userID, monitorID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Monitor{}, NewPublicError(ErrNotFound, "monitor was not found", err)
	}
	return monitor, operationError(err, "could not read monitor; try again later")
}

func (s *MonitorService) AddRule(ctx context.Context, userID, monitorID int64, input domain.CreateMonitorRuleInput) (domain.MonitorRule, error) {
	rule, err := s.repository.AddRule(ctx, userID, monitorID, input)
	if errors.Is(err, repository.ErrMonitorRuleLimit) {
		return domain.MonitorRule{}, NewPublicError(ErrConflict, "monitor cannot contain more than three rules", err)
	}
	if isSerializationFailure(err) {
		return domain.MonitorRule{}, NewPublicError(ErrConflict, "resource settings changed concurrently; retry the request", err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.MonitorRule{}, NewPublicError(ErrNotFound, "monitor was not found", err)
	}
	return rule, operationError(err, "could not add monitor rule; try again later")
}

func (s *MonitorService) Stop(ctx context.Context, userID, monitorID int64) error {
	return monitorMutationError(s.repository.Stop(ctx, userID, monitorID))
}

func (s *MonitorService) Resume(ctx context.Context, userID, monitorID int64) error {
	err := s.repository.Resume(ctx, userID, monitorID)
	switch {
	case errors.Is(err, repository.ErrTargetHostnameUnavailable):
		return NewPublicError(ErrConflict, "monitor cannot be resumed while its hostname is inactive or blocked", err)
	case errors.Is(err, repository.ErrMonitorNotResumable):
		return NewPublicError(ErrConflict, "monitor cannot be resumed from its current status", err)
	default:
		return monitorMutationError(err)
	}
}

func (s *MonitorService) Delete(ctx context.Context, userID, monitorID int64) error {
	return monitorMutationError(s.repository.SoftDelete(ctx, userID, monitorID))
}

func (s *MonitorService) UpdateName(ctx context.Context, userID, monitorID int64, name string) (domain.Monitor, error) {
	monitor, err := s.repository.UpdateName(ctx, userID, monitorID, name)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Monitor{}, NewPublicError(ErrNotFound, "monitor was not found", err)
	}
	return monitor, operationError(err, "could not update monitor; try again later")
}

func (s *MonitorService) UpdateRulesCountToFinish(ctx context.Context, userID, monitorID int64, rulesCountToFinish int) (domain.Monitor, error) {
	monitor, err := s.repository.UpdateRulesCountToFinish(ctx, userID, monitorID, rulesCountToFinish)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Monitor{}, NewPublicError(ErrNotFound, "monitor was not found", err)
	}
	return monitor, operationError(err, "could not update monitor; try again later")
}

func (s *MonitorService) ReplaceSession(ctx context.Context, userID, id int64, mode string, raw []byte) (domain.Monitor, error) {
	// Verify ownership before processing the credentials.
	if _, err := s.Get(ctx, userID, id); err != nil {
		return domain.Monitor{}, err
	}
	ciphertext := ""
	if mode == "provided" {
		if s.sessionProtector == nil {
			return domain.Monitor{}, NewPublicError(nil, "monitor session management is unavailable; try again later", errors.New("monitor session encryption is not configured"))
		}
		encrypted, err := s.sessionProtector.Encrypt(string(raw))
		if err != nil {
			return domain.Monitor{}, NewPublicError(nil, "could not prepare monitor session; try again later", err)
		}
		ciphertext = encrypted
	}
	monitor, err := s.repository.ReplaceSession(ctx, userID, id, mode, ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Monitor{}, NewPublicError(ErrNotFound, "monitor was not found", err)
	}
	return monitor, operationError(err, "could not replace monitor session; try again later")
}

func monitorMutationError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return NewPublicError(ErrNotFound, "monitor was not found", err)
	}
	return operationError(err, "could not update monitor; try again later")
}

func isSerializationFailure(err error) bool {
	var postgresError *pq.Error
	return errors.As(err, &postgresError) && postgresError.Code == "40001"
}

func monitorCreationError(err error) error {
	switch {
	case errors.Is(err, repository.ErrEndpointTypeConflict):
		return NewPublicError(ErrConflict, "this URL is already configured with a different type", err)
	case errors.Is(err, repository.ErrEndpointPageConflict):
		return NewPublicError(ErrConflict, "this URL is already configured with a different browser_page_url", err)
	case errors.Is(err, repository.ErrEndpointJSONSourceConflict):
		return NewPublicError(ErrConflict, "this URL is already configured with a different json_source", err)
	case errors.Is(err, repository.ErrEndpointHeadersConflict):
		return NewPublicError(ErrConflict, "this URL is already configured with incompatible request settings", err)
	case errors.Is(err, repository.ErrTargetHostnameUnavailable):
		return NewPublicError(ErrTargetHostnameUnavailable, "hostname is not configured, inactive, or blocked", err)
	case isSerializationFailure(err):
		return NewPublicError(ErrConflict, "resource settings changed concurrently; retry the request", err)
	default:
		return operationError(err, "could not create monitor; try again later")
	}
}
