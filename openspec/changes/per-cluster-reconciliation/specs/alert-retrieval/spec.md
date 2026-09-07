## ADDED Requirements

### Requirement: Retrieve alerts from remote Alertmanager endpoints
The system SHALL retrieve active, non-silenced, non-inhibited alerts for every configured spoke target by querying the remote Alertmanager endpoint supplied in the `alertmanager-url` data value of its credential Secret.

#### Scenario: Successful spoke alert retrieval
- **WHEN** a configured spoke target's credential Secret provides a reachable Alertmanager endpoint
- **THEN** the system SHALL return that spoke target's active, non-silenced, non-inhibited alerts

#### Scenario: Remote Alertmanager is unavailable
- **WHEN** the configured remote Alertmanager endpoint cannot be reached
- **THEN** the system SHALL return an error identifying remote Alertmanager retrieval

### Requirement: Authenticate remote Alertmanager requests with credential-Secret tokens
The system SHALL authenticate every remote Alertmanager request with the bearer token supplied in the `token` data value of the spoke target's credential Secret.

#### Scenario: Valid credential token
- **WHEN** a configured spoke target's credential Secret provides a bearer token accepted by its remote Alertmanager
- **THEN** the system SHALL use that token in the Alertmanager request authorization header

#### Scenario: Alertmanager rejects the credential token
- **WHEN** the remote Alertmanager rejects the bearer token from the credential Secret
- **THEN** the system SHALL return an authentication or authorization error for that spoke target

### Requirement: Validate remote Alertmanager TLS certificates
The system SHALL validate the TLS certificate presented by a remote Alertmanager endpoint using the PEM-encoded CA bundle in the `ca-bundle` data value of its credential Secret. The system SHALL NOT disable TLS certificate validation.

#### Scenario: Trusted remote certificate
- **WHEN** the remote Alertmanager endpoint presents a certificate trusted by the credential Secret's CA bundle
- **THEN** the system SHALL successfully establish the TLS connection

#### Scenario: Untrusted remote certificate
- **WHEN** the remote Alertmanager endpoint presents a certificate that cannot be validated by the credential Secret's CA bundle
- **THEN** the system SHALL fail the spoke alert retrieval with a TLS validation error
