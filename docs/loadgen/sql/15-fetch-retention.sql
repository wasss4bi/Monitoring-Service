-- Exact SQL source: internal/repository/monitoring_maintenance_repository.go, DeleteExpired
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on

BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(timestamptz,integer) AS
DELETE FROM endpoint_fetches WHERE fetch_id IN (SELECT fetch_id FROM endpoint_fetches WHERE created_at < $1 ORDER BY created_at LIMIT $2);
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(NOW()-INTERVAL '7 days',1000);
DEALLOCATE loadgen_query;
ROLLBACK;
