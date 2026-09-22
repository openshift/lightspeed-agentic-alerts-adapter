# agenticrun-creation-backoff Specification

## Purpose

Limits repeated AgenticRun creation failures per alert group while allowing automatic recovery after the failure condition clears.

## Requirements

### Requirement: Per-alert-group creation failure backoff
The adapter SHALL defer repeated AgenticRun creation attempts after a creation error. Backoff state MUST be isolated by reconciliation target and stable alert-group identity, and it MUST increase exponentially from an initial delay to a bounded maximum delay.

#### Scenario: Repeated failure for one alert group
- **WHEN** an AgenticRun creation attempt for an eligible alert group returns an error
- **THEN** the adapter MUST defer the next creation attempt for that target and alert group until its current backoff delay has elapsed

#### Scenario: Independent alert group is not delayed
- **WHEN** one target and alert group is within its creation backoff delay
- **THEN** the adapter MUST continue evaluating and creating AgenticRuns for other eligible target and alert-group identities

### Requirement: Backoff recovery and compatibility
The adapter SHALL clear an alert group's creation backoff after a creation request completes without an error. An AlreadyExists response MUST retain its existing no-op behavior and MUST NOT advance backoff. Errors outside AgenticRun creation MUST NOT use creation backoff.

#### Scenario: Creation succeeds after prior failure
- **WHEN** an AgenticRun creation attempt succeeds after that target and alert group entered backoff
- **THEN** the adapter MUST clear its backoff state and allow a future failure to begin at the initial delay

#### Scenario: AgenticRun already exists
- **WHEN** an AgenticRun creation request finds that the AgenticRun already exists
- **THEN** the adapter MUST treat the result as a no-op and MUST NOT add a creation backoff delay

#### Scenario: Alert retrieval fails
- **WHEN** retrieving alerts fails
- **THEN** the adapter MUST retain its existing alert retrieval error handling without adding creation backoff state

### Requirement: Backoff lifecycle observability
The adapter SHALL log when an alert group enters creation backoff, when its delay increases, and when its backoff is cleared. Each such log entry MUST include the target, alert identity, and applicable backoff duration.

#### Scenario: Initial creation error
- **WHEN** a creation error creates backoff state for an alert group
- **THEN** the adapter MUST log that the alert group entered backoff and include the initial delay

#### Scenario: Failure after a delay expires
- **WHEN** a subsequent creation attempt for an alert group fails after its current backoff delay has elapsed
- **THEN** the adapter MUST log the increased delay

#### Scenario: Backoff is cleared
- **WHEN** a no-error creation response clears existing backoff state
- **THEN** the adapter MUST log that the alert group left backoff and include the prior delay
