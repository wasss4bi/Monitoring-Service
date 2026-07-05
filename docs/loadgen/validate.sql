\set ON_ERROR_STOP on
SELECT report #>> '{config,as_of}' AS as_of FROM loadgen_meta.run WHERE state='complete' \gset
-- Diagnostic SQL, not an application query. :'as_of'::timestamptz is the dataset's fixed as_of timestamp.
WITH
monitor_checks AS (
 SELECT
 count(*) FILTER(WHERE m.created_at>m.updated_at OR m.completed_at<m.created_at OR m.stopped_at<m.created_at OR m.last_checked_at<m.created_at OR m.last_checked_at>m.updated_at OR m.deleted_at<m.created_at) AS monitor_time,
 count(*) FILTER(WHERE (m.status IN ('success','error'))<>(m.completed_at IS NOT NULL) OR (m.status='stopped')<>(m.stopped_at IS NOT NULL)) AS monitor_terminal,
 count(*) FILTER(WHERE (e.type='json' AND m.path_type<>'json') OR (e.type='html' AND m.path_type NOT IN ('selector','xpath'))) AS path_type,
 count(*) FILTER(WHERE m.page_url<>e.canonical_url OR e.target_hostname_id IS NULL) AS endpoint_identity,
 count(*) FILTER(WHERE m.status='monitoring' AND m.check_status IN ('error','access_denied')) AS terminal_transport_state
 FROM monitors m JOIN monitored_endpoints e ON e.id=m.endpoint_id
), fetch_checks AS (
 SELECT
 count(*) FILTER(WHERE f.endpoint_id<>m.endpoint_id OR f.generation>m.generation OR f.monitor_id IS NULL) AS fetch_owner,
 count(*) FILTER(WHERE f.scheduled_at<f.created_at OR f.started_at<f.scheduled_at OR f.finished_at<f.started_at OR f.scheduled_at<m.created_at) AS fetch_time,
 count(*) FILTER(WHERE (f.status IN ('succeeded','failed'))<>(f.finished_at IS NOT NULL) OR (f.status<>'pending' AND f.started_at IS NULL) OR (f.status='fetching' AND (f.lease_token IS NULL OR f.lease_until IS NULL)) OR (f.status='succeeded' AND f.response_status NOT BETWEEN 200 AND 299)) AS fetch_state,
 count(*) FILTER(WHERE f.status IN ('pending','fetching') AND (m.status<>'monitoring' OR m.deleted_at IS NOT NULL OR f.generation<>m.generation OR m.check_status<>CASE WHEN f.status='pending' THEN 'scheduled' ELSE 'fetching' END)) AS live_owner,
 count(*) FILTER(WHERE m.status='stopped' AND m.generation<=f.generation) AS stopped_generation
 FROM endpoint_fetches f JOIN monitors m ON m.id=f.monitor_id
), notification_checks AS (
 SELECT
 count(*) FILTER(WHERE uc.user_id<>m.user_id OR m.status NOT IN ('success','error') OR n.event_type<>CASE WHEN m.status='success' THEN 'monitor.succeeded' ELSE 'monitor.failed' END) AS notification_owner,
 count(*) FILTER(WHERE n.created_at<m.completed_at OR n.sent_at<n.created_at OR n.next_attempt_at<n.created_at OR (n.status='sent')<>(n.sent_at IS NOT NULL)) AS notification_time
 FROM notifications n JOIN monitors m ON m.id=n.monitor_id JOIN user_contacts uc ON uc.id=n.user_contact_id
), evaluation_checks AS (
 SELECT count(*) FILTER(WHERE v.monitor_id<>f.monitor_id OR f.status<>'succeeded' OR v.created_at<f.started_at OR v.created_at>f.finished_at) AS evaluation_owner
 FROM monitor_fetch_evaluations v JOIN endpoint_fetches f ON f.fetch_id=v.fetch_id
), rule_checks AS (
 SELECT count(*) FILTER(WHERE n NOT BETWEEN 1 AND 3 OR m.rules_count_to_finish>n OR m.rules_count_to_finish<1) AS rule_count
 FROM monitors m LEFT JOIN (SELECT monitor_id,count(*) n FROM monitor_rules GROUP BY monitor_id) r ON r.monitor_id=m.id
), endpoint_checks AS (
 SELECT count(*) FILTER(WHERE e.canonical_url<>'https://'||t.hostname||'/resources/'||e.id OR e.created_at>e.updated_at OR e.last_attempt_at<e.created_at OR e.last_success_at>e.last_checked_at OR (e.status='fetching' AND (e.lease_token IS NULL OR e.lease_until IS NULL))) AS endpoint_state
 FROM monitored_endpoints e JOIN target_hostnames t ON t.id=e.target_hostname_id
), outbox_checks AS (
 SELECT count(*) FILTER(WHERE o.event_type NOT IN ('endpoint.fetch.json','endpoint.fetch.html','notification.deliver') OR
  (o.event_type LIKE 'endpoint.fetch.%' AND (o.aggregate_type<>'monitor' OR f.fetch_id IS NULL OR f.monitor_id<>o.aggregate_id OR o.event_type<>'endpoint.fetch.'||e.type OR o.payload<>jsonb_build_object('fetch_id',f.fetch_id::text))) OR
  (o.event_type='notification.deliver' AND (o.aggregate_type<>'notification' OR n.id IS NULL OR o.payload<>jsonb_build_object('notification_id',n.id))) OR
  o.published_at<o.created_at OR o.next_attempt_at<o.created_at OR (o.published_at IS NOT NULL AND (o.lease_until IS NOT NULL OR o.lease_token IS NOT NULL))) AS outbox_payload
 FROM outbox_events o
 LEFT JOIN endpoint_fetches f ON o.event_type LIKE 'endpoint.fetch.%' AND f.fetch_id=(o.payload->>'fetch_id')::uuid
 LEFT JOIN monitored_endpoints e ON e.id=f.endpoint_id
 LEFT JOIN notifications n ON o.event_type='notification.deliver' AND n.id=(o.payload->>'notification_id')::bigint
), inbox_checks AS (
 SELECT count(*) FILTER(WHERE o.id IS NULL OR o.published_at IS NULL OR p.processed_at<o.published_at OR p.consumer_name<>CASE WHEN o.event_type='notification.deliver' THEN 'notification-worker' ELSE 'endpoint-fetch-worker' END) AS inbox_event
 FROM processed_messages p LEFT JOIN outbox_events o ON o.event_id=p.message_id
), key_checks AS (
 SELECT count(*) FILTER(WHERE expires_at<=created_at OR status NOT IN ('processing','completed') OR (status='completed' AND response_code IS NULL) OR created_at>:'as_of'::timestamptz) AS idempotency_state FROM idempotency_keys
)
SELECT x.key,x.value::bigint FROM (
 SELECT to_jsonb(m)||to_jsonb(f)||to_jsonb(n)||to_jsonb(v)||to_jsonb(r)||to_jsonb(e)||to_jsonb(o)||to_jsonb(p)||to_jsonb(k) body
 FROM monitor_checks m CROSS JOIN fetch_checks f CROSS JOIN notification_checks n CROSS JOIN evaluation_checks v CROSS JOIN rule_checks r CROSS JOIN endpoint_checks e CROSS JOIN outbox_checks o CROSS JOIN inbox_checks p CROSS JOIN key_checks k
) s CROSS JOIN LATERAL jsonb_each_text(s.body) x
UNION ALL SELECT 'unvalidated_constraints',count(*) FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname='public' AND NOT c.convalidated
UNION ALL SELECT 'disabled_integrity_triggers',count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND t.tgisinternal AND t.tgenabled<>'O';

