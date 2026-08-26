## Purpose
Translate Alertmanager alerts into AgenticRun custom resources so the agentic operator can act on firing alerts through its analyze-execute-verify workflow.

## Requirements
### Requirement: Build an AgenticRun CR from a single alert
The system SHALL convert an Alertmanager `GettableAlert` into an `AgenticRun` custom resource with deterministic naming, Kubernetes-safe metadata, and a templated request for the analysis agent. Two fingerprint labels SHALL be set: `agentic.openshift.io/alert-fingerprint` with the original AlertManager fingerprint (truncated to 8 characters) for UI lookups, and `agentic.openshift.io/alert-group-id` with the stable fingerprint computed from the alert's labels minus ignored labels for deduplication.

#### Scenario: Alert with namespace label
- **WHEN** the alert has a `namespace` label
- **THEN** the AgenticRun name is `{alertname}-{namespace}-{startsAt_hash}`, `spec.targetNamespaces` is set to `[namespace]`, the `agentic.openshift.io/alert-fingerprint` label is set to the original AlertManager fingerprint (truncated to 8 characters), the `agentic.openshift.io/alert-group-id` label is set to the stable fingerprint, and the AgenticRun is created in `openshift-lightspeed`

#### Scenario: Cluster-scoped alert (no namespace)
- **WHEN** the alert has no `namespace` label
- **THEN** the AgenticRun name is `{alertname}-{startsAt_hash}`, `spec.targetNamespaces` is omitted, the `agentic.openshift.io/alert-fingerprint` label is set to the original AlertManager fingerprint (truncated to 8 characters), the `agentic.openshift.io/alert-group-id` label is set to the stable fingerprint, and the AgenticRun is created in `openshift-lightspeed`

#### Scenario: Deterministic naming produces idempotent creates
- **WHEN** the same alert is passed to Build twice
- **THEN** both calls produce AgenticRuns with identical names, enabling Kubernetes 409 deduplication for the exact same alert instance

#### Scenario: Second alert for the same problem is deduplicated
- **WHEN** two alerts differ only in ignored labels (e.g., different pod names) and an active AgenticRun already exists for the first alert
- **THEN** the second alert produces the same `agentic.openshift.io/alert-group-id` label value, `hasActiveRun` matches the existing AgenticRun, and no new AgenticRun is created

### Requirement: Sanitize alert data for Kubernetes metadata
The system SHALL sanitize alert values to conform to Kubernetes naming and label restrictions.

#### Scenario: AgenticRun name contains invalid DNS characters
- **WHEN** the alertname or namespace contains characters not allowed in DNS subdomain names
- **THEN** those characters are replaced with hyphens and the result is lowercased

#### Scenario: AgenticRun name exceeds 63 characters
- **WHEN** the computed name would exceed 63 characters (the Kubernetes label value limit, since the agentic operator uses the AgenticRun name as a label value)
- **THEN** the alertname component is truncated to fit within the 63-character limit while preserving the namespace and startsAt hash suffix

#### Scenario: Label value exceeds 63 characters
- **WHEN** an alert field used as a label value exceeds 63 characters
- **THEN** the value is truncated to 63 characters and trimmed of trailing non-alphanumeric characters

#### Scenario: Label value contains invalid characters
- **WHEN** an alert field used as a label value contains characters not allowed in Kubernetes labels
- **THEN** those characters are replaced with hyphens and leading/trailing non-alphanumeric characters are trimmed

### Requirement: Render a structured request from alert data
The system SHALL render the `spec.request` field using an embedded Go template that includes the alert name, severity, namespace, description, and runbook URL. All alert-sourced values SHALL be sanitized before template rendering by stripping Unicode control characters (except newline), Unicode format characters, and backtick runs of 3 or more. Only allow-listed fields SHALL be passed to the template; the full Labels map SHALL NOT be included in the template data.

#### Scenario: Alert with all annotation fields populated
- **WHEN** the alert has summary and description annotations
- **THEN** both are included in the rendered request

