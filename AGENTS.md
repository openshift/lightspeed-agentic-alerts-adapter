# Lightspeed Agentic Alerts Adapter

A Go component that polls OpenShift AlertManager for firing alerts and creates `AgenticRun` CRs (`agentic.openshift.io/v1alpha1`) to trigger automated remediation via the Lightspeed Agentic operator. Stateless, single-replica, create-only design — no internal state, diffs AlertManager vs Kubernetes API each cycle.

## Commands

```sh
make build          # → ./bin/alerts-adapter
make test           # go test ./...
make lint           # golangci-lint run ./...
make fmt            # go fmt ./...
make vet            # go vet ./...
make coverage       # HTML coverage report → coverage.html
make container-build # podman build
make container-push  # podman build + push (set IMAGE_NAME, IMAGE_TAG defaults to latest)

# Run a single test
go test -run TestFunctionName ./internal/adapter/

# Run a single subtest
go test -run TestFunctionName/subtest_name ./internal/adapter/
```

## Architecture

Three internal packages, each behind an interface, wired together in `cmd/alerts-adapter/main.go`:

- **`internal/alertmanager`** — AlertManager API client. Reads bearer token on every call (handles rotation). TLS via in-cluster CA. Implements `adapter.AlertSource`.
- **`internal/agenticrun`** — Two concerns: `build.go` translates an alert into an `AgenticRun` CR (deterministic name from alertname, namespace, and startsAt hash; embedded Go template `request.tmpl` for the request field); `client.go` wraps controller-runtime to create/list AgenticRuns. Implements `adapter.AgenticRunClient`.
- **`internal/adapter`** — Poll loop (`Run` → `reconcile` on ticker). Stateless deduplication: skips alerts below `preRunDelay` (default 0s), with an active (non-terminal) AgenticRun, or within `postRunDelay` (default 1h) of a terminal AgenticRun. Matching is by `alert-dedup-fingerprint` label (stable FNV-64a hash of labels minus configurable ignored labels).

The AgenticRun CRD types come from `github.com/openshift/lightspeed-agentic-operator/api`.

## Key design decisions

- Polls (not webhooks) for resilience — restart immediately sees all firing alerts.
- AgenticRuns always created in `openshift-lightspeed` namespace.
- 409 AlreadyExists on create is expected and handled as a no-op (returns `false, nil`).
- Two fingerprints on each AgenticRun CR: `alert-fingerprint` stores the original AlertManager fingerprint (for UI lookups by alert), `alert-dedup-fingerprint` stores the stable fingerprint (FNV-64a[:8] of sorted labels minus configurable ignored labels, used for dedup matching). Default ignored labels: `pod`, `instance`, `endpoint`, `uid`. Configurable via `deduplication.ignoredLabels` in the ConfigMap. AgenticRun names use a hash of the alert's `startsAt` timestamp for uniqueness.
- Terminal phases: Completed, Failed, Denied, Escalated.
- `preRunDelay` (default 0s) and `postRunDelay` (default 1h) are clamped to 0 for zero or negative values. Both can be explicitly set to `0s` to disable the delay. Invalid duration syntax causes a config load error.

## Conventions

- Commit messages: [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `test:`, `docs:`, `refactor:`).
- Structured logging with `log/slog` (JSON handler), passed explicitly — no globals except `slog.SetDefault` in main.
- Interfaces defined in the consumer package (`adapter`), not the provider.
- Tests use table-driven style.
