## Purpose
Retrieve active alert groups from the in-cluster Alertmanager so the adapter can translate them into actionable Proposal resources.

## Requirements
### Requirement: Retrieve active alert groups from Alertmanager
The system SHALL query the Alertmanager groups API in the OpenShift cluster and return the set of currently active alert groups using the types provided by the Alertmanager client library.

#### Scenario: Successful alert group retrieval
- **WHEN** the adapter queries Alertmanager and alerts are firing
- **THEN** the system returns a list of alert groups, each containing shared group labels and a list of alerts with their name, severity, state, labels, annotations, and firing start time

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
The system SHALL request only active, non-silenced, non-inhibited alert groups from the Alertmanager API so the adapter never processes suppressed alerts.

#### Scenario: Silenced alerts excluded
- **WHEN** an alert is silenced in Alertmanager
- **THEN** the alert is not included in the response

#### Scenario: Inhibited alerts excluded
- **WHEN** an alert is inhibited by another alert in Alertmanager
- **THEN** the alert is not included in the response

#### Scenario: Resolved alerts excluded
- **WHEN** an alert has resolved and is no longer active
- **THEN** the alert is not included in the response

### Requirement: Log retrieved alert groups on startup
The system SHALL fetch alert groups during startup and log a summary of the results using structured logging.

#### Scenario: Alert groups fetched and logged
- **WHEN** the adapter starts and successfully retrieves alert groups
- **THEN** the system logs the number of groups and alerts retrieved

#### Scenario: Alert group retrieval fails on startup
- **WHEN** the adapter starts and alert group retrieval fails
- **THEN** the system logs the error and exits with a non-zero status code
