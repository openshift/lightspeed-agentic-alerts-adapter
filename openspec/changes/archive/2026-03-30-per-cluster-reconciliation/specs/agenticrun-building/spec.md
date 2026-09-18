## MODIFIED Requirements

### Requirement: Create AgenticRun resources in the cluster
The system SHALL provide a hub Kubernetes client that lists and creates AgenticRun CRs in the hub cluster for every reconciliation target. The client SHALL filter listed Alertmanager-created AgenticRuns by reconciliation-target identity and SHALL return a boolean indicating whether the AgenticRun was created, treating 409 AlreadyExists as a non-error.

#### Scenario: Successful local creation
- **WHEN** CreateAgenticRun is called with a valid AgenticRun for the local target
- **THEN** the AgenticRun is created on the local cluster, returns true and no error

#### Scenario: Successful spoke-derived creation
- **WHEN** CreateAgenticRun is called with a valid AgenticRun for a spoke target
- **THEN** the AgenticRun is created on the hub cluster with the spoke target identity label, returns true and no error

#### Scenario: AgenticRun already exists
- **WHEN** the hub cluster's Kubernetes API returns 409 AlreadyExists
- **THEN** CreateAgenticRun logs at Info level and returns false and no error

#### Scenario: Creation failure
- **WHEN** the hub cluster's Kubernetes API returns a non-409 error
- **THEN** CreateAgenticRun returns false and a wrapped error identifying the hub operation
