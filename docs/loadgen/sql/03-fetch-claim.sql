-- Exact SQL source: internal/repository/monitoring_execution_repository.go, ClaimFetch
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on
SELECT f.fetch_id::text AS fetch_id FROM endpoint_fetches f JOIN monitors m ON m.id=f.monitor_id JOIN monitored_endpoints e ON e.id=f.endpoint_id JOIN target_hostnames t ON t.id=e.target_hostname_id WHERE f.status='pending' AND m.check_status='scheduled' AND m.status='monitoring' AND m.deleted_at IS NULL AND f.generation=m.generation AND t.is_active ORDER BY f.scheduled_at LIMIT 1 \gset
BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(uuid,uuid,bigint) AS
WITH claimed AS (
		UPDATE endpoint_fetches f SET status='fetching', started_at=COALESCE(started_at,NOW()), lease_token=$2::uuid, lease_until=NOW()+($3*INTERVAL '1 microsecond')
		FROM monitors m JOIN monitored_endpoints e ON e.id=m.endpoint_id JOIN target_hostnames t ON t.id=e.target_hostname_id
        WHERE f.fetch_id=$1::uuid AND f.endpoint_id=e.id AND f.monitor_id=m.id AND f.generation=m.generation
		  AND t.is_active AND m.status='monitoring' AND m.deleted_at IS NULL
          AND (f.status='pending' OR (f.status='fetching' AND f.lease_until<NOW()))
		RETURNING f.fetch_id::text,f.endpoint_id,e.canonical_url,e.type,'{}'::jsonb /* caller headers disabled */,m.attempts_count,m.page_url,COALESCE(e.browser_page_url,''),COALESCE(e.json_source,'null'::jsonb),
		          t.id,t.hostname,t.limit_per_minute,
		          COALESCE(t.bootstrap_url,''),COALESCE(m.session_ciphertext,''),m.session_generation,m.id,m.generation,m.session_mode,m.profile_generation
	  ) SELECT * FROM claimed;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(:'fetch_id','00000000-0000-5000-8000-000000000001',60000000);
DEALLOCATE loadgen_query;
ROLLBACK;
