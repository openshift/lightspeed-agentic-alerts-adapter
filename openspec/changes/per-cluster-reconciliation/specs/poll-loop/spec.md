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
