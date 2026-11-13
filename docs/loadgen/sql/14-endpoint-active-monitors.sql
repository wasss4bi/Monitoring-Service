-- Optional repository-only baseline: no current runtime callers found.
-- Exact SQL source: internal/repository/monitoring_execution_repository.go, SwitchEndpointType
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on
SELECT endpoint_id FROM monitors WHERE status='monitoring' AND deleted_at IS NULL ORDER BY id LIMIT 1 \gset
BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(bigint) AS
SELECT id,user_id,name FROM monitors WHERE endpoint_id=$1 AND status='monitoring' AND deleted_at IS NULL ORDER BY id FOR UPDATE;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(:endpoint_id);
DEALLOCATE loadgen_query;
ROLLBACK;
