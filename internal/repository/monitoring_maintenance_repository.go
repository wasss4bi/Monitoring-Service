package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const monitoringRetention = 7 * 24 * time.Hour

type MonitoringMaintenanceRepository struct{ db *sql.DB }

func NewMonitoringMaintenanceRepository(db *sql.DB) *MonitoringMaintenanceRepository {
	return &MonitoringMaintenanceRepository{db: db}
}

type MonitoringCleanupResult struct {
	EndpointFetches   int64
	ProcessedMessages int64
	Notifications     int64
}

// DeleteExpired removes at most batchSize rows per table. Outbox events are deliberately retained.
func (r *MonitoringMaintenanceRepository) DeleteExpired(ctx context.Context, batchSize int) (MonitoringCleanupResult, error) {
	if batchSize < 1 {
		return MonitoringCleanupResult{}, fmt.Errorf("cleanup batch size must be positive")
	}
	cutoff := time.Now().Add(-monitoringRetention)
	result := MonitoringCleanupResult{}
	var err error
	if result.EndpointFetches, err = r.deleteBatch(ctx, `DELETE FROM endpoint_fetches WHERE fetch_id IN (SELECT fetch_id FROM endpoint_fetches WHERE status IN ('succeeded','failed') AND finished_at < $1 ORDER BY finished_at LIMIT $2)`, cutoff, batchSize); err != nil {
		return result, fmt.Errorf("delete expired endpoint fetches: %w", err)
	}
	if result.ProcessedMessages, err = r.deleteBatch(ctx, `DELETE FROM processed_messages WHERE (consumer_name, message_id) IN (SELECT consumer_name, message_id FROM processed_messages WHERE processed_at < $1 ORDER BY processed_at LIMIT $2)`, cutoff, batchSize); err != nil {
		return result, fmt.Errorf("delete expired processed messages: %w", err)
	}
	if result.Notifications, err = r.deleteBatch(ctx, `DELETE FROM notifications WHERE id IN (SELECT id FROM notifications WHERE status IN ('sent','failed','skipped') AND created_at < $1 ORDER BY created_at LIMIT $2)`, cutoff, batchSize); err != nil {
		return result, fmt.Errorf("delete expired notifications: %w", err)
	}
	return result, nil
}

func (r *MonitoringMaintenanceRepository) deleteBatch(ctx context.Context, query string, cutoff time.Time, batchSize int) (int64, error) {
	result, err := r.db.ExecContext(ctx, query, cutoff, batchSize)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
