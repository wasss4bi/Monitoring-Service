-- Exact SQL source: internal/repository/outbox_repository.go, Claim
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on

BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(uuid,bigint) AS
WITH candidate AS (
            SELECT id FROM outbox_events
            WHERE published_at IS NULL
              AND next_attempt_at <= NOW()
              AND (lease_until IS NULL OR lease_until < NOW())
            ORDER BY next_attempt_at, id
            FOR UPDATE SKIP LOCKED
            LIMIT 1
        )
        UPDATE outbox_events event
        SET lease_token = $1::uuid,
            lease_until = NOW() + ($2 * INTERVAL '1 microsecond'),
            publish_attempts = publish_attempts + 1
        FROM candidate
        WHERE event.id = candidate.id
        RETURNING event.id, event.event_id::text, event.event_type,
                  event.aggregate_type, event.aggregate_id, event.payload;
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query('00000000-0000-5000-8000-000000000001',30000000);
DEALLOCATE loadgen_query;
ROLLBACK;
