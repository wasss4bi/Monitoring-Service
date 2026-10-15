package handler

import (
	"encoding/json"
	"time"

	"monitoring_service/internal/domain"
)

type createTargetHostnameRequest struct {
	BootstrapRules json.RawMessage `json:"bootstrap_rules,omitempty"`
	Hostname       string          `json:"hostname"`
	BootstrapURL   *string         `json:"bootstrap_url,omitempty"`
	LimitPerMinute int             `json:"limit_per_minute"`
	IsActive       *bool           `json:"is_active,omitempty"`
}

type updateTargetHostnameRequest struct {
	BootstrapRules json.RawMessage `json:"bootstrap_rules,omitempty"`
	BootstrapURL   json.RawMessage `json:"bootstrap_url,omitempty"`
	LimitPerMinute *int            `json:"limit_per_minute,omitempty"`
	IsActive       *bool           `json:"is_active,omitempty"`
}

type targetHostnameResponse struct {
	BootstrapRules    json.RawMessage `json:"bootstrap_rules"`
	ID                int64           `json:"id"`
	Hostname          string          `json:"hostname"`
	BootstrapURL      *string         `json:"bootstrap_url"`
	LimitPerMinute    int             `json:"limit_per_minute"`
	IsActive          bool            `json:"is_active"`
	HasSession        bool            `json:"has_session"`
	SessionStatus     string          `json:"session_status"`
	SessionGeneration int64           `json:"session_generation"`
	SessionUpdatedAt  *time.Time      `json:"session_updated_at"`
	LastSessionError  *string         `json:"last_session_error"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

func toTargetHostnameResponse(target domain.TargetHostname) targetHostnameResponse {
	return targetHostnameResponse{
		BootstrapRules: target.BootstrapRules,
		ID:             target.ID, Hostname: target.Hostname,
		BootstrapURL: target.BootstrapURL, LimitPerMinute: target.LimitPerMinute, IsActive: target.IsActive,
		HasSession: target.SessionStatus == domain.TargetSessionReady, SessionStatus: target.SessionStatus,
		SessionGeneration: target.SessionGeneration, SessionUpdatedAt: target.SessionUpdatedAt,
		LastSessionError: target.LastSessionError, CreatedAt: target.CreatedAt, UpdatedAt: target.UpdatedAt,
	}
}

type userResponse struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type createUserRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type updateUserRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type checkPasswordRequest struct {
	Password string `json:"password"`
}

type checkPasswordResponse struct {
	Valid bool `json:"valid"`
}

type contactRequest struct {
	Name string `json:"name"`
}

type contactUpdateRequest struct {
	Name string `json:"name"`
}

type userContactRequest struct {
	ContactID int64  `json:"contact_id"`
	Contact   string `json:"contact"`
}

type userContactUpdateRequest struct {
	Contact  string `json:"contact"`
	IsActive bool   `json:"is_active"`
}

type contactResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type userContactResponse struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	ContactID  int64     `json:"contact_id"`
	Contact    string    `json:"contact"`
	Channel    string    `json:"channel"`
	IsVerified bool      `json:"is_verified"`
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type monitorRuleRequest struct {
	Description  string             `json:"description"`
	Source       *domain.RuleSource `json:"source,omitempty"`
	NumberFormat string             `json:"number_format,omitempty"`
	Path         string             `json:"path"`
	Operator     string             `json:"operator"`
	Value        *float64           `json:"value"`
}

type createMonitorRequest struct {
	BrowserPageURL     string             `json:"browser_page_url,omitempty"`
	SessionMode        string             `json:"session_mode,omitempty"`
	Session            json.RawMessage    `json:"session,omitempty"`
	JSONSource         *domain.JSONSource `json:"json_source,omitempty"`
	Name               string             `json:"name"`
	PageURL            string             `json:"page_url"`
	URL                string             `json:"url"`
	Type               string             `json:"type"`
	PathType           string             `json:"path_type"`
	RulesCountToFinish *int               `json:"rules_count_to_finish,omitempty"`
	// Custom upstream headers are disabled. Unknown JSON fields are rejected.
	Rules []monitorRuleRequest `json:"rules"`
}

type updateMonitorRequest struct {
	SessionMode        string          `json:"session_mode,omitempty"`
	Session            json.RawMessage `json:"session,omitempty"`
	Name               *string         `json:"name,omitempty"`
	RulesCountToFinish *int            `json:"rules_count_to_finish,omitempty"`
}

type monitorRuleResponse struct {
	Description  string             `json:"description"`
	Source       *domain.RuleSource `json:"source,omitempty"`
	NumberFormat string             `json:"number_format"`
	ID           int64              `json:"id"`
	Path         string             `json:"path"`
	Operator     string             `json:"operator"`
	Value        float64            `json:"value"`
	CreatedAt    time.Time          `json:"created_at"`
}

type monitorResponse struct {
	BrowserPageURL     string                `json:"browser_page_url,omitempty"`
	SessionMode        string                `json:"session_mode"`
	SessionStatus      string                `json:"session_status"`
	HasSession         bool                  `json:"has_session"`
	JSONSource         *domain.JSONSource    `json:"json_source,omitempty"`
	ID                 int64                 `json:"id"`
	PageURL            string                `json:"page_url"`
	Name               string                `json:"name"`
	URL                string                `json:"url"`
	Type               string                `json:"type"`
	PathType           string                `json:"path_type"`
	RulesCountToFinish int                   `json:"rules_count_to_finish"`
	Status             string                `json:"status"`
	LastCheckedAt      *time.Time            `json:"last_checked_at,omitempty"`
	LastError          *string               `json:"last_error,omitempty"`
	StoppedAt          *time.Time            `json:"stopped_at,omitempty"`
	CreatedAt          time.Time             `json:"created_at"`
	UpdatedAt          time.Time             `json:"updated_at"`
	Rules              []monitorRuleResponse `json:"rules"`
}
type monitorListResponse struct {
	Items      []monitorResponse `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}