SELECT relname,n_live_tup FROM pg_stat_user_tables WHERE schemaname='public' ORDER BY relname;
SELECT tablename,indexname,indexdef FROM pg_indexes WHERE schemaname='public' ORDER BY tablename,indexname;
SELECT pg_size_pretty(pg_database_size(current_database())) AS database_size;

SELECT format('SELECT %L AS check_name,n AS violations FROM (%s) result(n)',name,query) FROM (

 SELECT 'fk:'||con.conname,
 'SELECT count(*) FROM '||con.conrelid::regclass||' a WHERE '||
 string_agg(format('a.%I IS NOT NULL',a.attname),' AND ' ORDER BY k.ord)||
 ' AND NOT EXISTS (SELECT 1 FROM '||con.confrelid::regclass||' b WHERE '||
 string_agg(format('a.%I=b.%I',a.attname,b.attname),' AND ' ORDER BY k.ord)||')'
 FROM pg_constraint con
 JOIN pg_namespace ns ON ns.oid=con.connamespace AND ns.nspname='public'
 CROSS JOIN LATERAL unnest(con.conkey,con.confkey) WITH ORDINALITY k(ak,bk,ord)
 JOIN pg_attribute a ON a.attrelid=con.conrelid AND a.attnum=k.ak
 JOIN pg_attribute b ON b.attrelid=con.confrelid AND b.attnum=k.bk
 WHERE con.contype='f' GROUP BY con.oid
) checks(name,query) \gexec


