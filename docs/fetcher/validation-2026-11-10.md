# Validation — 2026-11-10

Fetcher release: [v0.1.1](https://github.com/wasss4bi/values-from-body-fetcher/releases/tag/v0.1.1), source `b27197c69f7fda86db303cca610c5b06e5ca218e`.
[Release CI](https://github.com/wasss4bi/values-from-body-fetcher/actions/runs/42754152698) passed. [Main CI](https://github.com/wasss4bi/values-from-body-fetcher/actions/runs/42755141306) also passed after sample defaults were updated to the already published version.
All five private Linux/amd64 images were pulled by the version/digests in [images.json](images.json).

## Monitoring-Service

- `go test -mod=readonly ./...`: passed after source extraction.
- `go test -mod=readonly -tags integration ./... -count=1`: passed against an isolated PostgreSQL16/Redis/RabbitMQ Docker project.
- The separately gated `TestMonitoringInfrastructureAndInbox` passed with all three explicit integration URLs, including INTEGRATION_RABBITMQ_URL.
- Real HTTP login/admin/owner access and immediate role revocation, one-time bcrypt admin migration/no reset/email conflict, managed profile migration/reset, stale-result fencing, transaction rollback/replay, monitor lifecycle, notification snapshots/retry, dependency episodes and RabbitMQ mandatory routing passed.
- Final Monitoring-Service Docker image built under a separate validation tag. Migration and dependency-health binaries are present; `.env` and transferred Fetcher source files are absent.
- OpenAPI references, all 61 Bruno YAML files, Compose configuration and `git diff --check` passed.

## Fetcher

CI passed proto regeneration, Go unit/race, Redis integration, Node unit suites and three browser fixture jobs. Browser checks cover real HTTPS redirect handling, repeated profile closure/reopening, retained-volume provider recreation, stale-lock backup and preservation of identity. These fixtures do not contact marketplaces.

The final stack was run from downloaded GHCR v0.1.1 images with its retained test volume. Real mTLS RPC smoke results:

```text
JSON call 1: scalar ok, origin=VALUE_ORIGIN_NETWORK
JSON call 2: scalar ok, origin=VALUE_ORIGIN_CACHE
HTML: Browserless/Squid scalar ok
SSRF: private destination rejected
mTLS: valid client accepted; missing client certificate rejected
```

Only public JSON/HTML fixtures were used for those network calls. Slow external responses also demonstrated the configured RPC deadline; fixture success does not promise target-site availability.

## Runtime state

**Последующий живой прогон:** рабочий Compose и БД переключены, миграции
22–25 применены, реальные Telegram-уведомления отправлены по отдельному
запросу пользователя. Текущие результаты и ограничения:
[живые карточки](live-cards-2026-11-10.md). Текст ниже описывает состояние
сразу после изолированного acceptance, до этого живого прогона.

The working Monitoring-Service database/containers were not switched. Migrations22–25 require the documented coordinated cutover. Migration25 intentionally clears managed identity and cannot restore it through down; provided identity and old Docker volumes remain. Replacing only Browserless also requires restarting its connected HTML engine; full version updates replace the coordinated stack.

No real Telegram/email notification was sent. No load, coverage or SLA measurement is claimed. Monitoring-Service changes remain uncommitted. Fetcher commits/releases were pushed under the user's existing GitHub/Git identity. Validation containers were isolated from working data; local logs are under ignored `tmp/fetcher-validation`.
