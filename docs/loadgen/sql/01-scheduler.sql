-- Exact SQL source: internal/repository/monitoring_execution_repository.go, ScheduleDue
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on

BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(integer,boolean,text[],boolean) AS
WITH outstanding AS (
 SELECT e.target_hostname_id,count(*) AS jobs FROM endpoint_fetches f
 JOIN monitors owner ON owner.id=f.monitor_id JOIN monitored_endpoints e ON e.id=owner.endpoint_id JOIN target_hostnames ot ON ot.id=e.target_hostname_id
 WHERE f.status IN ('pending','fetching') AND owner.status='monitoring' AND owner.deleted_at IS NULL
 AND owner.generation=f.generation AND (e.type<>'json' OR e.browser_page_url IS NOT NULL OR e.json_source IS NOT NULL OR ot.bootstrap_url IS NOT NULL)=$4 GROUP BY e.target_hostname_id
 ) SELECT m.id,c.type,c.endpoint_id FROM target_hostnames t
 LEFT JOIN outstanding o ON o.target_hostname_id=t.id
 JOIN LATERAL (
  SELECT candidate.*,row_number() OVER(ORDER BY candidate.next_check_at NULLS FIRST,candidate.id) AS position
  FROM (SELECT selected.id,selected.next_check_at,e.id AS endpoint_id,e.type FROM monitors selected
   JOIN monitored_endpoints e ON e.id=selected.endpoint_id
   WHERE e.target_hostname_id=t.id AND selected.check_status IN ('idle','retry_wait')
   AND (e.type<>'json' OR e.browser_page_url IS NOT NULL OR e.json_source IS NOT NULL OR t.bootstrap_url IS NOT NULL)=$4
   AND selected.status='monitoring' AND selected.deleted_at IS NULL
   AND (selected.next_check_at IS NULL OR selected.next_check_at<=NOW())
   AND (NOT ($2 AND (COALESCE(cardinality($3::text[]),0)=0 OR t.hostname=ANY($3::text[])))
        OR NOT EXISTS(SELECT 1 FROM service_dependencies WHERE name='direct' AND status='down'))
   AND (NOT ($2 AND (COALESCE(cardinality($3::text[]),0)=0 OR t.hostname=ANY($3::text[])))
        OR (e.type='json' AND e.browser_page_url IS NULL AND e.json_source IS NULL AND t.bootstrap_url IS NULL)
        OR NOT EXISTS(SELECT 1 FROM service_dependencies WHERE name='browser' AND status='down'))
   AND EXISTS(SELECT 1 FROM user_contacts uc WHERE uc.user_id=selected.user_id AND uc.is_active AND uc.is_verified)
   ORDER BY selected.next_check_at NULLS FIRST,selected.id LIMIT $1
  ) candidate
 ) c ON true JOIN monitors m ON m.id=c.id
 WHERE t.is_active AND ($4 OR t.bootstrap_url IS NULL) AND m.status='monitoring' AND m.deleted_at IS NULL AND m.check_status IN ('idle','retry_wait')
 ORDER BY COALESCE(o.jobs,0)+c.position,c.next_check_at NULLS FIRST,m.id
 FOR UPDATE OF m SKIP LOCKED LIMIT LEAST($1,GREATEST(0,50-(SELECT COALESCE(sum(jobs),0) FROM outstanding)));
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(100,false,ARRAY[]::text[],false);
DEALLOCATE loadgen_query;
ROLLBACK;
