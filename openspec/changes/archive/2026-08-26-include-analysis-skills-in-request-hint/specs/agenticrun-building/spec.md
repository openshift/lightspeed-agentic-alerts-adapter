## MODIFIED Requirements

### Requirement: Render a structured request from alert data
The system SHALL render the `spec.request` field using an embedded Go template that includes the alert name, severity, namespace, description, and runbook URL. All alert-sourced values SHALL be sanitized before template rendering by stripping Unicode control characters (except newline), Unicode format characters, and backtick runs of 3 or more. Only allow-listed fields SHALL be passed to the template; the full Labels map SHALL NOT be included in the template data. The skill hint SHALL include paths from both shared skills and analysis-level skills.

#### Scenario: Alert with all annotation fields populated
- **WHEN** the alert has summary and description annotations
- **THEN** both are included in the rendered request

#### Scenario: Alert with missing annotations
- **WHEN** the alert has no summary or description annotations
- **THEN** the corresponding fields are empty in the rendered request and no error is returned

#### Scenario: Control characters in alert data are stripped
- **WHEN** an alert label or annotation contains Unicode control characters (e.g., null bytes, escape sequences)
- **THEN** the control characters are removed from the rendered request, except for newlines which are preserved

#### Scenario: Unicode format characters in alert data are stripped
- **WHEN** an alert annotation contains Unicode format characters (e.g., zero-width spaces, bidi overrides)
- **THEN** the format characters are removed from the rendered request

#### Scenario: Backtick runs in alert data are stripped
- **WHEN** an alert annotation contains a sequence of 3 or more consecutive backtick characters
- **THEN** the backtick sequence is removed from the rendered request, while single and double backticks are preserved

#### Scenario: Extra labels are not exposed in the request
- **WHEN** an alert has labels beyond the allow-listed fields (alertname, severity, namespace)
- **THEN** those extra labels do not appear in the rendered request

#### Scenario: Skill hint includes shared skill paths
- **WHEN** shared skills are configured with paths
- **THEN** the rendered request SHALL contain the skill hint listing those paths (prefixed with `/app`)

#### Scenario: Skill hint includes analysis-level skill paths
- **WHEN** analysis-level skills are configured with paths but no shared skills are configured
- **THEN** the rendered request SHALL contain the skill hint listing the analysis skill paths (prefixed with `/app`)

#### Scenario: Skill hint includes both shared and analysis skill paths
- **WHEN** both shared skills and analysis-level skills are configured with paths
- **THEN** the rendered request SHALL contain the skill hint listing paths from both sources (each prefixed with `/app`)

#### Scenario: No skill hint when no skills configured
- **WHEN** neither shared skills nor analysis-level skills are configured
- **THEN** the rendered request SHALL contain the generic investigation instruction instead of a skill hint
