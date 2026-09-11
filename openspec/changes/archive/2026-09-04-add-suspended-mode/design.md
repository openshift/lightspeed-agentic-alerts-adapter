## Context

The adapter starts by loading file-based configuration, constructing AlertManager and Kubernetes clients, then running `adapter.Run(ctx)`. `Run` immediately performs one reconcile before starting its ticker. Each reconcile can therefore check the current cluster-wide suspension state before accessing AlertManager or `AgenticRun` resources.

`AgenticOLSConfig` is a cluster-scoped singleton named `cluster`. Its `spec.suspended` flag is the existing cluster-wide control for agentic operations. The CR is optional; when it or its CRD is absent, the operator API defines normal behavior as unsuspended.

## Goals / Non-Goals

**Goals:**
- Read `AgenticOLSConfig.spec.suspended` at the start of every reconcile cycle.
- Skip AlertManager retrieval and `AgenticRun` listing or creation when suspension is enabled.
- Treat a missing `AgenticOLSConfig` CRD or singleton object as unsuspended.
- Skip and retry the cycle when the suspension state cannot otherwise be read.
- Keep the existing poll-loop architecture without a controller-runtime manager or watch.

**Non-Goals:**
- Suspending or cancelling already-created `AgenticRun` resources.
- Changing receiver filtering, deduplication, or AgenticRun build semantics.

## Decisions

### 1. Check AgenticOLSConfig during each reconcile

The adapter defines a `SuspensionSource` interface and calls `Suspended(ctx)` as the first operation in `reconcile`. If it returns true, the adapter logs that `AgenticOLSConfig` suspension is enabled and returns before calling AlertManager or the `AgenticRun` client.

Rationale: the adapter already polls AlertManager and lists `AgenticRun` resources per cycle. A per-cycle Kubernetes read gives dynamic suspension changes effect on the next poll without adding watch, cache, manager, or shared-state lifecycle handling.

Alternative considered: controller-runtime watch. Rejected because this polling adapter does not otherwise run a manager or cache; adding a watch would require extra lifecycle and optional-CRD handling for little benefit.

### 2. Retrieve the cluster-scoped singleton by name

`internal/agenticolsconfig.Client` uses the existing controller-runtime client to get `AgenticOLSConfig` with the cluster-scoped name `cluster`. It returns `cfg.Spec.Suspended` when found.

`NotFound` and `NoMatch` errors return `false, nil`, preserving normal operation in clusters without the optional CR or CRD. Other errors are returned to the adapter, which logs them and skips the current cycle.

Rationale: a named `Get` needs only `get` authorization and follows the singleton contract. Treating only absence as unsuspended prevents remediation when the configured control state cannot be read.

### 3. Grant minimal cluster RBAC

The adapter service account receives a `ClusterRole` and `ClusterRoleBinding` granting `get` for `agentic.openshift.io/agenticolsconfigs`.

Rationale: `AgenticOLSConfig` is cluster-scoped, so a namespace-scoped Role cannot grant access. The adapter uses a known singleton name and does not need `list` or `watch`.

## Risks / Trade-offs

- **[Trade-off] Suspension changes take effect on the next poll interval** → The adapter checks the CR once per reconcile; this avoids adding a watch to the polling architecture.
- **[Risk] Suspension-state read errors prevent processing for a cycle** → Mitigation: log the error and retry at the next poll. This avoids creating remediation runs while the cluster-wide control state is unknown.
- **[Risk] Suspended pods remain healthy while doing no work** → Mitigation: log that `AgenticOLSConfig` suspension caused the cycle to be skipped.

## Migration Plan

No data migration is required. Clusters without an `AgenticOLSConfig` CRD or singleton retain normal behavior. To suspend the adapter, set `spec.suspended: true` on `AgenticOLSConfig/cluster`; set it back to `false` to allow subsequent poll cycles to create new `AgenticRun` resources.
