-- Exact SQL source: internal/repository/monitor_repository.go, ListByUser
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on
SELECT user_id FROM monitors ORDER BY id LIMIT 1 \gset
BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(bigint,timestamptz,bigint,integer) AS
SELECT m.id, m.user_id, m.endpoint_id, e.canonical_url, m.name, e.type, '{}'::jsonb /* caller headers disabled */, m.page_url, COALESCE(e.browser_page_url,''), COALESCE(e.json_source,'null'::jsonb), m.path_type, m.rules_count_to_finish, m.status,
           m.last_checked_at, m.last_error, m.completed_at, m.stopped_at,
           m.deleted_at, m.created_at, m.updated_at, m.session_mode, m.session_status, m.session_ciphertext IS NOT NULL
    FROM monitors m
    JOIN monitored_endpoints e ON e.id = m.endpoint_id

        WHERE m.user_id = $1 AND m.deleted_at IS NULL
          AND ($2::timestamptz IS NULL OR (m.created_at, m.id) < ($2, $3))
        ORDER BY m.created_at DESC, m.id DESC
        LIMIT $4;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(:user_id,NULL,NULL,100);
DEALLOCATE loadgen_query;
ROLLBACK;
