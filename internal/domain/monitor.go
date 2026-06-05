package domain

import (
	"encoding/json"
	"time"
)

const (
	EndpointTypeJSON = "json"
	EndpointTypeHTML = "html"

	EndpointStatusIdle         = "idle"
	EndpointStatusScheduled    = "scheduled"
	EndpointStatusFetching     = "fetching"
	EndpointStatusRetryWait    = "retry_wait"
	EndpointStatusError        = "error"
	EndpointStatusAccessDenied = "access_denied"

	MonitorStatusMonitoring = "monitoring"
	MonitorStatusSuccess    = "success"
	MonitorStatusError      = "error"
	MonitorStatusStopped    = "stopped"

	FetchStatusPending   = "pending"
	FetchStatusFetching  = "fetching"
	FetchStatusSucceeded = "succeeded"
	FetchStatusFailed    = "failed"

	NotificationStatusPending = "pending"
	NotificationStatusSending = "sending"
	NotificationStatusSent    = "sent"
	NotificationStatusFailed  = "failed"
	NotificationStatusSkipped = "skipped"
)

// JSONSource selects an object in an inline HTML script without executing it.
type JSONSource struct {
	ScriptContains string          `json:"script_contains"`
	ObjectKey      string          `json:"object_key"`
	Match          JSONSourceMatch `json:"match"`
}

type JSONSourceMatch struct {
	Path  string `json:"path"`
	Value string `json:"value"`
}

type MonitoredEndpoint struct {
	JSONSource        *JSONSource
	ID                int64
	BrowserPageURL    string
	CanonicalURL      string
	Type              string
	Headers           map[string]string
	Status            string
	NextCheckAt       *time.Time
	LastAttemptAt     *time.Time
	LastSuccessAt     *time.Time
	LastCheckedAt     *time.Time
	AttemptsCount     int64
	ConsecutiveErrors int
	RetryStartedAt    *time.Time
	LastError         *string
	LeaseToken        *string
	LeaseUntil        *time.Time
	Generation        int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Monitor struct {
	BrowserPageURL     string
	SessionMode        string
	SessionStatus      string
	HasSession         bool
	JSONSource         *JSONSource
	ID                 int64
	PageURL            string
	UserID             int64
	EndpointID         int64
	URL                string
	Name               string
	EndpointType       string
	Headers            map[string]string
	PathType           string
	RulesCountToFinish int
	Status             string
	LastCheckedAt      *time.Time
	LastError          *string
	CompletedAt        *time.Time
	StoppedAt          *time.Time
	DeletedAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Rules              []MonitorRule
}

type RuleSource struct {
	Path       string `json:"path"`
	KeyPrefix  string `json:"key_prefix"`
	DecodeJSON bool   `json:"decode_json"`
}

type MonitorRule struct {
	Description        string
	Source             *RuleSource
	NumberFormat       string
	ID                 int64
	MonitorID          int64
	JSONPath           string
	ComparisonOperator string
	ExpectedValue      float64
	CreatedAt          time.Time
}

type CreateMonitorInput struct {
	IdempotencyKey     string
	IdempotencyHash    string
	BrowserPageURL     string
	SessionMode        string
	Session            json.RawMessage
	SessionCiphertext  string
	JSONSource         *JSONSource
	UserID             int64
	PageURL            string
	Name               string
	CanonicalURL       string
	EndpointType       string
	PathType           string
	RulesCountToFinish int
	Headers            map[string]string
	HeadersFingerprint string
	TargetHostnameID   int64
	Rules              []CreateMonitorRuleInput
}

type CreateMonitorRuleInput struct {
	Description        string
	Source             *RuleSource
	NumberFormat       string
	JSONPath           string
	ComparisonOperator string
	ExpectedValue      float64
}

type MonitorCursor struct {
	CreatedAt time.Time
	ID        int64
}

type EndpointFetch struct {
	FetchID        string
	EndpointID     int64
	Generation     int64
	ScheduledAt    time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	Status         string
	LeaseToken     *string
	LeaseUntil     *time.Time
	ResponseStatus *int
	ResponseJSON   json.RawMessage
	LastError      *string
	CreatedAt      time.Time
}

type Notification struct {
	ID             int64
	MonitorID      int64
	UserContactID  *int64
	EventType      string
	Message        string
	Status         string
	AttemptsCount  int
	LastError      *string
	NextAttemptAt  time.Time
	CreatedAt      time.Time
	SentAt         *time.Time
	IdempotencyKey string
}

type OutboxEvent struct {
	ID            int64
	EventID       string
	EventType     string
	AggregateType string
	AggregateID   int64
	Payload       json.RawMessage
}

type FetchJob struct {
	BrowserPageURL     string
	ProfileGeneration  int64
	MonitorID          int64
	Generation         int64
	SessionMode        string
	JSONSource         *JSONSource
	FetchID            string
	PageURL            string
	EndpointID         int64
	CanonicalURL       string
	EndpointType       string
	Headers            map[string]string
	PathType           string
	RulesCountToFinish int
	AttemptsCount      int64
	TargetHostnameID   int64
	TargetHostname     string
	TargetLimitPerMin  int
	BootstrapURL       string
	SessionCiphertext  string
	SessionGeneration  int64
}

const (
	TargetSessionMissing    = "missing"
	TargetSessionReady      = "ready"
	TargetSessionRefreshing = "refreshing"
	TargetSessionBlocked    = "blocked"
)

type TargetHostname struct {
	BootstrapRules    json.RawMessage
	ID                int64
	Hostname          string
	BootstrapURL      *string
	LimitPerMinute    int
	IsActive          bool
	SessionStatus     string
	SessionGeneration int64
	SessionUpdatedAt  *time.Time
	LastSessionError  *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type CreateTargetHostnameInput struct {
	BootstrapRules json.RawMessage
	Hostname       string
	BootstrapURL   *string
	LimitPerMinute int
	IsActive       bool
}

type UpdateTargetHostnameInput struct {
	BootstrapRules json.RawMessage
	BootstrapURL   *string
	ClearBootstrap bool
	LimitPerMinute *int
	IsActive       *bool
}

type NotificationJob struct {
	Notification     Notification
	Channel          string
	Contact          string
	ContactUpdatedAt time.Time
	LeaseToken       string
}
