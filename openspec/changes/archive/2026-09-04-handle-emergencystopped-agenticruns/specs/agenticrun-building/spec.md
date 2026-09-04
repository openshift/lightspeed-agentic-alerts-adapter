## ADDED Requirements

### Requirement: Build replacement AgenticRuns after EmergencyStopped
The system SHALL support creating a replacement AgenticRun for a still-firing alert when the original alert-derived AgenticRun name already exists for an EmergencyStopped run and the alert is eligible for creation.

#### Scenario: Replacement AgenticRun after EmergencyStopped
- **WHEN** an alert is still firing, the original deterministic AgenticRun name already exists for that alert instance, and the existing matching run is EmergencyStopped outside the configured post-run delay
- **THEN** the system creates a replacement AgenticRun using the next deterministic retry name

#### Scenario: Retry name preserves Kubernetes constraints
- **WHEN** a replacement AgenticRun name requires a retry suffix
- **THEN** the final name remains within the existing Kubernetes name length limit

#### Scenario: Deterministic retry naming
- **WHEN** the same set of existing AgenticRuns is evaluated for the same alert instance
- **THEN** the same next retry name is selected
