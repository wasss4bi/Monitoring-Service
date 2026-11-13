-- Exact SQL source: internal/repository/notification_repository.go, ListByUser
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on
SELECT m.user_id FROM notifications n JOIN monitors m ON m.id=n.monitor_id ORDER BY n.id LIMIT 1 \gset
BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(bigint,timestamptz,bigint,integer) AS
SELECT n.id,n.monitor_id,n.user_contact_id,n.event_type,n.message,n.status,n.attempts_count,n.last_error,n.next_attempt_at,n.created_at,n.sent_at,n.idempotency_key::text
        FROM notifications n JOIN monitors m ON m.id=n.monitor_id
        WHERE m.user_id=$1 AND m.deleted_at IS NULL
          AND ($2::timestamptz IS NULL OR (n.created_at,n.id)<($2,$3))
        ORDER BY n.created_at DESC,n.id DESC LIMIT $4;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(:user_id,NULL,NULL,100);
DEALLOCATE loadgen_query;
ROLLBACK;
