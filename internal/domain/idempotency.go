package domain

import "time"

const (
	IdempotencyStatusProcessing = "processing"
	IdempotencyStatusCompleted  = "completed"
)

type IdempotencyEntry struct {
	MonitorID    *int64
	Key          string
	Status       string
	RequestHash  string
	ResponseCode int
	ResponseBody []byte
	UserID       *int64
	IP           string
	Device       string
	Browser      string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}
