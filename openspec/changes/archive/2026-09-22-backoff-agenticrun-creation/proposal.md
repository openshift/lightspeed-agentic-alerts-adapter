## Why

The adapter retries every AgenticRun creation error for each eligible alert group on every poll cycle. Persistent failures therefore generate repeated API requests and log noise. Per-alert-group backoff reduces this load while preserving automatic recovery when creation becomes possible.

## What Changes

- Add exponential backoff for AgenticRun creation failures, scoped independently to each reconciliation target and stable alert group.
- Apply backoff to all creation errors; retain the existing no-op behavior for AlreadyExists responses.
- Clear backoff after a successful or AlreadyExists creation response and log backoff entry, increases, and exit.
- Retain current behavior for non-creation reconciliation failures.

## Capabilities

### New Capabilities
- `agenticrun-creation-backoff`: Throttle repeated AgenticRun creation failures per target and alert group.

### Modified Capabilities

None.

## Impact

- Affected code: `internal/adapter/adapter.go` and its tests.
- Uses the existing `k8s.io/client-go/util/flowcontrol` dependency; no configuration or public API changes.
- Creation failures retry on an exponential schedule instead of every poll cycle.
