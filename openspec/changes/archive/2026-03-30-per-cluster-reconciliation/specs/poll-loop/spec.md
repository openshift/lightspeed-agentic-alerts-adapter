## MODIFIED Requirements

### Requirement: Poll AlertManager on a fixed interval
The system SHALL read operational parameters (`pollInterval`, `preRunDelay`, `postRunDelay`) from the `ConfigSource` at the start of each reconcile cycle and use them for that cycle's filtering and deduplication rules. The default poll interval is 30 seconds. When the loaded `pollInterval` differs from the current ticker interval, the system SHALL reset the ticker to the new interval. For each configured reconciliation target, the filter order SHALL be: receiver allowlist -> pre-run delay -> active AgenticRun -> post-run delay.

#### Scenario: Normal poll cycle
- **WHEN** the poll interval elapses
- **THEN** the system fetches alerts from every configured reconciliation target, lists hub AgenticRuns matching that target's identity, applies receiver filtering then dedup rules independently for that target, and creates target-identified AgenticRuns on the hub for qualifying alerts

#### Scenario: Configuration loaded each cycle
- **WHEN** a reconcile cycle begins
- **THEN** the system calls `ConfigSource.Load()` and uses the returned values for every target's pre-run delay check and post-run delay check in that cycle

#### Scenario: Poll interval changes between cycles
- **WHEN** the `pollInterval` value from `ConfigSource.Load()` differs from the current ticker interval
- **THEN** the system resets the ticker to the new interval and logs the change

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