SELECT format('SELECT %L AS check_name,n AS violations FROM (%s) result(n)',name,query) FROM (

 SELECT 'unique:'||idx.indexrelid::regclass,
 'SELECT count(*) FROM (SELECT 1 FROM '||idx.indrelid::regclass||' WHERE '||
 COALESCE(pg_get_expr(idx.indpred,idx.indrelid),'true')||' AND '||
 string_agg(format('%I IS NOT NULL',a.attname),' AND ' ORDER BY k.ord)||
 ' GROUP BY '||string_agg(format('%I',a.attname),',' ORDER BY k.ord)||' HAVING count(*)>1) duplicate'
 FROM pg_index idx JOIN pg_class t ON t.oid=idx.indrelid
 JOIN pg_namespace ns ON ns.oid=t.relnamespace AND ns.nspname='public'
 CROSS JOIN LATERAL unnest(idx.indkey) WITH ORDINALITY k(attnum,ord)
 JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=k.attnum
 WHERE idx.indisunique AND t.relname<>'schema_migrations' AND k.ord<=idx.indnkeyatts
 GROUP BY idx.indexrelid,idx.indrelid,pg_get_expr(idx.indpred,idx.indrelid)
) checks(name,query) \gexec


SELECT format('SELECT %L AS check_name,n AS violations FROM (%s) result(n)',name,query) FROM (

 SELECT 'not-null:'||t.relname,
 'SELECT count(*) FROM '||t.oid::regclass||' WHERE '||string_agg(format('%I IS NULL',a.attname),' OR ' ORDER BY a.attnum)
 FROM pg_class t JOIN pg_namespace ns ON ns.oid=t.relnamespace AND ns.nspname='public'
 JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum>0 AND NOT a.attisdropped AND a.attnotnull
 WHERE t.relkind='r' AND t.relname<>'schema_migrations' GROUP BY t.oid
) checks(name,query) \gexec

SELECT format('SELECT %L AS table_name,count(*) AS actual_rows,%L::bigint AS expected_rows FROM %I',e.key,e.value->>'count',e.key)
FROM loadgen_meta.run r CROSS JOIN LATERAL jsonb_each(r.report->'tables') e
WHERE r.state='complete' ORDER BY e.key \gexec
SELECT 'endpoint' AS kind,status,count(*) FROM monitored_endpoints GROUP BY status
UNION ALL SELECT 'monitor',status,count(*) FROM monitors GROUP BY status
UNION ALL SELECT 'monitor_check',check_status,count(*) FROM monitors GROUP BY check_status
UNION ALL SELECT 'fetch',status,count(*) FROM endpoint_fetches GROUP BY status
UNION ALL SELECT 'notification',status,count(*) FROM notifications GROUP BY status ORDER BY 1,2;
SELECT CASE WHEN next_check_at IS NULL THEN 'null' WHEN next_check_at<=:'as_of'::timestamptz THEN 'due' ELSE 'future' END AS period,count(*) FROM monitored_endpoints GROUP BY 1;
SELECT CASE WHEN next_check_at IS NULL THEN 'null' WHEN next_check_at<=:'as_of'::timestamptz THEN 'due' ELSE 'future' END AS period,count(*) FROM monitors GROUP BY 1;
SELECT event_type,published_at IS NOT NULL AS published,publish_attempts,count(*) FROM outbox_events GROUP BY 1,2,3 ORDER BY 1,2,3;
SELECT consumer_name,count(*),min(processed_at),max(processed_at) FROM processed_messages GROUP BY consumer_name;
SELECT n.nspname,t.relname,con.conname,pg_get_constraintdef(con.oid) FROM pg_constraint con JOIN pg_class t ON t.oid=con.conrelid JOIN pg_namespace n ON n.oid=t.relnamespace WHERE n.nspname='public' ORDER BY t.relname,con.conname;

