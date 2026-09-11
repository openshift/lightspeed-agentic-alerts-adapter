## MODIFIED Requirements

### Requirement: Skip alerts with active AgenticRuns
The system SHALL not create an AgenticRun for an alert that already has an active (non-terminal) AgenticRun, identified by matching the alert fingerprint label. `EmergencyStopped` SHALL be treated as terminal, not active.

#### Scenario: Active AgenticRun exists for alert
- **WHEN** an AgenticRun with matching fingerprint label exists and its phase is Pending, Analyzing, Proposed, Executing, Verifying, or Escalating
- **THEN** the alert is skipped and logged at Debug level

#### Scenario: EmergencyStopped AgenticRun exists for alert
- **WHEN** the only AgenticRun with matching fingerprint label is in phase EmergencyStopped
- **THEN** the alert passes the active-run check

#### Scenario: No AgenticRun exists for alert
- **WHEN** no AgenticRun with matching fingerprint label exists
- **THEN** the alert passes the active-run check

### Requirement: Skip alerts within post-run delay
The system SHALL not create an AgenticRun for an alert that has a terminal AgenticRun (Completed, Failed, Denied, Escalated, EmergencyStopped) within the configured `postRunDelay` (default 1h), to avoid repeated analysis of an alert that has recently reached a terminal state. When `postRunDelay` is 0, this check is a no-op and all alerts pass.

#### Scenario: postRunDelay is 0
- **WHEN** `postRunDelay` is 0s
- **THEN** all alerts pass the post-run delay check regardless of terminal AgenticRun timing

#### Scenario: Terminal AgenticRun within postRunDelay
- **WHEN** `postRunDelay` is greater than 0 and an AgenticRun with matching fingerprint label is in a terminal phase and its terminal condition's `LastTransitionTime` is less than `postRunDelay` ago
- **THEN** the alert is skipped and logged at Debug level

#### Scenario: Terminal AgenticRun outside postRunDelay
- **WHEN** `postRunDelay` is greater than 0 and an AgenticRun with matching fingerprint label is in a terminal phase and its terminal condition's `LastTransitionTime` is equal to or greater than `postRunDelay` ago
- **THEN** the alert passes the post-run delay check

#### Scenario: EmergencyStopped AgenticRun within postRunDelay
- **WHEN** `postRunDelay` is greater than 0 and an AgenticRun with matching fingerprint label is in phase EmergencyStopped and its EmergencyStopped condition's `LastTransitionTime` is less than `postRunDelay` ago
- **THEN** the alert is skipped and logged at Debug level

#### Scenario: EmergencyStopped AgenticRun outside postRunDelay
- **WHEN** `postRunDelay` is greater than 0 and an AgenticRun with matching fingerprint label is in phase EmergencyStopped and its EmergencyStopped condition's `LastTransitionTime` is equal to or greater than `postRunDelay` ago
- **THEN** the alert passes the post-run delay check
