package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"
)

//go:embed validation.sql
var validationSQL string

func check(ctx context.Context, c *sql.Conn, cfg config) (map[string]int64, error) {
	checks := map[string]int64{}
	rows, err := c.QueryContext(ctx, validationSQL, cfg.AsOf)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		var n int64
		if err = rows.Scan(&name, &n); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		checks[name] = n
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return nil, err
	}
	// Derive every FK and unique index from the current catalog, including partial legacy uniqueness.
	for _, catalog := range []string{foreignKeysSQL, uniquesSQL, notNullSQL} {
		rows, err = c.QueryContext(ctx, catalog)
		if err != nil {
			return nil, err
		}
		type query struct{ name, sql string }
		var queries []query
		for rows.Next() {
			var q query
			if err = rows.Scan(&q.name, &q.sql); err != nil {
				return nil, errors.Join(err, rows.Close())
			}
			queries = append(queries, q)
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return nil, err
		}
		for _, q := range queries {
			var n int64
			if err = c.QueryRowContext(ctx, q.sql).Scan(&n); err != nil {
				return nil, fmt.Errorf("%s: %w", q.name, err)
			}
			checks[q.name] = n
		}
	}
	for name, n := range checks {
		if n != 0 {
			return checks, fmt.Errorf("integrity check %s found %d violations", name, n)
		}
	}
	return checks, nil
}

const foreignKeysSQL = `
 SELECT 'fk:'||con.conname,
 'SELECT count(*) FROM '||con.conrelid::regclass||' a WHERE '||
 string_agg(format('a.%I IS NOT NULL',a.attname),' AND ' ORDER BY k.ord)||
 ' AND NOT EXISTS (SELECT 1 FROM '||con.confrelid::regclass||' b WHERE '||
 string_agg(format('a.%I=b.%I',a.attname,b.attname),' AND ' ORDER BY k.ord)||')'
 FROM pg_constraint con
 JOIN pg_namespace ns ON ns.oid=con.connamespace AND ns.nspname='public'
 CROSS JOIN LATERAL unnest(con.conkey,con.confkey) WITH ORDINALITY k(ak,bk,ord)
 JOIN pg_attribute a ON a.attrelid=con.conrelid AND a.attnum=k.ak
 JOIN pg_attribute b ON b.attrelid=con.confrelid AND b.attnum=k.bk
 WHERE con.contype='f' GROUP BY con.oid`
const uniquesSQL = `
 SELECT 'unique:'||idx.indexrelid::regclass,
 'SELECT count(*) FROM (SELECT 1 FROM '||idx.indrelid::regclass||' WHERE '||
 COALESCE(pg_get_expr(idx.indpred,idx.indrelid),'true')||' AND '||
 string_agg(format('%I IS NOT NULL',a.attname),' AND ' ORDER BY k.ord)||
 ' GROUP BY '||string_agg(format('%I',a.attname),',' ORDER BY k.ord)||' HAVING count(*)>1) duplicate'
 FROM pg_index idx JOIN pg_class t ON t.oid=idx.indrelid
 JOIN pg_namespace ns ON ns.oid=t.relnamespace AND ns.nspname='public'
 CROSS JOIN LATERAL unnest(idx.indkey) WITH ORDINALITY k(attnum,ord)
 JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=k.attnum
 WHERE idx.indisunique AND t.relname<>'schema_migrations' AND k.ord<=idx.indnkeyatts
 GROUP BY idx.indexrelid,idx.indrelid,pg_get_expr(idx.indpred,idx.indrelid)`
const notNullSQL = `
 SELECT 'not-null:'||t.relname,
 'SELECT count(*) FROM '||t.oid::regclass||' WHERE '||string_agg(format('%I IS NULL',a.attname),' OR ' ORDER BY a.attnum)
 FROM pg_class t JOIN pg_namespace ns ON ns.oid=t.relnamespace AND ns.nspname='public'
 JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum>0 AND NOT a.attisdropped AND a.attnotnull
 WHERE t.relkind='r' AND t.relname<>'schema_migrations' GROUP BY t.oid`

func distributions(ctx context.Context, c *sql.Conn, asOf time.Time) (map[string]map[string]int64, error) {
	result := map[string]map[string]int64{}
	queries := map[string]string{
		"endpoint_status":         `SELECT status,count(*) FROM monitored_endpoints GROUP BY status`,
		"monitor_status":          `SELECT status,count(*) FROM monitors GROUP BY status`,
		"monitor_check_status":    `SELECT check_status,count(*) FROM monitors GROUP BY check_status`,
		"fetch_status":            `SELECT status,count(*) FROM endpoint_fetches GROUP BY status`,
		"notification_status":     `SELECT status,count(*) FROM notifications GROUP BY status`,
		"outbox_type":             `SELECT event_type,count(*) FROM outbox_events GROUP BY event_type`,
		"outbox_publication":      `SELECT CASE WHEN published_at IS NULL THEN 'unpublished' ELSE 'published' END,count(*) FROM outbox_events GROUP BY 1`,
		"consumers":               `SELECT consumer_name,count(*) FROM processed_messages GROUP BY consumer_name`,
		"endpoint_due":            `SELECT CASE WHEN next_check_at IS NULL THEN 'null' WHEN next_check_at <= $1 THEN 'due' ELSE 'future' END,count(*) FROM monitored_endpoints GROUP BY 1`,
		"monitor_due":             `SELECT CASE WHEN next_check_at IS NULL THEN 'null' WHEN next_check_at <= $1 THEN 'due' ELSE 'future' END,count(*) FROM monitors GROUP BY 1`,
		"scheduler_candidates":    `SELECT 'eligible',count(*) FROM monitors m JOIN monitored_endpoints e ON e.id=m.endpoint_id JOIN target_hostnames t ON t.id=e.target_hostname_id WHERE m.check_status IN ('idle','retry_wait') AND m.status='monitoring' AND m.deleted_at IS NULL AND t.is_active AND (m.next_check_at IS NULL OR m.next_check_at <= $1) AND EXISTS(SELECT 1 FROM user_contacts uc WHERE uc.user_id=m.user_id AND uc.is_active AND uc.is_verified)`,
		"worker_candidates":       `SELECT 'claimable_fetches',count(*) FROM endpoint_fetches f JOIN monitors m ON m.id=f.monitor_id JOIN monitored_endpoints e ON e.id=f.endpoint_id JOIN target_hostnames t ON t.id=e.target_hostname_id WHERE f.status='pending' AND m.check_status='scheduled' AND m.status='monitoring' AND m.deleted_at IS NULL AND m.generation=f.generation AND t.is_active`,
		"outbox_candidates":       `SELECT 'claimable_events',count(*) FROM outbox_events WHERE published_at IS NULL AND next_attempt_at <= $1 AND (lease_until IS NULL OR lease_until < $1)`,
		"notification_candidates": `SELECT 'claimable_notifications',count(*) FROM notifications n JOIN user_contacts uc ON uc.id=n.user_contact_id WHERE (n.status='pending' OR (n.status='sending' AND n.next_attempt_at<$1)) AND uc.is_active AND uc.is_verified`,
	}
	for name, q := range queries {
		var args []any
		if strings.Contains(q, "$1") {
			args = []any{asOf}
		}
		rows, err := c.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		values := map[string]int64{}
		for rows.Next() {
			var state string
			var n int64
			if err = rows.Scan(&state, &n); err != nil {
				return nil, errors.Join(err, rows.Close())
			}
			values[state] = n
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return nil, err
		}
		result[name] = values
	}
	return result, nil
}
