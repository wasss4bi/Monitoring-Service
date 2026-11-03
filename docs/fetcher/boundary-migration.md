# Extraction ownership

The user selected complete extraction to `wasss4bi/values-from-body-fetcher`;
Monitoring-Service keeps monitoring business logic and consumes gRPC plus GHCR images.

| Previous implementation | Resulting owner / regression checks |
|---|---|
| HTTP transport, DNS/IP checks, redirects | Fetcher `internal/fetcher`; `TestLegacyNetworkSafetyCasesRemainBlocked`, `TestSpecialUseNetworksAndURLBypasses`, real HTTPS redirect browser tests |
| JSON paths, regex, decoded sources | Fetcher `internal/fetcher`; `TestPathsAndDecodedSourceKeepExactValues`, scalar/invalid selector tests |
| Node/HTML clients, browser profiles, scalar cache | Fetcher stack; Node unit/browser suites and Redis integration test |
| Comparison, numeric formats, threshold, mismatch vs terminal failure | Monitoring-Service `remote_fetch.go`, `rule_options.go`, `html_number.go`; `TestHTMLMonitoringUsesScalarContractAndRuleThreshold`, `TestRemoteThresholdMissingAndInvalidSelector`, money/HTML numeric tests |
| Retry and notification policy | Monitoring-Service `monitoring_execution_service.go`; HTTP classification, jitter, comparison and notification tests retained |
| Generation/lease fencing, scheduling, finalization, outbox/inbox | Monitoring-Service repository and service; PostgreSQL integration tests retained |
| Authentication, ownership, roles | Monitoring-Service JWT/Casbin/PostgreSQL; access policy tests |

`html_monitoring_execution_service.go` is the retired direct Node-client path.
Both JSON and HTML workers now call the same gRPC orchestration. Removing that
retired path removes duplicated evaluation/cache/transport orchestration; the
business behaviors above remain in Monitoring-Service and are tested through scalar gRPC
responses. The network safety test cases move to Fetcher; Monitoring-Service's retry tests
remain in `monitoring_execution_security_test.go`.

During extraction, the complete Monitoring-Service service file set excluding the retired
HTML implementation passed its tests. The standalone Fetcher Go suite passed.
Full Monitoring-Service unit/integration checks passed after extraction. The standalone Fetcher pipeline and image manifests are tracked in docs/fetcher/images.json. Source backup and ownership checks preceded removal; local ignored profiles/node_modules and old Docker volumes were retained.
