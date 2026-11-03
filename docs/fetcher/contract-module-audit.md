# Fetcher boundary audit

The protobuf contract is now a real private Go dependency:
`github.com/wasss4bi/values-from-body-fetcher v0.1.1`, package `api/fetcher/v1`.
It was downloaded from GitHub tag v0.1.1, commit
`b27197c69f7fda86db303cca610c5b06e5ca218e`, matching images.json.
There is no local replace, vendor copy or locally generated protobuf package.

| Former files | Result |
|---|---|
| api/fetcher/v1/*.pb.go and SOURCE.md | Removed local copies after comparison with downloaded module; imports use GitHub package |
| internal/fetchertransport/* | Removed duplicated server/client helper; client-only mTLS configuration belongs to internal/fetcherclient |
| cmd/fetcher, internal/fetcher, internal/parser, internal/playwright, Node engines and browser/proxy builds | No maintained source in Monitoring-Service |
| tmp/fetcher-extracted and source backup trees | 120 published-source files and 88 SHA256-verified code backups moved to source-only archive outside Monitoring-Service |
| Retired Go rewrite/test-client sources | Archived 8 source files; no private state or directories relocated |
| tools/fetcher/clear-historical-bodies.sql | Moved to tools/monitoring: it manages old PostgreSQL business data |

The source-only archive is ignored by Git and Docker in the local Fetcher checkout:
`D:/ITSHNICHNAYA/Projects/Go/values-from-body-fetcher/tmp/monitoring_service-retired-source-2026-11-10`.
Existing ignored profiles, credentials, test certificates, logs, legacy node_modules
and Docker volumes were preserved at their original locations. They are not
Monitoring-Service source dependencies or Docker build inputs.

## What remains in Monitoring-Service

- internal/fetcherclient: a gRPC consumer and client certificate configuration;
  no HTTP download, selector evaluation, document cache or browser code.
- internal/config/fetcher.go: address and client identity wiring.
- internal/service/remote_fetch.go: maps monitor configuration into extraction
  requests, applies business retry/defer/session/lease policy, compares scalars
  with user rules and commits results/notifications.
- dependency health/metrics and migrations: Monitoring-owned operational and
  PostgreSQL business state; these are not Fetcher server mechanisms.
- GHCR version/digest pins: deployment infrastructure for the separate service.

`go list -deps` for both fetch workers includes only the upstream
`api/fetcher/v1` package from the Fetcher module, without its internal engines.
GitHub is used to obtain build dependencies; runtime calls FETCHER_ADDRESS over
mTLS gRPC. Fetcher returns scalar values/errors for JSONPath, CSS selector,
XPath and the already supported regex/source modes; business comparisons and
notifications remain in Monitoring-Service.

## Private builds

Docker uses a BuildKit secret for private repository access. tools/build-go.ps1
reads existing Git Credential Manager credentials or a caller-provided
GO_MODULE_GITHUB_TOKEN, forwards it only for build, and restores environment on
success/failure. No token is baked into Dockerfile ARG/ENV. GHCR package pull and
private Git repository read are separate permissions.

The dev image preloads the same pinned module during build. Its old module-cache
volume is retained but no longer hides the image's downloaded dependencies.
Runtime/hot reload needs no GitHub credential; dependency version changes require
rebuilding the dev image.

## Verification of the module switch

- The downloaded GitHub tag origin matches b27197c and the existing v0.1.1 image manifest. Both former generated files matched the downloaded module before their removal.
- `go test -mod=readonly ./...` passed. Integration-tag packages compiled with `go test -mod=readonly -tags integration ./... -run '^$'`; the DB integration scenarios were not rerun for this import-only/wiring change.
- The new external-module client received real JSONPath, CSS and XPath scalar values from a separate GHCR v0.1.1 stack over mTLS gRPC. No business workers or real notifications were started.
- Production and dev images built with actual private Git authentication. After initial transient Alpine CDN DNS errors, standard production build also passed without DNS overrides. No permanent daemon/network setting was changed.
- Worker binary module metadata contains the upstream v0.1.1 dependency. Runtime image ENV has no build token/GIT_ASKPASS; the secret mount and .netrc are absent.
- Dev image compiled the real Monitoring-Service worker with networking disabled and no credentials. PowerShell helper tests cover cached/provided credentials, production/dev arguments, failure cleanup and environment/location restoration.
- Production/dev Compose config and git diff whitespace checks passed. HTTP/OpenAPI and the gRPC wire contract were unchanged. No commits, pushes or working database migrations were performed for this change.

Validation logs are under ignored `tmp/contract-module-validation`. The separate validation stack is stopped after the checks; data/profile volumes are preserved.
