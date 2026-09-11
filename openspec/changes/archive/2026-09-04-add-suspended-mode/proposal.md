## Why

Operators need a way to temporarily disable the alerts adapter without deleting the Deployment, changing AlertManager routing, or risking new automated remediation activity. The cluster-scoped `AgenticOLSConfig` singleton provides the existing operational kill switch.

## What Changes

- Read the `AgenticOLSConfig` singleton named `cluster` at the start of each reconcile cycle.
- When `spec.suspended` is true, skip the cycle before polling AlertManager or listing or creating `AgenticRun` resources.
- Treat an absent `AgenticOLSConfig` CRD or singleton object as unsuspended.
- Log and skip the current cycle when the suspension state cannot otherwise be read; retry on the next poll.
- Grant the adapter service account cluster-scoped `get` permission for `AgenticOLSConfig`.

## Capabilities

### New Capabilities

### Modified Capabilities
- `poll-loop`: Add a suspended mode exception to the normal reconcile loop so no polling or AgenticRun creation occurs while suspended.
- `alert-retrieval`: Clarify that AlertManager retrieval is not attempted during a suspended reconcile cycle.

## Impact

- `cmd/alerts-adapter/main.go` — construct and wire an `AgenticOLSConfig` client into the adapter.
- `internal/adapter/` — check suspension before AlertManager or AgenticRun access.
- `internal/agenticolsconfig/` — retrieve the cluster-scoped singleton and handle its optional presence.
- `manifests/rbac.yaml` — allow reading the cluster-scoped `AgenticOLSConfig`.
- Documentation and tests for cycle-level suspension behavior.
