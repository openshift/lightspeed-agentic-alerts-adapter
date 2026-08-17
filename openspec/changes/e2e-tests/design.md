## Context

The adapter polls AlertManager for firing alerts and creates AgenticRun CRs, which are then reconciled by the lightspeed-agentic-operator. The adapter's correctness in production depends on:
- Correctly authenticating to AlertManager (bearer token, TLS)
- Parsing real AlertManager API responses
- Applying deduplication filters in the right order against real cluster state
- Generating AgenticRun CRs that the live operator accepts and reconciles
- Reloading ConfigMap changes without restart
- Handling Kubernetes API errors (409 AlreadyExists, transient failures)

Unit tests validate individual components with mocks but cannot catch:
- RBAC misconfigurations preventing AgenticRun creation
- ConfigMap format issues causing config load failures
- AlertManager authentication/TLS problems
- Incompatibilities between adapter-created AgenticRuns and operator expectations
- Race conditions or timing issues in the poll loop

E2E tests on a real OpenShift cluster with live AlertManager and operator are needed to validate the full integration.

## Goals / Non-Goals

**Goals:**
- Validate the full reconciliation loop on a live OpenShift cluster: real firing alert → adapter processes → AgenticRun created → live operator reconciles it.
- Test deduplication logic end-to-end: verify alerts are correctly skipped based on severity, receiver filtering, pre-run delay, active AgenticRun presence, and post-run delay.
- Verify `alert-fingerprint` and `alert-dedup-fingerprint` labels are set correctly on created AgenticRun CRs.
- Verify ConfigMap changes are picked up by the adapter on the next poll cycle (no restart required).
- Test error handling: AlertManager unreachable, Kubernetes API errors.
- Tests run in OpenShift CI on a provisioned cluster and locally via `make test-e2e` (requires `oc login` to a cluster).

**Non-Goals:**
- Testing the lightspeed-agentic-operator's reconciliation logic in detail (that's the operator's own test suite). We only verify that AgenticRuns are created with correct structure and picked up by the operator (status phase transitions).
- Performance/load testing (not covered in this phase).
- Testing AlertManager's routing logic (we assume AlertManager works correctly; we test adapter's consumption of its API).
- Testing against mocked/fake dependencies (envtest, httptest) — this is explicitly a **live cluster** E2E test.

## Decisions

### 1. Test environment: live OpenShift cluster with real AlertManager and operator

**Decision:** E2E tests run against a real OpenShift cluster with:
- Real AlertManager in `openshift-monitoring` namespace (standard OCP deployment)
- Live lightspeed-agentic-operator deployed and running
- Adapter deployed via `hack/deploy-e2e.sh` with test-specific ConfigMap patches

