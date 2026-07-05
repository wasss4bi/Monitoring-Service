-- Exact SQL source: internal/repository/monitoring_execution_repository.go, RecoverExpiredFetches
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on

BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(integer) AS
SELECT f.fetch_id::text,f.monitor_id,f.generation,m.generation,e.type,(m.status='monitoring' AND m.deleted_at IS NULL) FROM endpoint_fetches f JOIN monitors m ON m.id=f.monitor_id JOIN monitored_endpoints e ON e.id=f.endpoint_id WHERE f.status='fetching' AND f.lease_until<NOW() ORDER BY f.lease_until,f.fetch_id FOR UPDATE OF m SKIP LOCKED LIMIT $1;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(1000);
DEALLOCATE loadgen_query;
ROLLBACK;
