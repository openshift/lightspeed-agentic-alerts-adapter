# Project Structure

## Module Map

| File/Directory | Key Symbols | Responsibility |
|---|---|---|
| `cmd/alerts-adapter/main.go` | `main`, `run`, `newTargets`, `newTargetRegistry`, `startServer`, `startSpokeClusterController` | Validates listener/TLS settings, loads configuration, wires clients and independent polling/investigation components, coordinates HTTPS and poller shutdown |
| `internal/adapter/adapter.go` | `Adapter`, `Run`, `reconcile`, `reconcileTarget`, `AlertSource`, `AgenticRunClient`, `SuspensionSource`, `Target`, `TargetSource` | Poll loop and stateless deduplication. Defines consumer interfaces. Implements receiver filtering, pre-run delay, active-run check, post-run delay, creation backoff |
| `internal/adapter/server.go` | `NewInvestigationServer` | Configures the HTTPS server, POST route, TLS minimum version, connection timeouts, and header limit |
| `internal/adapter/middleware.go` | `withTimeout`, `AuthMiddleware`, `readToken`, `creatorContextKey` | Derives the request deadline, validates bearer headers, authorizes the caller, and passes the request-scoped creator through the context |
| `internal/adapter/auth.go` | `AgenticRunRequest`, `AgenticRunCreator`, `AgenticRunInvestigationRequest`, `Authorize`, `logError` | ServiceAccount TokenReview, caller-authenticated SelfSubjectAccessReview, isolated caller clients, and categorized API failure logging |
| `internal/adapter/handler.go` | `Investigator`, `NewInvestigationHandler`, `requestBody`, `responseBody`, `writeErrorResponse` | Bounded body parsing, fingerprint validation, creator lookup, investigation invocation, and shared typed API errors with stable HTTP responses |
| `internal/adapter/investigation.go` | `InvestigationService`, `Investigate`, `nextRunName`, `matchesAttempt`, `shortenBase` | Local on-demand investigation flow, suspension, canonical alert selection, episode deduplication, naming, and caller-only creation; independent of polling backoff |
| `internal/agenticrun/build.go` | `Build`, `BuildForTarget`, `StableFingerprint`, `NextAvailableName`, `ShortenName`, `requestData`, `requestTemplate` | Alert-to-AgenticRun translation: deterministic naming, metadata sanitization, FNV-64a fingerprinting, Go template rendering |
| `internal/agenticrun/client.go` | `Client`, `ListAgenticRuns`, `CreateAgenticRun`, `SpokeTargetID` | Kubernetes CRUD for AgenticRun CRs. Target-scoped listing and creation. Spoke target ID computation |
| `internal/agenticrun/request.tmpl` | (template) | Embedded Go template for the `spec.request` field |
| `internal/alertmanager/client.go` | `Client`, `GetAlerts`, `Config` | AlertManager v2 API client. Bearer token auth, TLS via service CA or custom CA bundle. Supports local and remote endpoints |
| `internal/config/config.go` | `Config`, `LoadFromFile`, `Default`, `ToolsConfig`, `AgentConfig` | Configuration read from a mounted YAML file at startup. YAML parsing, defaults, duration clamping, ignored labels, skills validation |
| `internal/agenticolsconfig/client.go` | `Client`, `Suspended` | Reads `AgenticOLSConfig` singleton for suspended mode. Treats missing CRD/object as not suspended |
| `internal/multicluster/controller.go` | `Reconciler`, `Reconcile`, `SetupWithManager` | controller-runtime reconciler for SpokeCluster watch. Adds, replaces, or removes targets on lifecycle events |
| `internal/multicluster/registry.go` | `Registry`, `SetSpoke`, `RemoveSpoke`, `Targets` | Thread-safe target registry combining fixed local targets with dynamic spoke targets |
| `internal/multicluster/target.go` | `BuildTarget`, `CredentialSecretLabel` | Constructs a reconciliation target from a SpokeCluster and its credential Secret |
| `manifests/` | Deployment, Service, ServiceAccount, RBAC, ConfigMap | Direct deployment, Service CA certificate request/mount, HTTPS port, and TokenReview permissions |

## Key Entry Points

- `cmd/alerts-adapter/main.go:run()` validates the port and loads the server certificate/key pair before configuration and Kubernetes client initialization. It builds targets, creates an independent `InvestigationService` for the fixed local target, and optionally starts the SpokeCluster controller.
- `startServer()` opens the listener, serves HTTPS using the preloaded certificate, runs the poller with the shared process context, and coordinates shutdown.
- `adapter.Run()` executes an immediate reconcile then enters a ticker loop until context cancellation.
- The request path is `withTimeout → AuthMiddleware → NewInvestigationHandler → InvestigationService.Investigate`.

## Naming Conventions

- One package per concern, named for the domain concept (not the implementation pattern).
- Interfaces defined in the consumer package (`adapter`), not the provider.
- Test files colocated with source: `*_test.go` in the same package.
- E2E tests in `test/e2e/`.

## Dependencies and Logging

The six internal packages are wired together in `cmd/alerts-adapter/main.go`. AgenticRun and suspension types come from `github.com/openshift/lightspeed-agentic-operator/api`; SpokeCluster types come from `github.com/openshift/lightspeed-hub/api`. Alert retrieval uses `github.com/prometheus/alertmanager` and its generated v2 client. Kubernetes object clients use controller-runtime; TokenReview and SelfSubjectAccessReview use client-go typed clients. Polling creation backoff uses client-go's `flowcontrol.Backoff`. HTTPS and middleware use the Go standard library.

Logging uses `log/slog` with a JSON handler at Info level. Main passes the logger to components and sets the default logger. Controller-runtime logging uses an adapter over the same handler.

API dependency failures are logged by operation and error category, with an upstream status code when available, rather than raw diagnostic text. HTTP failures remain generic where required by the API contract.

See [reconcile flow](reconcile-flow.md) for call order, state, and known implementation limits.
