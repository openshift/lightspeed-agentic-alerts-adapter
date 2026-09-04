## MODIFIED Requirements

### Requirement: Log retrieved alerts during initial reconcile
The system SHALL fetch alerts during the initial reconcile cycle and log a summary of the results using structured logging when suspended mode is disabled. When suspended mode is enabled through `AgenticOLSConfig.spec.suspended`, the system SHALL skip alert retrieval for that reconcile cycle and SHALL log that the adapter is suspended.

#### Scenario: Alerts fetched and logged
- **WHEN** suspended mode is disabled and the initial reconcile cycle successfully retrieves alerts
- **THEN** the system logs the number of alerts retrieved and key details for each alert

#### Scenario: Alert retrieval fails during initial reconcile
- **WHEN** suspended mode is disabled and alert retrieval fails during the initial reconcile cycle
- **THEN** the system logs the error and the next poll retries

#### Scenario: Alert retrieval skipped while suspended
- **WHEN** `AgenticOLSConfig.spec.suspended` is true and a reconcile cycle begins
- **THEN** the system does not fetch alerts and logs that the adapter is suspended
