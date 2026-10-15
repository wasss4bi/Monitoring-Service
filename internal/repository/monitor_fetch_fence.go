package repository

import (
	"context"
	"database/sql"
)

// The subscriber lock fences replacement/stop/delete and always precedes its job lock.
func lockOwnedFetch(ctx context.Context, tx *sql.Tx, fetchID, token string) (int64, error) {
	var owner int64
	err := tx.QueryRowContext(ctx, `SELECT m.id FROM monitors m JOIN endpoint_fetches f ON f.monitor_id=m.id
 WHERE f.fetch_id=$1::uuid AND f.lease_token=$2::uuid AND f.status='fetching' AND f.lease_until>NOW()
 AND f.generation=m.generation AND m.deleted_at IS NULL FOR UPDATE OF m`, fetchID, token).Scan(&owner)
	if err != nil {
		return 0, err
	}
	err = tx.QueryRowContext(ctx, `SELECT monitor_id FROM endpoint_fetches WHERE fetch_id=$1::uuid
 AND lease_token=$2::uuid AND lease_until>NOW() AND status='fetching' FOR UPDATE`, fetchID, token).Scan(&owner)
	return owner, err
}
