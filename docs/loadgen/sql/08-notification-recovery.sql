-- Exact SQL source: internal/repository/monitoring_execution_repository.go, RecoverExpiredNotifications
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on

BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(integer) AS
SELECT id,attempts_count,created_at FROM notifications WHERE status='sending' AND next_attempt_at<NOW() ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT $1;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(1000);
DEALLOCATE loadgen_query;
ROLLBACK;
