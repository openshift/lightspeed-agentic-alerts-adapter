## Why

The request template renders a skill hint ("Investigate using the skill at ...") only when shared skills are configured, because `buildRequest` only receives `tools.Shared`. Skills configured at the `analysis` step level are correctly mounted on the AgenticRun CR, but the analysis agent's prompt never mentions them, so the agent does not know to use them.

## What Changes

- Pass analysis-level skill paths (in addition to shared skill paths) into `buildRequest`, so the rendered prompt includes all skills the analysis agent can actually access.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agenticrun-building`: The "Render a structured request from alert data" requirement changes to include analysis-level skill paths in the skill hint, not only shared skill paths.

## Impact

- `internal/agenticrun/build.go`: `buildRequest` signature changes to also accept analysis skills; skill paths from both sources are merged.
- `internal/agenticrun/build_test.go`: tests that verify skill hint rendering need updating.
- No config, API, or dependency changes.
