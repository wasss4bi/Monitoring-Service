package repository

import (
	"context"
	"database/sql"
	"fmt"
)

type ProcessedMessageRepository struct{}

func NewProcessedMessageRepository() *ProcessedMessageRepository {
	return &ProcessedMessageRepository{}
}

// RecordOnce must run in the same transaction as the message's business changes.
func (r *ProcessedMessageRepository) RecordOnce(ctx context.Context, tx *sql.Tx, consumerName, messageID string) (bool, error) {
	if tx == nil || consumerName == "" || messageID == "" {
		return false, fmt.Errorf("transaction, consumer name and message id are required")
	}
	result, err := tx.ExecContext(ctx, `
        INSERT INTO processed_messages (consumer_name, message_id)
        VALUES ($1, $2::uuid)
        ON CONFLICT (consumer_name, message_id) DO NOTHING
    `, consumerName, messageID)
	if err != nil {
		return false, fmt.Errorf("record processed message: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func (r *ProcessedMessageRepository) IsProcessed(ctx context.Context, db *sql.DB, consumerName, messageID string) (bool, error) {
	if consumerName == "" || messageID == "" {
		return false, fmt.Errorf("consumer name and message id are required")
	}
	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM processed_messages WHERE consumer_name=$1 AND message_id=$2::uuid)`, consumerName, messageID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check processed message: %w", err)
	}
	return exists, nil
}
