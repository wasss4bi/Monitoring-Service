# Go linters

CI runs govet, staticcheck, errcheck, ineffassign, unused, gosec and bodyclose
using golangci-lint v2.12.2 and the settings in `.golangci.yml`, including
the `integration` build tag. Findings fail CI and prevent deployment.

Install the same version using the [official instructions](https://golangci-lint.run/docs/welcome/install/local/).
Go must also have access to the project's private GitHub module.

Run from the project root:

```sh
golangci-lint run ./...
```

If inaccessible local directories under `tmp` prevent package discovery,
check the current source directories explicitly:

```sh
golangci-lint run ./cmd/... ./internal/... ./integration/... ./tools/...
```
