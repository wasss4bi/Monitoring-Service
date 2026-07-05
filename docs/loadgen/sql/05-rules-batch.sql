-- Exact SQL source: internal/repository/monitor_repository.go, ListRules
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on
SELECT array_agg(id)::text AS monitor_ids FROM (SELECT id FROM monitors ORDER BY id LIMIT 100) m \gset
BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(bigint[]) AS
SELECT id, monitor_id, json_path, comparison_operator, expected_value, created_at, source, number_format, description
        FROM monitor_rules
        WHERE monitor_id = ANY($1)
        ORDER BY monitor_id, id;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(:'monitor_ids');
DEALLOCATE loadgen_query;
ROLLBACK;
