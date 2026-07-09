# Monitoring

Monitoring-Service owns monitoring business state; [Fetcher](fetcher/README.md) owns external retrieval. Prometheus/Grafana/Loki remain in this repository. The API metrics listener is internal `monitoring_service:9100`; workers use 9101–9105 and Fetcher 9106. RabbitMQ coarse queue/consumer metrics stay on internal 15692. Labels exclude user/monitor IDs, URLs, selectors, sessions and arbitrary error text. Dashboard SLO alert thresholds have not been selected.

## Execution boundary

Creating a monitor validates and saves configuration without contacting the target. Scheduler creates a monitor-owned durable job and outbox event when its owner has an active verified contact and the hostname is active. JSON and HTML use the existing typed RabbitMQ queues, prefetch one. Workers call mTLS gRPC Fetcher, compare returned scalars and atomically finalize generation/token/lease, session CAS, evaluation, notification outbox and inbox. No direct HTTP or Node client path remains in Monitoring-Service.

Only admin can manage target-hostnames, bootstrap exceptions and global contact channels. Users own `/me/*`; Casbin reads their current PostgreSQL role per request. See OpenAPI and [Bruno](bruno/README.md).

`page_url` is a required monitor product link and does not choose retrieval mode. Optional shared `browser_page_url` selects exact browser JSON capture, including query. Optional `json_source` selects explicit HTTP extraction from HTML; it cannot be combined with browser_page_url. HTML selector/XPath mode renders `url`. Conflicting endpoint source configuration returns 409. Custom upstream headers are rejected; anonymous sessions may be managed or provided.

Fetcher returns raw JSON number tokens, text/bool values, selector errors and fetch-level failure. Numeric parsing/number_format, operators and rules_count_to_finish remain in Monitoring-Service. Invalid selector is terminal. Missing, unsupported or non-numeric data does not satisfy a rule. A common document failure suppresses notification triggers from partial cache hits. A success message includes only actually matched rules.

The scalar cache is shared across anonymous sessions by canonical URL + effective selector/source/mode, with fixed 10-minute TTL and mixed-age observations. It is deliberately independent of monitor IDs and comparisons. Full response bodies are not cached or stored in new fetch rows. Per-monitor jobs/session ownership does not imply per-monitor scalar cache isolation.

## Failures and concurrency

Generation monotonically increases after rules/threshold/credentials changes; old results are rejected and active monitors are rescheduled. Stopped/terminal monitors are not resumed by configuration fencing. Final terminal observations leave next_check_at NULL. Explicit resume and notification behavior are covered by lifecycle integration tests.

The scheduler allows at most 50 outstanding direct jobs and 50 browser-dependent jobs. PostgreSQL capacity rows serialize scheduling across replicas. Stop, delete and session replacement invalidate pending and in-flight fetches in the same transaction as the monitor change. Notification completion requires the current unexpired lease token; a permanent failure for an old address cannot deactivate a changed contact.

A monitor creation transaction associates the Idempotency-Key with its monitor. Until the key expires, repeating the same request returns the current monitor with HTTP 201, or 404 if it was deleted; a changed request with the same key remains a conflict. Other operations retain their existing replay semantics. The HTTP server bounds reading headers and the whole request body to two seconds.

Ordinary notification delivery has a 10-second budget for the entire email or Telegram send, including all Telegram parts. If a multipart delivery exceeds this budget, retry can repeat parts already delivered. SMTP connections are closed when the send context is cancelled.

Pending fetch recovery does not enqueue another outbox event while an unpublished event for the same fetch already exists. Once the previous event was published, recovery can enqueue a replacement if the fetch remains pending.

Infrastructure failure/capacity defers the same fetch_id without target attempt/error counters. Target failures retain six attempts/full jitter and existing HTTP retry classification. RPC/attempt budgets are 5/15 seconds direct, 100/300 seconds browser, at most three calls including preparation/re-evaluation; lease is at least 330 seconds. Details, mTLS, admission and dependency DOWN/UP ownership are in the [Fetcher runbook](fetcher/README.md).

Redis outage permits a local per-process hostname limiter fallback. There is no distributed exactly-one GET guarantee; replicas can collectively reach N times the per-process fallback quota. Profiles have one provider owner. Replace the coordinated stack together; if restarting only Browserless, stop/restart its connected HTML engine as well so it obtains a new CDP connection.

## Retention

