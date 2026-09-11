## Why

The alerts adapter currently does not treat `EmergencyStopped` AgenticRuns as terminal. A stopped run can therefore continue to block processing for the same alert even though that run will not continue.

When an alert is still firing after an emergency stop condition is no longer active, the adapter should be able to create a new AgenticRun after the configured post-run delay.

## What Changes

- Treat `EmergencyStopped` as a terminal AgenticRun phase.
- Include `EmergencyStopped` runs in the normal post-run delay check.
- Allow a replacement AgenticRun to be created for the same still-firing alert after the post-run delay has elapsed.
- Preserve create-only behavior by using a deterministic retry suffix when the original AgenticRun name already exists.

## Capabilities

### New Capabilities

### Modified Capabilities

- `poll-loop`: EmergencyStopped AgenticRuns become terminal for active-run checks and participate in post-run delay.
- `agenticrun-building`: Replacement AgenticRuns for previously EmergencyStopped alert instances can use deterministic retry names.

## Impact

- `internal/adapter/adapter.go` — terminal phase and terminal-time handling for EmergencyStopped.
- `internal/agenticrun` — support deterministic replacement naming when an EmergencyStopped run already uses the original alert-derived name.
- `internal/adapter/adapter_test.go` and related builder tests — cover EmergencyStopped active-run, post-run delay, and replacement-name behavior.