**Rationale:**
- This is the only way to validate RBAC, TLS/auth, and real operator interaction.
- Matches OpenShift project conventions (see cluster-health-analyzer PR #81, openshift/release PRs #73740, #73087).
- CI provisions clusters automatically via step-registry; developers can test locally with `oc login`.

**Alternative considered:** envtest with mocked AlertManager and operator. Rejected because it doesn't catch real-world integration issues (RBAC, TLS, operator compatibility).

### 2. Test framework: Ginkgo with `oc`-based cluster utilities

**Decision:** Use Ginkgo for structured tests (BeforeSuite/AfterSuite, table-driven scenarios). Implement a lightweight framework in `test/e2e/framework/cluster.go` that wraps `oc` CLI commands for cluster interaction (not client-go).

**Rationale:**
- Ginkgo is standard in OpenShift projects and provides excellent test organization and reporting.
- `oc` CLI wrapper is simpler than client-go for E2E tests (no kubeconfig parsing, automatic namespace handling, easy debugging).
- Matches cluster-health-analyzer pattern exactly.

**Alternative considered:** client-go directly. Rejected due to added complexity and less readable test code.

### 3. Deploy script patches manifests on-the-fly

**Decision:** `hack/deploy-e2e.sh` will:
1. Copy manifests to temp directory
2. Patch `deployment.yaml` with `$IMAGE` env var (built in CI or specified locally)
3. Patch `configmap.yaml` with test-friendly values:
   - `pollInterval: 10s` (faster than default 30s)
   - `postRunDelay: 1m` (shorter than default 1h for testing cooldown logic)
   - `filtering.allowedReceivers: [Default]` (uncommented — required for adapter to process alerts)
4. Deploy with `oc apply -f`
5. Wait for deployment to be available

**Rationale:**
- CI builds a fresh image that must be tested (not the `:latest` hardcoded in manifests).
- Test-specific config values make tests faster and more deterministic.
- `allowedReceivers` **must** be configured or adapter skips all alerts (critical for E2E).

**Alternative considered:** Separate test manifests. Rejected to avoid drift between production and test manifests.

### 4. Test scope: happy path + dedup + config reload + errors

**Decision:** E2E tests will cover:
1. **Happy path:** Firing alert (routed to configured receiver) → AgenticRun created with correct labels
2. **Deduplication:** Alerts skipped due to severity `info`/`none`, receiver not in allowlist, active AgenticRun, within `postRunDelay` of terminal AgenticRun
3. **Fingerprint labels:** Verify `alert-fingerprint` and `alert-dedup-fingerprint` are set correctly
4. **Error handling:** 409 AlreadyExists on AgenticRun create is no-op (logged at Info level)

**Rationale:** These scenarios cover the critical integration points and most common failure modes.

**Alternative considered:** Exhaustive coverage of all unit-test scenarios. Rejected as E2E tests are slower; unit tests cover detailed logic, E2E validates integration.

### 5. Make targets: deploy-e2e, test-e2e, undeploy-e2e

**Decision:** Add three make targets:
- `make deploy-e2e` — runs `hack/deploy-e2e.sh`, deploys adapter with test config
- `make test-e2e` — runs Ginkgo suite in `test/e2e/` (assumes adapter is already deployed)
- `make undeploy-e2e` — deletes adapter resources

Unit tests (`make test`) remain unchanged and fast.

**Rationale:** Separation of concerns — developers can run unit tests quickly, E2E tests are opt-in and require cluster access.

**Alternative considered:** Merging E2E into `make test`. Rejected because E2E requires cluster and is much slower.

### 6. CI integration via openshift/release step-registry

**Decision:** Add a step-registry job definition in `openshift/release` repo (separate PR) that:
1. Provisions an OpenShift cluster (via ci-operator pool)
2. Deploys lightspeed-agentic-operator
3. Runs `make deploy-e2e` and `make test-e2e`
4. Collects artifacts on failure (pod logs, events, deployment status)

Job will be presubmit and optional initially (can be made required after stabilization).

**Rationale:** Standard OpenShift CI pattern. Cluster provisioning is handled by CI infrastructure.

**Alternative considered:** External CI system. Rejected to stay consistent with OpenShift project conventions.

## Risks / Trade-offs

- **[Risk] Cluster dependency** → E2E tests require a live OpenShift cluster. Mitigation: CI provisions clusters automatically; local testing documented with `oc login` requirement.
- **[Risk] Operator dependency** → Tests depend on lightspeed-agentic-operator being deployed and compatible. Mitigation: deploy script verifies operator is present; document operator version requirements.
- **[Risk] AlertManager state** → Tests may be affected by existing alerts in the cluster. Mitigation: tests use specific alert labels/receivers to isolate test alerts; cleanup after test runs.
- **[Trade-off] Test execution time** → E2E tests will be slower than unit tests (~2-5 minutes vs <10 seconds). Acceptable given the value of full integration validation.
- **[Trade-off] Real vs simulated alerts** → We rely on AlertManager's existing alerts or need to inject test alerts. Initial MVP will test against naturally-firing alerts or use AlertManager API to inject test alerts.
