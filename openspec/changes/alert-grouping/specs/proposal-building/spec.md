## Purpose
Translate Alertmanager alert groups into Proposal custom resources so the agentic operator can act on firing alerts through its analyze-execute-verify workflow.

## Requirements
### Requirement: Build a Proposal CR from an alert group
The system SHALL convert an Alertmanager `AlertGroup` into a `Proposal` custom resource with deterministic naming, Kubernetes-safe metadata, a templated request listing all alerts, and `spec.targetNamespaces` populated from all alerts' namespace labels.

#### Scenario: Group with single alert type and namespace
- **WHEN** the group labels include `alertname` and `namespace` and all alerts share the same alert name
- **THEN** the Proposal name is `{alertname}-{namespace}-{grouphash}`, `spec.targetNamespaces` is set to `[namespace]`, and the request lists all alerts in the group

#### Scenario: Group spanning multiple namespaces
- **WHEN** alerts in the group have different `namespace` labels
- **THEN** `spec.targetNamespaces` contains all distinct namespaces from the alerts

#### Scenario: Group with no namespace
- **WHEN** no alert in the group has a `namespace` label
- **THEN** `spec.targetNamespaces` is omitted

#### Scenario: Deterministic naming produces idempotent creates
- **WHEN** the same alert group is passed to BuildFromGroup twice
- **THEN** both calls produce Proposals with identical names, enabling Kubernetes 409 deduplication

### Requirement: Build a Proposal CR from a single alert (fallback)
The system SHALL convert a single Alertmanager `GettableAlert` into a `Proposal` custom resource using the alert's fingerprint as the group hash, for alerts that cannot be meaningfully grouped (empty group labels).

#### Scenario: Cluster-scoped alert fallback
- **WHEN** an alert comes from a group with empty labels
- **THEN** the Proposal name is `{alertname}-{fingerprint[:8]}`, the `alert-group-hash` label is set to `fingerprint[:8]`, and the request describes the single alert

#### Scenario: Alert with namespace in fallback path
- **WHEN** a fallback alert has a `namespace` label
- **THEN** the Proposal name is `{alertname}-{namespace}-{fingerprint[:8]}` and `spec.targetNamespaces` is set to `[namespace]`

### Requirement: Compute deterministic group hash
The system SHALL compute a deterministic hash from an alert group's shared labels by sorting key=value pairs alphabetically and taking the first 8 characters of the SHA-256 hex digest.

#### Scenario: Same labels produce same hash
- **WHEN** two groups have identical label sets
- **THEN** their computed hashes are identical

#### Scenario: Different labels produce different hashes
- **WHEN** two groups have different label sets
- **THEN** their computed hashes differ

### Requirement: Sanitize alert data for Kubernetes metadata
The system SHALL sanitize alert values to conform to Kubernetes naming and label restrictions.

#### Scenario: Proposal name contains invalid DNS characters
- **WHEN** the alertname or namespace contains characters not allowed in DNS subdomain names
- **THEN** those characters are replaced with hyphens and the result is lowercased

#### Scenario: Proposal name exceeds 253 characters
- **WHEN** the computed name would exceed 253 characters
- **THEN** the alertname component is truncated to fit within the limit while preserving the namespace and hash suffix

#### Scenario: Label value exceeds 63 characters
- **WHEN** an alert field used as a label value exceeds 63 characters
- **THEN** the value is truncated to 63 characters and trimmed of trailing non-alphanumeric characters

### Requirement: Set group-level labels and annotations
The system SHALL set labels for filtering/dedup and annotations for traceability on the Proposal.

#### Scenario: Labels set correctly
- **WHEN** a Proposal is built from a group
- **THEN** labels include `source=alertmanager`, `alert-group-hash=<hash>`, and `alert-severity=<highest severity in group>`

#### Scenario: Annotations set correctly
- **WHEN** a Proposal is built from a group
- **THEN** annotations include `alert-names` (JSON array of distinct alert names), `alert-starts-at` (earliest startsAt in group), and `alert-summary`

#### Scenario: Severity ranking
- **WHEN** a group contains alerts with severities `warning` and `critical`
- **THEN** the `alert-severity` label is set to `critical`

### Requirement: Render a structured request from alert group data
The system SHALL render the `spec.request` field using an embedded Go template that lists the group labels and all alerts with their individual details (name, severity, namespace, summary, description, runbook URL, labels).

#### Scenario: Group with multiple alerts
- **WHEN** the group contains 3 alerts
- **THEN** the rendered request includes group labels and all 3 alerts with their full details

#### Scenario: Group with single alert
- **WHEN** the group contains 1 alert
- **THEN** the rendered request includes the group labels and the single alert's details

### Requirement: Configure all three workflow steps
The system SHALL set the analysis, execution, and verification steps on the Proposal, each referencing the `default` agent.

#### Scenario: Built Proposal has full workflow
- **WHEN** a Proposal is built from any alert group
- **THEN** `spec.analysis`, `spec.execution`, and `spec.verification` all have `agent` set to `"default"`

### Requirement: List existing Proposals by source
The system SHALL list Proposal CRs filtered by the `agentic.openshift.io/source=alertmanager` label to support deduplication queries.

#### Scenario: Proposals exist
- **WHEN** ListProposals is called and Proposals with the alertmanager source label exist
- **THEN** the system returns the matching Proposals with their status conditions

#### Scenario: No proposals exist
- **WHEN** ListProposals is called and no Proposals with the alertmanager source label exist
- **THEN** the system returns an empty list and no error

### Requirement: Create Proposal resources in the cluster
The system SHALL provide a Kubernetes client that creates Proposal CRs using controller-runtime with in-cluster config. The client SHALL return a boolean indicating whether the Proposal was created, and treat 409 AlreadyExists as a non-error.

#### Scenario: Successful creation
- **WHEN** CreateProposal is called with a valid Proposal
- **THEN** the Proposal is created in the cluster, returns true and no error

#### Scenario: Proposal already exists
- **WHEN** the Kubernetes API returns 409 AlreadyExists
- **THEN** CreateProposal logs at Info level and returns false and no error

#### Scenario: Creation failure
- **WHEN** the Kubernetes API returns a non-409 error
- **THEN** CreateProposal returns false and a wrapped error with context