`monitoring_cleanup` runs on startup and every MONITOR_CLEANUP_INTERVAL (default 24 hours). Each pass deletes at most 1,000 expired rows per table after seven days: terminal fetches by finished_at, processed inbox entries, terminal notifications. Pending/fetching jobs and pending operational deliveries are retained. Outbox events are not deleted by this service. Historical body cleanup is a separate explicit SQL tool; profile volumes are not purged.

## Queue migration

The former generic `monitoring_service.endpoint-fetch.v2` queue and `endpoint.fetch.v2` binding stay declared during migration. Stop its legacy consumers, deploy the typed publisher and JSON worker, then run `docker compose --profile migration run --rm endpoint_fetch_queue_migrator`. It republishes every legacy message to the JSON queue with the same `MessageId` and acknowledges the source only after a broker confirmation. A crash in between can produce a duplicate, which the existing transactional inbox safely ignores. A legacy message without `MessageId` is retained in the legacy dead-letter queue and makes the migrator fail for manual investigation. Do not delete the legacy queue or its DLQ until they are empty and the JSON worker has processed the migrated backlog.

## Successful Telegram notification

New rules require a non-blank `description` of 1..255 characters. Existing rules
receive their path/operator/value as a migration description. All three evaluator
paths collect matched rule identities, so a threshold of two out of three shows
only the conditions that actually matched. The completed message is persisted
inside the completion/outbox transaction. Redelivery retains that exact snapshot.

The Telegram success message contains monitor name; an API line for JSON only;
product page_url; the matched descriptions as bullets; monitor created_at and
completed_at formatted as YYYY-MM-DD HH:MM:SS GMT (UTC+00:00). Completion time is
the trigger's DB timestamp, not Telegram delivery time. Failure messages and
email success messages retain their previous format.

Telegram text above its 4096-character limit is sent in sequential parts, preferring
line boundaries and preserving all text. Unicode characters are never split;
UTF-16 units are counted conservatively so emoji also fit. Parts are spaced one
second apart, stop on the first failure, and honor cancellation. Delivery is
marked sent only after all parts succeed. A retry replays the whole stored text,
so a partially delivered prefix may repeat; per-part exactly-once delivery was
not introduced. The limit and pacing follow the [Bot API](https://core.telegram.org/bots/api#sendmessage)
and [Telegram FAQ](https://core.telegram.org/bots/faq#my-bot-is-hitting-limits-how-do-i-avoid-this).


## Sessions and deployment

Session envelopes are encrypted in Monitoring-Service and never returned by normal API responses. Only anonymous identities are in scope. Provided rejection never silently becomes a fresh guest. `reset-session` is admin-only and resets managed sessions for the hostname, with generation fencing and rescheduling; provided sessions and stopped/terminal business status are preserved.

The standalone Fetcher owns profiles, browser storage policy, selector implementation, DNS/redirect checks and manual diagnostics. Its images are pulled from private GHCR by chosen version and digest; no engine source/build stays in Monitoring-Service. See [session contract](monitor-sessions.md), [manual diagnostics](manual-browser.md) and [Fetcher runbook](fetcher/README.md).

## Verification

Monitoring-Service unit/integration tests cover HTTP RBAC/login, migration bootstrap, lifecycle, session isolation, stale results, notification snapshots/retries, lease recovery, dependency episodes and mandatory routing. Browser/scalar/Redis tests live in Fetcher CI. Coverage and SLA were not measured.

`make integration-monitoring` uses the Go migration runner and temporary databases; configure dedicated integration URLs before running. Test fixtures use a typed gRPC fake rather than a Redis response-body cache. k6 scenarios remain manual and independent: `make k6-create-1000-monitors` and `make k6-list-monitors` require TOKEN and can create persistent test records.

## Contact verification

A signed-in user requests a code for an unverified email or Telegram contact. The API atomically stores the verification hash and creates an outbox event; the notification worker delivers the code. The ten-minute validity starts at request time, including any queue delay. Failed delivery is retried while the code is valid; delivery can be duplicated after a worker failure. Codes are single-use, allow five incorrect attempts, and requests for the same contact are limited to one per minute. Changing a contact invalidates the old code and resets verification and activation. A successfully verified contact remains inactive until the user enables it.

The worker derives the code from the challenge ID and JWT_SECRET, and only a keyed hash is stored in the database. Changing JWT_SECRET invalidates outstanding codes. Both API and notification worker must use the same secret. Email verification requires SMTP STARTTLS. For Telegram, the recipient must have initiated a conversation with the configured bot. Never log codes or credentials.
