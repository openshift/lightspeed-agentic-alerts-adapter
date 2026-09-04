## Purpose
Retrieve active alerts from the in-cluster Alertmanager so the adapter can translate them into actionable AgenticRun resources.

## Requirements
### Requirement: Retrieve active alerts from Alertmanager
The system SHALL query the Alertmanager API in the OpenShift cluster and return the set of currently active alerts using the types provided by the Alertmanager client library.

#### Scenario: Successful alert retrieval
- **WHEN** the adapter queries Alertmanager and alerts are firing
- **THEN** the system returns a list of alerts containing their name, severity, state, labels, annotations, and firing start time

#### Scenario: No active alerts
- **WHEN** the adapter queries Alertmanager and no alerts are currently firing
- **THEN** the system returns an empty list and no error

#### Scenario: Alertmanager unreachable
- **WHEN** the adapter attempts to query Alertmanager and the service is unreachable
- **THEN** the system returns an error indicating the Alertmanager could not be contacted

#### Scenario: Authentication failure
- **WHEN** the adapter's request to Alertmanager is rejected due to insufficient permissions or an invalid token
- **THEN** the system returns an error indicating an authentication or authorization failure

### Requirement: Authenticate using in-cluster ServiceAccount credentials
The system SHALL authenticate to the Alertmanager API using the pod's ServiceAccount bearer token and trust the OpenShift service CA certificate (`service-ca.crt`) for TLS verification.

#### Scenario: Valid in-cluster credentials
- **WHEN** the adapter runs inside a cluster with a valid ServiceAccount token and service CA certificate
- **THEN** the system uses the token for authentication and the service CA for TLS without additional configuration

#### Scenario: Missing ServiceAccount token
- **WHEN** the ServiceAccount token file is not present at the expected path
- **THEN** the system returns an error indicating the token could not be loaded

### Requirement: Configurable Alertmanager URL
The system SHALL allow the Alertmanager URL to be configured via an environment variable, with a default of `https://alertmanager-main.openshift-monitoring.svc:9094`.

#### Scenario: Custom URL provided
- **WHEN** the environment variable for the Alertmanager URL is set
- **THEN** the system uses the provided URL instead of the default

#### Scenario: No custom URL
- **WHEN** the environment variable for the Alertmanager URL is not set
- **THEN** the system uses the default in-cluster Alertmanager URL

### Requirement: Filter for actionable alerts
The system SHALL request only active, non-silenced, non-inhibited alerts from the Alertmanager API so the adapter never processes suppressed alerts.

#### Scenario: Silenced alerts excluded
- **WHEN** an alert is silenced in Alertmanager
- **THEN** the alert is not included in the response

#### Scenario: Inhibited alerts excluded
- **WHEN** an alert is inhibited by another alert in Alertmanager
- **THEN** the alert is not included in the response

#### Scenario: Resolved alerts excluded
- **WHEN** an alert has resolved and is no longer active
- **THEN** the alert is not included in the response

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
