-- Exact SQL source: internal/repository/monitoring_execution_repository.go, ActiveMonitors
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on
SELECT id AS monitor_id FROM monitors WHERE status='monitoring' AND deleted_at IS NULL ORDER BY id LIMIT 1 \gset
BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(bigint) AS
SELECT m.id,m.user_id,m.path_type,m.rules_count_to_finish,r.id,r.json_path,r.comparison_operator,r.expected_value,r.source,r.number_format,r.description FROM monitors m JOIN monitor_rules r ON r.monitor_id=m.id WHERE m.id=$1 AND m.status='monitoring' AND m.deleted_at IS NULL ORDER BY m.id,r.id;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(:monitor_id);
DEALLOCATE loadgen_query;
ROLLBACK;
