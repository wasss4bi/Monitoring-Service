-- Exact SQL source: internal/repository/monitoring_execution_repository.go, ClaimFetch
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on
SELECT f.fetch_id::text AS fetch_id FROM endpoint_fetches f JOIN monitors m ON m.id=f.monitor_id JOIN monitored_endpoints e ON e.id=f.endpoint_id JOIN target_hostnames t ON t.id=e.target_hostname_id WHERE f.status='pending' AND m.check_status='scheduled' AND m.status='monitoring' AND m.deleted_at IS NULL AND f.generation=m.generation AND t.is_active ORDER BY f.scheduled_at LIMIT 1 \gset
BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(uuid) AS
SELECT m.id FROM monitors m JOIN endpoint_fetches f ON f.monitor_id=m.id
 WHERE f.fetch_id=$1::uuid AND m.generation=f.generation AND m.status='monitoring' AND m.deleted_at IS NULL
 AND m.check_status='scheduled' FOR UPDATE OF m;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(:'fetch_id');
DEALLOCATE loadgen_query;
ROLLBACK;
