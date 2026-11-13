-- Exact SQL source: internal/repository/processed_message_repository.go, IsProcessed
-- EXPLAIN ANALYZE executes writes; every file rolls them back.
\set ON_ERROR_STOP on
\timing on
SELECT consumer_name,message_id::text FROM processed_messages ORDER BY processed_at DESC LIMIT 1 \gset
BEGIN;
SET LOCAL statement_timeout='10min';
PREPARE loadgen_query(varchar,uuid) AS
SELECT EXISTS(SELECT 1 FROM processed_messages WHERE consumer_name=$1 AND message_id=$2::uuid);
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(:'consumer_name',:'message_id');
DEALLOCATE loadgen_query;
ROLLBACK;
