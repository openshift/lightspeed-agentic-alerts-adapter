## MODIFIED Requirements

### Requirement: Poll AlertManager on a fixed interval
The system SHALL read operational parameters (`pollInterval`, `preRunDelay`, `postRunDelay`) from the `ConfigSource` at the start of each reconcile cycle and use them for that cycle's filtering and deduplication rules. The default poll interval is 30 seconds. When the loaded `pollInterval` differs from the current ticker interval, the system SHALL reset the ticker to the new interval. The filter order SHALL be: receiver allowlist -> pre-run delay -> active AgenticRun -> post-run delay. At the start of each reconcile cycle, the system SHALL read the cluster-scoped `AgenticOLSConfig` singleton named `cluster`; when `spec.suspended` is true, the system SHALL skip that reconcile cycle before polling AlertManager, listing existing AgenticRuns, or creating AgenticRuns. When the `AgenticOLSConfig` CRD or singleton object is absent, the system SHALL behave as if suspended mode is disabled.

#### Scenario: Normal poll cycle
- **WHEN** suspended mode is disabled and the poll interval elapses
- **THEN** the system fetches alerts from AlertManager, lists existing AgenticRuns, applies receiver filtering then dedup rules, and creates AgenticRuns for qualifying alerts

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
- **WHEN** suspended mode is disabled and the AlertManager API returns an error during a poll cycle
- **THEN** the system logs the error and skips the cycle; the next poll retries

#### Scenario: Kubernetes API unreachable during poll
- **WHEN** suspended mode is disabled and the Kubernetes API returns an error during AgenticRun listing or creation
- **THEN** the system logs the error and skips the cycle; the next poll retries
