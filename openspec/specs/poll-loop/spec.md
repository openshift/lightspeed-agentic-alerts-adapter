## Purpose
Continuously poll AlertManager for firing alerts and create AgenticRun CRs for new alerts, with stateless deduplication to avoid duplicate or premature AgenticRuns.

## Requirements
### Requirement: Poll AlertManager on a fixed interval
The system SHALL read operational parameters (`pollInterval`, `preRunDelay`, `postRunDelay`) from the `ConfigSource` at the start of each reconcile cycle and use them for that cycle's filtering and deduplication rules. The default poll interval is 30 seconds. When the loaded `pollInterval` differs from the current ticker interval, the system SHALL reset the ticker to the new interval. The filter order SHALL be: receiver allowlist -> pre-run delay -> active AgenticRun -> post-run delay.

#### Scenario: Normal poll cycle
- **WHEN** the poll interval elapses
- **THEN** the system fetches alerts from AlertManager, lists existing AgenticRuns, applies receiver filtering then dedup rules, and creates AgenticRuns for qualifying alerts

#### Scenario: Configuration loaded each cycle
- **WHEN** a reconcile cycle begins
- **THEN** the system calls `ConfigSource.Load()` and uses the returned values for that cycle's pre-run delay check and post-run delay check

#### Scenario: Poll interval changes between cycles
- **WHEN** the `pollInterval` value from `ConfigSource.Load()` differs from the current ticker interval
- **THEN** the system resets the ticker to the new interval and logs the change

#### Scenario: AlertManager unreachable during poll
- **WHEN** the AlertManager API returns an error during a poll cycle
- **THEN** the system logs the error and skips the cycle; the next poll retries

#### Scenario: Kubernetes API unreachable during poll
- **WHEN** the Kubernetes API returns an error during AgenticRun listing or creation
- **THEN** the system logs the error and skips the cycle; the next poll retries

### Requirement: Skip transient alerts (pre-run delay)
The system SHALL not create an AgenticRun for an alert that has been firing for less than the configured `preRunDelay`, to filter out transient alerts that resolve on their own. When `preRunDelay` is 0, this check is a no-op and all alerts pass.

#### Scenario: preRunDelay is 0
- **WHEN** `preRunDelay` is 0s
- **THEN** all alerts pass the pre-run delay check regardless of how long they have been firing

#### Scenario: Alert firing for less than preRunDelay
- **WHEN** `preRunDelay` is greater than 0 and `now - alert.startsAt` is less than `preRunDelay`
- **THEN** the alert is skipped and logged at Debug level

#### Scenario: Alert firing for longer than preRunDelay
- **WHEN** `preRunDelay` is greater than 0 and `now - alert.startsAt` is equal to or greater than `preRunDelay`
- **THEN** the alert passes the pre-run delay check

### Requirement: Skip alerts with active AgenticRuns
The system SHALL not create an AgenticRun for an alert that already has an active (non-terminal) AgenticRun, identified by matching the alert fingerprint label.

#### Scenario: Active AgenticRun exists for alert
- **WHEN** an AgenticRun with matching fingerprint label exists and its phase is Pending, Analyzing, Proposed, Executing, Verifying, or Escalating
- **THEN** the alert is skipped and logged at Debug level

#### Scenario: No AgenticRun exists for alert
- **WHEN** no AgenticRun with matching fingerprint label exists
- **THEN** the alert passes the active-run check

### Requirement: Skip alerts within post-run delay
The system SHALL not create an AgenticRun for an alert that has a terminal AgenticRun (Completed, Failed, Denied, Escalated) within the configured `postRunDelay` (default 1h), to avoid repeated analysis of an alert that has already been investigated. When `postRunDelay` is 0, this check is a no-op and all alerts pass.

#### Scenario: postRunDelay is 0
- **WHEN** `postRunDelay` is 0s
- **THEN** all alerts pass the post-run delay check regardless of terminal AgenticRun timing

#### Scenario: Terminal AgenticRun within postRunDelay
- **WHEN** `postRunDelay` is greater than 0 and an AgenticRun with matching fingerprint label is in a terminal phase and its terminal condition's `LastTransitionTime` is less than `postRunDelay` ago
- **THEN** the alert is skipped and logged at Debug level

#### Scenario: Terminal AgenticRun outside postRunDelay
- **WHEN** `postRunDelay` is greater than 0 and an AgenticRun with matching fingerprint label is in a terminal phase and its terminal condition's `LastTransitionTime` is equal to or greater than `postRunDelay` ago
- **THEN** the alert passes the post-run delay check

### Requirement: Shut down gracefully on OS signals
The system SHALL exit cleanly when it receives SIGTERM or SIGINT, completing any in-flight poll cycle before stopping.

#### Scenario: SIGTERM received while idle
- **WHEN** the adapter receives SIGTERM between poll cycles
- **THEN** the adapter exits with status code 0

#### Scenario: SIGINT received during poll
- **WHEN** the adapter receives SIGINT during a poll cycle
- **THEN** the adapter completes or cancels the in-flight cycle and exits with status code 0
