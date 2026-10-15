package repository

import (
	"context"
	"encoding/json"
)

func (r *MonitoringExecutionRepository) RecoverPendingFetches(ctx context.Context, limit int) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT f.fetch_id::text,m.id,e.type FROM monitors m JOIN endpoint_fetches f ON f.monitor_id=m.id
 JOIN monitored_endpoints e ON e.id=m.endpoint_id WHERE f.status='pending' AND m.status='monitoring' AND m.deleted_at IS NULL
 AND m.check_status='scheduled' AND m.generation=f.generation AND COALESCE(f.next_dispatch_at,f.created_at+INTERVAL '30 seconds')<NOW()
 AND f.lease_token IS NULL
 AND NOT EXISTS (SELECT 1 FROM outbox_events o WHERE o.aggregate_type='monitor' AND o.aggregate_id=m.id
   AND o.published_at IS NULL AND o.payload->>'fetch_id'=f.fetch_id::text
   AND o.event_type IN ('endpoint.fetch.json','endpoint.fetch.html'))
 ORDER BY f.next_dispatch_at NULLS FIRST,f.fetch_id FOR UPDATE OF m SKIP LOCKED LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	type job struct {
		id      string
		monitor int64
		kind    string
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.monitor, &j.kind); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, j := range jobs {
		result, err := tx.ExecContext(ctx, `UPDATE endpoint_fetches SET next_dispatch_at=NOW()+INTERVAL '30 seconds' WHERE fetch_id=$1::uuid AND status='pending' AND lease_token IS NULL`, j.id)
		if err != nil {
			return 0, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		if n == 0 {
			continue
		}
		event, err := endpointFetchEvent(j.kind)
		if err != nil {
			return 0, err
		}
		payload, _ := json.Marshal(map[string]string{"fetch_id": j.id})
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload,next_attempt_at) VALUES($1,'monitor',$2,$3,NOW())`, event, j.monitor, payload); err != nil {
			return 0, err
		}
	}
	return len(jobs), tx.Commit()
}
