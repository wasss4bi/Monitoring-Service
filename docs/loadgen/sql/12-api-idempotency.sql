-- Exact SQL source: internal/repository/idempotency_repository.go, Get
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on
SELECT key FROM idempotency_keys ORDER BY (expires_at>NOW()) DESC,created_at DESC LIMIT 1 \gset
BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(varchar) AS
SELECT key, status, request_hash, response_code, response_body, user_id, ip, device, browser, created_at, expires_at
		FROM idempotency_keys
		WHERE key = $1 AND expires_at > NOW();
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(:'key');
DEALLOCATE loadgen_query;
ROLLBACK;
