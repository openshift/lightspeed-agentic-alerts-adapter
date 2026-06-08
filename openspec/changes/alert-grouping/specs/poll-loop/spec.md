## Purpose
Continuously poll AlertManager for firing alert groups and create Proposal CRs, with stateless deduplication to avoid duplicate or premature Proposals.

## Requirements
### Requirement: Poll AlertManager on a fixed interval
The system SHALL poll AlertManager every 30 seconds for firing alert groups and process each group against the deduplication rules.

#### Scenario: Normal poll cycle
- **WHEN** the poll interval elapses
- **THEN** the system fetches alert groups from AlertManager, lists existing Proposals, applies dedup rules per group, and creates Proposals for qualifying groups

#### Scenario: AlertManager unreachable during poll
- **WHEN** the AlertManager API returns an error during a poll cycle
- **THEN** the system logs the error and skips the cycle; the next poll retries

#### Scenario: Kubernetes API unreachable during poll
- **WHEN** the Kubernetes API returns an error during proposal listing or creation
- **THEN** the system logs the error and skips the cycle; the next poll retries

### Requirement: Process alert groups
The system SHALL iterate over alert groups returned by AlertManager. For groups with non-empty labels, the system creates one Proposal per group. For groups with empty labels, the system falls back to creating one Proposal per individual alert in the group.

#### Scenario: Group with non-empty labels
- **WHEN** an alert group has non-empty shared labels (e.g., `alertname`, `namespace`)
- **THEN** the system creates a single Proposal for the entire group

#### Scenario: Group with empty labels (cluster-scoped fallback)
- **WHEN** an alert group has empty shared labels
- **THEN** the system processes each alert in the group individually, creating one Proposal per alert

### Requirement: Filter low-severity alerts within groups
The system SHALL filter out alerts with severity `none` or `info` from each group before Proposal creation. If no alerts remain after filtering, the group is skipped.

#### Scenario: Group with all low-severity alerts
- **WHEN** all alerts in a group have severity `none` or `info`
- **THEN** the entire group is skipped and logged at Debug level

#### Scenario: Group with mixed severities
- **WHEN** a group contains both low-severity and actionable alerts
- **THEN** the low-severity alerts are removed and the Proposal is built from the remaining alerts

### Requirement: Skip transient alert groups (initial delay)
The system SHALL not create a Proposal for an alert group where no alert has been firing for at least 5 minutes.

#### Scenario: No alert in group exceeds initial delay
- **WHEN** all alerts in the group have `now - startsAt` less than 5 minutes
- **THEN** the group is skipped and logged at Debug level

#### Scenario: At least one alert exceeds initial delay
- **WHEN** at least one alert in the group has `now - startsAt` of 5 minutes or more
- **THEN** the group passes the initial delay check

### Requirement: Skip groups with active Proposals
The system SHALL not create a Proposal for an alert group that already has an active (non-terminal) Proposal, identified by matching the `alert-group-hash` label.

#### Scenario: Active proposal exists for group
- **WHEN** a Proposal with matching `alert-group-hash` label exists and its phase is non-terminal
- **THEN** the group is skipped and logged at Debug level

#### Scenario: No proposal exists for group
- **WHEN** no Proposal with matching `alert-group-hash` label exists
- **THEN** the group passes the active-proposal check

### Requirement: Skip groups within cooldown window
The system SHALL not create a Proposal for an alert group that has a terminal Proposal (Completed, Failed, Denied, Escalated) within the cooldown window of 1 hour.

#### Scenario: Terminal proposal within cooldown
- **WHEN** a Proposal with matching `alert-group-hash` label is in a terminal phase and its terminal condition's `LastTransitionTime` is less than 1 hour ago
- **THEN** the group is skipped and logged at Debug level

#### Scenario: Terminal proposal outside cooldown
- **WHEN** a Proposal with matching `alert-group-hash` label is in a terminal phase and its terminal condition's `LastTransitionTime` is 1 hour or more ago
- **THEN** the group passes the cooldown check

### Requirement: Shut down gracefully on OS signals
The system SHALL exit cleanly when it receives SIGTERM or SIGINT, completing any in-flight poll cycle before stopping.

#### Scenario: SIGTERM received while idle
- **WHEN** the adapter receives SIGTERM between poll cycles
- **THEN** the adapter exits with status code 0

#### Scenario: SIGINT received during poll
- **WHEN** the adapter receives SIGINT during a poll cycle
- **THEN** the adapter completes or cancels the in-flight cycle and exits with status code 0
