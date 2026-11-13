-- Diagnostic SQL, not an application query. $1 is the dataset's fixed as_of timestamp.
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
 SELECT count(*) FILTER(WHERE expires_at<=created_at OR status NOT IN ('processing','completed') OR (status='completed' AND response_code IS NULL) OR created_at>$1) AS idempotency_state FROM idempotency_keys
)
SELECT x.key,x.value::bigint FROM (
 SELECT to_jsonb(m)||to_jsonb(f)||to_jsonb(n)||to_jsonb(v)||to_jsonb(r)||to_jsonb(e)||to_jsonb(o)||to_jsonb(p)||to_jsonb(k) body
 FROM monitor_checks m CROSS JOIN fetch_checks f CROSS JOIN notification_checks n CROSS JOIN evaluation_checks v CROSS JOIN rule_checks r CROSS JOIN endpoint_checks e CROSS JOIN outbox_checks o CROSS JOIN inbox_checks p CROSS JOIN key_checks k
) s CROSS JOIN LATERAL jsonb_each_text(s.body) x
UNION ALL SELECT 'unvalidated_constraints',count(*) FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname='public' AND NOT c.convalidated
UNION ALL SELECT 'disabled_integrity_triggers',count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND t.tgisinternal AND t.tgenabled<>'O';
