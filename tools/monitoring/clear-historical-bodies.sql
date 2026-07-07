-- Run this bounded statement repeatedly until it updates zero rows.
-- Keeps fetch metadata, jobs, sessions, notifications and browser profiles.
WITH batch AS (
 SELECT fetch_id FROM endpoint_fetches WHERE response_json IS NOT NULL
 ORDER BY fetch_id LIMIT 1000 FOR UPDATE SKIP LOCKED
)
UPDATE endpoint_fetches f SET response_json=NULL FROM batch WHERE f.fetch_id=batch.fetch_id;
