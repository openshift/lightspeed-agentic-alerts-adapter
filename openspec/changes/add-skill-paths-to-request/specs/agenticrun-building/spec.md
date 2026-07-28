## MODIFIED Requirements

### Requirement: Render a structured request from alert data
The system SHALL render the `spec.request` field using an embedded Go template that includes the alert name, severity, namespace, description, and runbook URL. When shared skills are configured, the request SHALL include all skill paths prefixed with `/app` so the agent can locate mounted skills at runtime.

#### Scenario: Alert with all annotation fields populated
- **WHEN** the alert has summary and description annotations
- **THEN** both are included in the rendered request

#### Scenario: Alert with missing annotations
- **WHEN** the alert has no summary or description annotations
- **THEN** the corresponding fields are empty in the rendered request and no error is returned

#### Scenario: Shared skills configured with one source and multiple paths
- **WHEN** an AgenticRun is built with shared skills containing one source with paths `/skills/cluster-troubleshoot/investigate-alert` and `/skills/prometheus`
- **THEN** the request SHALL contain "Investigate using the skill at" followed by `/app/skills/cluster-troubleshoot/investigate-alert` and `/app/skills/prometheus`

#### Scenario: Shared skills configured with multiple sources
- **WHEN** an AgenticRun is built with shared skills containing multiple sources, each with their own paths
- **THEN** the request SHALL contain "Investigate using the skill at" followed by all paths from all sources, each prefixed with `/app`

#### Scenario: No shared skills configured
- **WHEN** an AgenticRun is built with no shared skills
- **THEN** the request SHALL NOT contain "Investigate using the skill at"
