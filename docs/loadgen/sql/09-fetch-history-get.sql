-- Optional repository-only baseline: no current runtime callers found.
-- Exact SQL source: internal/repository/endpoint_fetch_repository.go, Get
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on
SELECT fetch_id::text AS fetch_id FROM endpoint_fetches ORDER BY created_at DESC LIMIT 1 \gset
BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(uuid) AS
SELECT fetch_id::text, endpoint_id, generation, scheduled_at, started_at,
               finished_at, status, lease_token::text, lease_until, response_status,
               response_json, last_error, created_at
        FROM endpoint_fetches
        WHERE fetch_id = $1::uuid;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(:'fetch_id');
DEALLOCATE loadgen_query;
ROLLBACK;
