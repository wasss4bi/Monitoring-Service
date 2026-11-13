-- Exact SQL source: internal/repository/monitoring_execution_repository.go, ScheduleDue
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on

BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(integer) AS
SELECT m.id,e.type,e.id FROM monitors m JOIN monitored_endpoints e ON e.id=m.endpoint_id
		JOIN target_hostnames t ON t.id=e.target_hostname_id
        WHERE m.check_status IN ('idle', 'retry_wait') AND m.status='monitoring' AND m.deleted_at IS NULL
		  AND t.is_active
          AND (m.next_check_at IS NULL OR m.next_check_at <= NOW())
          AND EXISTS (
              SELECT 1 FROM user_contacts uc WHERE uc.user_id=m.user_id AND uc.is_active AND uc.is_verified
          )
        ORDER BY m.next_check_at NULLS FIRST, m.id
        FOR UPDATE OF m SKIP LOCKED LIMIT $1;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(1000);
DEALLOCATE loadgen_query;
ROLLBACK;
