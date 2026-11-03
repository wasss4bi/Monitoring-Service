# Managed session cutover — approved 2026-11-10

The user approved fresh managed anonymous identities, including PostgreSQL
session envelopes. The implementation is migration 25; its SQL is the source of
truth in migrations/000025_fetcher_profile_cutover.up.sql.

After stopping old consumers and draining in-flight work, this migration clears
only managed envelopes, advances monitor/session/profile generations, cancels
old pending/fetching jobs and schedules active monitors again. Stopped/terminal
business status, provided sessions and old Docker volumes are preserved. The new
stack uses a separate fetcher_profiles volume. Cleared identity cannot be
reconstructed by a down migration.

Regression tests on isolated PostgreSQL verify stale-result rejection, job
cancellation and preservation of provided/stopped state. The working database
was not migrated during validation. No old Docker volume was deleted.