#### Scenario: Alert with missing annotations
- **WHEN** the alert has no summary or description annotations
- **THEN** the corresponding fields are empty in the rendered request and no error is returned

#### Scenario: Control characters in alert data are stripped
- **WHEN** an alert label or annotation contains Unicode control characters (e.g., null bytes, escape sequences)
- **THEN** the control characters are removed from the rendered request, except for newlines which are preserved

#### Scenario: Unicode format characters in alert data are stripped
- **WHEN** an alert annotation contains Unicode format characters (e.g., zero-width spaces, bidi overrides)
- **THEN** the format characters are removed from the rendered request

#### Scenario: Backtick runs in alert data are stripped
- **WHEN** an alert annotation contains a sequence of 3 or more consecutive backtick characters
- **THEN** the backtick sequence is removed from the rendered request, while single and double backticks are preserved

#### Scenario: Extra labels are not exposed in the request
- **WHEN** an alert has labels beyond the allow-listed fields (alertname, severity, namespace)
- **THEN** those extra labels do not appear in the rendered request

### Requirement: Configure all three workflow steps with tools
The system SHALL set the analysis, execution, and verification steps on the AgenticRun, each referencing the `default` agent. The system SHALL support shared tools and per-step tool overrides.

#### Scenario: Built AgenticRun has full workflow
- **WHEN** an AgenticRun is built from any alert
- **THEN** `spec.analysis`, `spec.execution`, and `spec.verification` all have `agent` set to `"default"`

#### Scenario: Built AgenticRun with shared skills configured
- **WHEN** an AgenticRun is built and shared skills configuration is provided with one or more skills entries
- **THEN** `spec.tools.skills` SHALL contain the configured skills entries with their images and paths

#### Scenario: Built AgenticRun with per-step skills configured
- **WHEN** an AgenticRun is built and per-step skills are configured for analysis, execution, or verification
- **THEN** the corresponding `spec.{step}.tools.skills` SHALL contain the configured skills entries for that step

#### Scenario: Built AgenticRun with both shared and per-step skills
- **WHEN** an AgenticRun is built with both shared skills and per-step skills for a given step
- **THEN** `spec.tools.skills` SHALL contain the shared skills AND `spec.{step}.tools.skills` SHALL contain the per-step skills for steps that have overrides

#### Scenario: Built AgenticRun with no tools configured
- **WHEN** an AgenticRun is built and no tools configuration is provided (all slices empty)
- **THEN** `spec.tools` SHALL be omitted from the AgenticRun (zero value) and no per-step tools SHALL be set

### Requirement: List existing AgenticRuns by source
The system SHALL list AgenticRun CRs filtered by the `agentic.openshift.io/source=alertmanager` label to support deduplication queries.

#### Scenario: AgenticRuns exist
- **WHEN** ListAgenticRuns is called and AgenticRuns with the alertmanager source label exist
- **THEN** the system returns the matching AgenticRuns with their status conditions

#### Scenario: No agenticruns exist
- **WHEN** ListAgenticRuns is called and no AgenticRuns with the alertmanager source label exist
- **THEN** the system returns an empty list and no error

#### Scenario: Kubernetes API error
- **WHEN** the Kubernetes API returns an error during listing
- **THEN** ListAgenticRuns returns a wrapped error with context

### Requirement: Create AgenticRun resources in the cluster
The system SHALL provide a Kubernetes client that creates AgenticRun CRs using controller-runtime with in-cluster config. The client SHALL return a boolean indicating whether the AgenticRun was created, and treat 409 AlreadyExists as a non-error.

#### Scenario: Successful creation
- **WHEN** CreateAgenticRun is called with a valid AgenticRun
- **THEN** the AgenticRun is created in the cluster, returns true and no error

#### Scenario: AgenticRun already exists
- **WHEN** the Kubernetes API returns 409 AlreadyExists
- **THEN** CreateAgenticRun logs at Info level and returns false and no error

#### Scenario: Creation failure
- **WHEN** the Kubernetes API returns a non-409 error
- **THEN** CreateAgenticRun returns false and a wrapped error with context