type errorResponse struct {
	Error errorBody `json:"error"`
}
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type validationErrorResponse struct {
	Error validationErrorBody `json:"error"`
}
type validationErrorBody struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Fields  map[string][]string `json:"fields"`
}

func toUserResponse(u domain.User) userResponse {
	return userResponse{
		ID:    u.ID,
		Name:  u.Name,
		Email: u.Email,
	}
}
func toCheckPasswordResponse(cpo domain.CheckPasswordOutput) checkPasswordResponse {
	return checkPasswordResponse{
		Valid: cpo.Valid,
	}
}

func toContactResponse(contact domain.Contact) contactResponse {
	return contactResponse{ID: contact.ID, Name: contact.Name}
}

func toContactResponses(contacts []domain.Contact) []contactResponse {
	responses := make([]contactResponse, 0, len(contacts))
	for _, contact := range contacts {
		responses = append(responses, toContactResponse(contact))
	}
	return responses
}

func toUserContactResponse(contact domain.UserContact) userContactResponse {
	return userContactResponse{
		ID: contact.ID, UserID: contact.UserID, ContactID: contact.ContactID, Contact: contact.Contact,
		Channel: contact.Channel, IsVerified: contact.IsVerified, IsActive: contact.IsActive,
		CreatedAt: contact.CreatedAt, UpdatedAt: contact.UpdatedAt,
	}
}

func toUserContactResponses(contacts []domain.UserContact) []userContactResponse {
	responses := make([]userContactResponse, 0, len(contacts))
	for _, contact := range contacts {
		responses = append(responses, toUserContactResponse(contact))
	}
	return responses
}
func toUserResponses(users []domain.User) []userResponse {
	responses := make([]userResponse, 0, len(users))
	for _, u := range users {
		responses = append(responses, toUserResponse(u))
	}
	return responses
}

func toMonitorRuleResponse(rule domain.MonitorRule) monitorRuleResponse {
	return monitorRuleResponse{Description: rule.Description, Source: rule.Source, NumberFormat: rule.NumberFormat, ID: rule.ID, Path: rule.JSONPath, Operator: rule.ComparisonOperator, Value: rule.ExpectedValue, CreatedAt: rule.CreatedAt}
}

func toMonitorResponse(monitor domain.Monitor) monitorResponse {
	rules := make([]monitorRuleResponse, 0, len(monitor.Rules))
	for _, rule := range monitor.Rules {
		rules = append(rules, toMonitorRuleResponse(rule))
	}
	return monitorResponse{BrowserPageURL: monitor.BrowserPageURL, SessionMode: monitor.SessionMode, SessionStatus: monitor.SessionStatus, HasSession: monitor.HasSession, ID: monitor.ID, JSONSource: monitor.JSONSource, PageURL: monitor.PageURL, Name: monitor.Name, URL: monitor.URL, Type: monitor.EndpointType, PathType: monitor.PathType, RulesCountToFinish: monitor.RulesCountToFinish, Status: monitor.Status, LastCheckedAt: monitor.LastCheckedAt, LastError: monitor.LastError, StoppedAt: monitor.StoppedAt, CreatedAt: monitor.CreatedAt, UpdatedAt: monitor.UpdatedAt, Rules: rules}
}
