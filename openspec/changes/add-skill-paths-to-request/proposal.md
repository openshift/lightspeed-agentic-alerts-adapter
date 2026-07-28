## Why

The AgenticRun request text tells the analysis agent what to investigate, but it does not tell the agent where to find the skills mounted from OCI images. Without explicit paths in the request, the agent has no way to discover and use the configured skills for investigation.

This is a workaround because the host (Agentic Lightspeed) does not currently load skills automatically. Until the operator supports automatic skill discovery, embedding the paths in the request text is the only way for the agent to locate them.

## What Changes

- The request template includes skill paths (prefixed with `/app`) from shared skills configuration when present.
- The request template no longer outputs raw alert labels (the Labels block has been removed as redundant with the structured fields already present).
- `buildRequest` accepts the shared skills configuration and extracts all paths, prepending `/app` to each.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agenticrun-building`: The request template now conditionally includes shared skill paths so the agent knows where to find mounted skills at runtime. The Labels block is removed from the template.

## Impact

- `internal/agenticrun/build.go`: `requestData` struct gains `SkillPaths` field; `buildRequest` signature changes to accept shared skills.
- `internal/agenticrun/request.tmpl`: Template updated to list skill paths and remove Labels block.
- `internal/agenticrun/build_test.go`: New test for skill paths in request; existing test updated to remove labels assertion.
