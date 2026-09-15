## Purpose

Define independent reconciliation targets so alerts are evaluated per
originating OpenShift cluster and eligible AgenticRuns are created on the hub.

## Requirements

### Requirement: Configure independent cluster targets
The system SHALL configure a local reconciliation target unless `ALERTMANAGER_URL` is explicitly empty. When `--multicluster` is set, it SHALL list cluster-scoped `hub.openshift.io/v1alpha1` `SpokeCluster` resources and configure one spoke target for every SpokeCluster bearing the `hub.openshift.io/alert-credential-secret` label. The label value SHALL name a credential Secret in the adapter namespace.

#### Scenario: Local-only deployment
- **WHEN** `--multicluster` is not set
- **THEN** the system SHALL reconcile only the local cluster using existing in-cluster behavior

#### Scenario: SpokeCluster CRD is unavailable in multicluster mode
- **WHEN** `--multicluster` is set and the cluster does not serve the `hub.openshift.io/v1alpha1` `SpokeCluster` resource
- **THEN** the system SHALL fail startup with an error identifying SpokeCluster discovery

#### Scenario: Multiple SpokeClusters are configured
- **WHEN** `--multicluster` is set and multiple SpokeClusters bear the `hub.openshift.io/alert-credential-secret` label
- **THEN** the system SHALL configure one independent spoke target for each labeled SpokeCluster

#### Scenario: Spoke Secret cannot be loaded
- **WHEN** a referenced credential Secret cannot be read or does not contain non-empty `alertmanager-url`, `token`, and `ca-bundle` data values
- **THEN** the system SHALL report the target initialization failure with the SpokeCluster name and SHALL not configure that spoke target

### Requirement: Use SpokeCluster credential Secrets
For each configured spoke target, the system SHALL read the Secret named by the SpokeCluster's `hub.openshift.io/alert-credential-secret` label. The `alertmanager-url` data value SHALL be the remote Alertmanager endpoint, the `token` data value SHALL be its bearer credential, and the `ca-bundle` data value SHALL be the PEM-encoded CA bundle used to validate the endpoint's TLS certificate.

#### Scenario: Valid credential Secret
- **WHEN** the referenced credential Secret contains non-empty `alertmanager-url`, `token`, and `ca-bundle` data values
- **THEN** the system SHALL configure the remote Alertmanager client with that endpoint, bearer credential, and CA bundle

### Requirement: Reconcile each target independently
The system SHALL retrieve alerts for each reconciliation target, list hub-cluster Alertmanager-created AgenticRuns bearing that target's identity, evaluate filtering and deduplication, and create eligible AgenticRuns on the hub cluster. The system SHALL NOT use an AgenticRun bearing one target identity when reconciling another target.

#### Scenario: Same alert fires on two targets
- **WHEN** equivalent alerts fire on two independently configured targets
- **THEN** each target SHALL independently determine eligibility and may create a distinct hub-cluster AgenticRun

#### Scenario: Active AgenticRun on another target
- **WHEN** a target has an eligible alert and an equivalent hub AgenticRun bears a different target identity
- **THEN** that AgenticRun SHALL NOT suppress creation for the eligible target

### Requirement: Isolate target failures
The system SHALL continue reconciling healthy targets when alert retrieval, AgenticRun listing, or AgenticRun creation fails for another target. Failure logs SHALL identify the affected target.

#### Scenario: Spoke Alertmanager unavailable
- **WHEN** Alertmanager retrieval fails for one spoke target during a poll cycle
- **THEN** the system SHALL skip reconciliation for that target and continue reconciling the local target and other configured spokes

#### Scenario: Spoke AgenticRun API unavailable
- **WHEN** listing or creating AgenticRuns fails for one spoke target during a poll cycle
- **THEN** the system SHALL skip the failed operation for that target and continue reconciling other targets

### Requirement: Create target-identified AgenticRuns on the hub
The system SHALL create eligible AgenticRuns in the configured hub AgenticRun
namespace. Every spoke-derived AgenticRun SHALL carry a deterministic,
label-safe target identity derived from its SpokeCluster name. The identity
SHALL differ from the local target identity and SHALL not exceed the Kubernetes
label-value length limit.

#### Scenario: Eligible spoke alert
- **WHEN** an alert retrieved from a spoke target passes receiver filtering and deduplication
- **THEN** the AgenticRun SHALL be created on the hub cluster and labeled with
  that spoke target's target identity
