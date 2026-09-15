## Purpose
Continuously poll AlertManager for firing alerts and create AgenticRun CRs for new alerts, with stateless deduplication to avoid duplicate or premature AgenticRuns.

## Requirements
### Requirement: Poll AlertManager on a fixed interval
The system SHALL read operational parameters (`pollInterval`, `preRunDelay`, `postRunDelay`) from the `ConfigSource` at the start of each reconcile cycle and use them for that cycle's filtering and deduplication rules. The default poll interval is 30 seconds. When the loaded `pollInterval` differs from the current ticker interval, the system SHALL reset the ticker to the new interval. For each configured reconciliation target, the filter order SHALL be: receiver allowlist -> pre-run delay -> active AgenticRun -> post-run delay. At the start of each reconcile cycle, the system SHALL read the cluster-scoped `AgenticOLSConfig` singleton named `cluster`; when `spec.suspended` is true, the system SHALL skip that reconcile cycle before polling AlertManager, listing existing AgenticRuns, or creating AgenticRuns. When the `AgenticOLSConfig` CRD or singleton object is absent, the system SHALL behave as if suspended mode is disabled.

#### Scenario: Normal poll cycle
- **WHEN** suspended mode is disabled and the poll interval elapses
- **THEN** the system fetches alerts from every configured reconciliation target, lists hub AgenticRuns matching that target's identity, applies receiver filtering then dedup rules independently for that target, and creates target-identified AgenticRuns on the hub for qualifying alerts

#### Scenario: Configuration loaded each cycle
- **WHEN** suspended mode is disabled and a reconcile cycle begins
- **THEN** the system calls `ConfigSource.Load()` and uses the returned values for that cycle's pre-run delay check and post-run delay check

#### Scenario: Poll interval changes between cycles
- **WHEN** suspended mode is disabled and the `pollInterval` value from `ConfigSource.Load()` differs from the current ticker interval
- **THEN** the system resets the ticker to the new interval and logs the change

#### Scenario: Suspended mode enabled
- **WHEN** the `AgenticOLSConfig` singleton named `cluster` exists with `spec.suspended` set to true and a reconcile cycle begins
- **THEN** the system logs that the adapter is suspended and skips the reconcile cycle

#### Scenario: AgenticOLSConfig absent
- **WHEN** the `AgenticOLSConfig` CRD or singleton object named `cluster` is absent and a reconcile cycle begins
- **THEN** the system treats suspended mode as disabled and continues the normal poll cycle

#### Scenario: AgenticOLSConfig read fails
- **WHEN** reading the `AgenticOLSConfig` suspension state fails for a reason other than absent CRD or absent singleton object
- **THEN** the system logs the error and skips the reconcile cycle; the next poll retries

#### Scenario: No AlertManager polling while suspended
- **WHEN** the `AgenticOLSConfig` singleton named `cluster` exists with `spec.suspended` set to true and a reconcile cycle begins
- **THEN** the system does not call the AlertManager API

#### Scenario: No AgenticRun access while suspended
- **WHEN** the `AgenticOLSConfig` singleton named `cluster` exists with `spec.suspended` set to true and a reconcile cycle begins
- **THEN** the system does not list existing AgenticRuns and does not create any AgenticRun

#### Scenario: AlertManager unreachable during poll
- **WHEN** the AlertManager API returns an error for one target during a poll cycle
- **THEN** the system logs the target-specific error, skips that target's reconciliation, and continues the cycle for the remaining targets

#### Scenario: Kubernetes API unreachable during poll
- **WHEN** the hub Kubernetes API returns an error during AgenticRun listing or creation for one target
- **THEN** the system logs the target-specific hub operation error, skips the failed operation for that target, and continues the cycle for the remaining targets

### Requirement: Bound concurrent target reconciliation
When `--multicluster` is set, the system SHALL reconcile independent targets concurrently while limiting the number of simultaneous target reconciliations to `MULTICLUSTER_MAX_CONCURRENT_TARGETS`. The default limit SHALL be `4`. The value SHALL be a positive integer. Alert processing within a target SHALL remain sequential, and the system SHALL wait for all started target reconciliations before beginning another poll cycle.

#### Scenario: Default multicluster concurrency
- **WHEN** `--multicluster` is set and `MULTICLUSTER_MAX_CONCURRENT_TARGETS` is unset
- **THEN** the system SHALL reconcile at most four targets simultaneously

#### Scenario: Configured multicluster concurrency
- **WHEN** `--multicluster` is set and `MULTICLUSTER_MAX_CONCURRENT_TARGETS` contains a positive integer
- **THEN** the system SHALL reconcile at most that number of targets simultaneously

#### Scenario: Invalid multicluster concurrency
- **WHEN** `--multicluster` is set and `MULTICLUSTER_MAX_CONCURRENT_TARGETS` is absent of a valid positive integer
- **THEN** the system SHALL fail startup with an error identifying `MULTICLUSTER_MAX_CONCURRENT_TARGETS`

#### Scenario: Local-only deployment
- **WHEN** `--multicluster` is not set
- **THEN** the system SHALL reconcile the local target sequentially and SHALL ignore `MULTICLUSTER_MAX_CONCURRENT_TARGETS`

### Requirement: Bound individual target reconciliation time
The system SHALL apply the configured `pollInterval` as a deadline to every
target reconciliation. Alertmanager and hub Kubernetes operations for a target
SHALL use that deadline while preserving cancellation from the parent context.

#### Scenario: Target operation does not respond
- **WHEN** an Alertmanager or hub Kubernetes operation for a target does not
  complete within `pollInterval`
- **THEN** the target reconciliation context SHALL be cancelled and the
  remaining targets SHALL continue to be reconciled

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

### Requirement: Shut down gracefully on OS signals
The system SHALL exit cleanly when it receives SIGTERM or SIGINT, completing any in-flight poll cycle before stopping.

#### Scenario: SIGTERM received while idle
- **WHEN** the adapter receives SIGTERM between poll cycles
- **THEN** the adapter exits with status code 0

#### Scenario: SIGINT received during poll
- **WHEN** the adapter receives SIGINT during a poll cycle
- **THEN** the adapter completes or cancels the in-flight cycle and exits with status code 0
