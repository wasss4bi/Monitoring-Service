-- Exact SQL source: internal/repository/monitoring_execution_repository.go, ClaimNotification
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on
SELECT id AS notification_id FROM notifications WHERE status='pending' ORDER BY id LIMIT 1 \gset
BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(bigint,bigint,uuid) AS
UPDATE notifications n SET status='sending',lease_token=$3::uuid,attempts_count=attempts_count+1,next_attempt_at=NOW()+($2*INTERVAL '1 microsecond') FROM user_contacts uc JOIN contacts c ON c.id=uc.contact_id WHERE n.id=$1 AND n.user_contact_id=uc.id AND (n.status='pending' OR (n.status='sending' AND n.next_attempt_at<NOW())) AND uc.is_active AND uc.is_verified RETURNING n.id,n.monitor_id,n.user_contact_id,n.event_type,n.message,n.status,n.attempts_count,n.last_error,n.next_attempt_at,n.created_at,n.sent_at,n.idempotency_key::text,c.name,uc.contact,uc.updated_at;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(:notification_id,30000000,'00000000-0000-5000-8000-000000000001');
DEALLOCATE loadgen_query;
ROLLBACK;
