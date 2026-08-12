## Why

The adapter is currently tested only with unit tests that mock AlertManager and Kubernetes clients. While unit tests validate individual components in isolation, they do not verify the full reconciliation loop end-to-end on a real OpenShift cluster: firing alerts from a live AlertManager → deduplication logic → AgenticRun CR creation → live lightspeed-agentic-operator reconciliation. Without E2E tests against real cluster infrastructure, integration bugs, ConfigMap configuration issues, RBAC problems, or incompatibilities with the live operator can go undetected until production.

## What Changes

- Add a Ginkgo-based E2E test suite that runs against a live OpenShift cluster with real AlertManager and the lightspeed-agentic-operator deployed.
- Add `hack/deploy-e2e.sh` to deploy the adapter with test-specific configuration (faster poll interval, shorter delays, configured receivers).
- E2E tests will validate: happy-path reconciliation, deduplication behavior, fingerprint labeling, ConfigMap reloads, and error handling.
- Add `make deploy-e2e`, `test-e2e`, and `undeploy-e2e` targets.
- Add OpenShift CI integration via step-registry (following the cluster-health-analyzer pattern).

## Capabilities

### New Capabilities
- `e2e-testing`: Ginkgo-based E2E test suite that validates the adapter's full reconciliation loop against live OpenShift cluster infrastructure (real AlertManager, live lightspeed-agentic-operator, real Kubernetes API).

### Modified Capabilities
- `testing`: Existing unit tests remain unchanged; E2E tests are additive and run separately via `make test-e2e`.

## Impact

- New directory: `test/e2e/` with Ginkgo suite and `test/e2e/framework/` with `oc`-based cluster utilities.
- New script: `hack/deploy-e2e.sh` to patch manifests and deploy the adapter for testing.
- `Makefile` — new targets: `deploy-e2e`, `test-e2e`, `undeploy-e2e`.
- CI pipeline: new step-registry job in `openshift/release` repo (separate PR, follows cluster-health-analyzer pattern).
- No changes to production code; E2E tests exercise existing adapter behavior on real infrastructure.
