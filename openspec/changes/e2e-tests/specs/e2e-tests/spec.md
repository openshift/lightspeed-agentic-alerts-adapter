## ADDED Requirements

### Requirement: E2E test environment on live OpenShift cluster
The E2E test suite SHALL run against a live OpenShift cluster with a real AlertManager (in `openshift-monitoring` namespace), a live lightspeed-agentic-operator deployed and reconciling AgenticRuns, and the alerts-adapter deployed via `hack/deploy-e2e.sh` with test-specific configuration.

#### Scenario: Test environment setup
- **WHEN** an E2E test suite begins (BeforeSuite)
- **THEN** the test verifies the adapter deployment is available, the operator is running, and AlertManager is reachable

#### Scenario: Test environment teardown
- **WHEN** an E2E test suite completes (AfterSuite)
- **THEN** debug artifacts (pod logs, events, deployment status) are collected if any test failed

### Requirement: Deploy script patches manifests
The deploy script (`hack/deploy-e2e.sh`) SHALL patch manifests with test-specific values and deploy the adapter to the cluster. The script SHALL be idempotent (safe to run multiple times).

#### Scenario: Image is patched
- **WHEN** `hack/deploy-e2e.sh` runs with `IMAGE=<test-image>`
- **THEN** the deployment manifest is patched to use `<test-image>` instead of the hardcoded `:latest`

#### Scenario: ConfigMap is patched for fast testing
- **WHEN** `hack/deploy-e2e.sh` runs
- **THEN** the ConfigMap is patched with `pollInterval: 10s`, `postRunDelay: 1m`, and `filtering.allowedReceivers: [Default]` (uncommented)

#### Scenario: Deployment waits for readiness
- **WHEN** `hack/deploy-e2e.sh` completes
- **THEN** the script waits for the adapter deployment to be available (timeout: 5 minutes)

### Requirement: Happy path reconciliation loop
The E2E test suite SHALL validate that a firing alert routed to a configured receiver results in an AgenticRun CR being created and reconciled by the live lightspeed-agentic-operator.

#### Scenario: Firing alert creates AgenticRun
- **GIVEN** a firing alert with severity `warning` is injected into all AlertManager replicas via the `/api/v2/alerts` API and routed to receiver `Default`
- **WHEN** the adapter polls AlertManager
- **THEN** an AgenticRun CR is created in the `openshift-lightspeed` namespace with labels `alert-fingerprint` and `alert-dedup-fingerprint`

### Requirement: Deduplication behavior
The E2E test suite SHALL verify that alerts are correctly skipped based on adapter's deduplication filters (severity, receiver, pre-run delay, active AgenticRun, post-run delay).

#### Scenario: Alert with severity info is skipped
- **GIVEN** a firing alert with severity `info` is injected into all AlertManager replicas
- **WHEN** the adapter polls AlertManager
- **THEN** no AgenticRun is created for this alert

#### Scenario: Alert with severity none is skipped
- **GIVEN** a firing alert with severity `none` is injected into all AlertManager replicas
- **WHEN** the adapter polls AlertManager
- **THEN** no AgenticRun is created for this alert

#### Scenario: Alert routed to non-allowed receiver is skipped
- **GIVEN** the adapter config has `filtering.allowedReceivers: [default]` and a `severity: critical` alert is injected (routed to `Critical` receiver, not in allowlist)
- **WHEN** the adapter polls AlertManager
- **THEN** no AgenticRun is created for this alert

#### Scenario: Alert with active AgenticRun is skipped
- **GIVEN** an AgenticRun with phase `Analyzing` and label `alert-dedup-fingerprint: X` and label `source: alerts-adapter` exists
- **WHEN** the adapter polls and finds a firing alert with dedup fingerprint X routed to `Default`
- **THEN** no additional AgenticRun is created

#### Scenario: Alert within postRunDelay is skipped
- **GIVEN** an AgenticRun with phase `Completed` (status patched via subresource with `Verified=True`) exists for alert dedup fingerprint X and completed less than `postRunDelay` ago (with `postRunDelay: 5m`)
- **WHEN** the adapter polls and finds a firing alert (injected) with dedup fingerprint X
- **THEN** no additional AgenticRun is created

#### Scenario: Alert outside postRunDelay creates new AgenticRun
- **GIVEN** an AgenticRun with phase `Failed` (status patched via subresource with `Analyzed=False`) exists for alert dedup fingerprint X and completed more than `postRunDelay` ago (10 minutes ago with `postRunDelay: 5m`)
- **WHEN** the adapter polls and finds a firing alert (injected) with dedup fingerprint X
- **THEN** a new AgenticRun is created

### Requirement: Fingerprint labels
The E2E test suite SHALL verify that created AgenticRun CRs have both `alert-fingerprint` (original AlertManager fingerprint) and `alert-dedup-fingerprint` (stable fingerprint computed from labels minus ignored labels) labels set correctly.

#### Scenario: Fingerprint labels are set
- **GIVEN** AlertManager returns a firing alert with fingerprint `abc123` and labels including `alertname`, `namespace`, `pod`
- **WHEN** the adapter creates an AgenticRun
- **THEN** the AgenticRun has label `alert-fingerprint: abc123` and label `alert-dedup-fingerprint: <computed-hash>` (non-empty)

#### Scenario: Dedup fingerprint ignores configured labels
- **GIVEN** the adapter config has `deduplication.ignoredLabels: [pod, instance, endpoint, uid]` and two alerts differ only in `pod` label
- **WHEN** the adapter processes both alerts
- **THEN** the first alert creates an AgenticRun with `alert-dedup-fingerprint: X`, and the second alert is skipped (because it would produce the same fingerprint X, proving deduplication works)

### Requirement: Error handling
The E2E test suite SHALL verify that the adapter handles expected errors gracefully (409 AlreadyExists, transient failures).

#### Scenario: 409 AlreadyExists on create is no-op
- **GIVEN** an AgenticRun with name X already exists in the cluster
- **WHEN** the adapter attempts to create an AgenticRun with the same name X (race condition or poll retry)
- **THEN** the create call returns 409, the adapter logs it at Info level, and the reconcile cycle completes successfully (no error reported)

### Requirement: Make targets for E2E workflow
The project SHALL provide make targets for E2E test workflow: `deploy-e2e`, `test-e2e`, `undeploy-e2e`.

#### Scenario: Deploying for E2E tests
- **WHEN** a developer runs `make deploy-e2e` with `IMAGE=<image>`
- **THEN** the adapter is deployed to the cluster with test configuration

#### Scenario: Running E2E tests
- **WHEN** a developer runs `make test-e2e` (after `make deploy-e2e`)
- **THEN** the Ginkgo E2E test suite executes and reports pass/fail status

#### Scenario: Cleaning up E2E deployment
- **WHEN** a developer runs `make undeploy-e2e`
- **THEN** all adapter resources are removed from the cluster
