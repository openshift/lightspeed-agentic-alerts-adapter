## Context

See proposal.md for motivation. The adapter currently builds a fixed target slice
before the poll loop starts. In multicluster mode, it already uses the generated
SpokeCluster API type and fails startup when the SpokeCluster resource cannot be
listed.

## Goals / Non-Goals

**Goals:**
- Maintain the configured spoke targets as SpokeClusters are created, updated,
  relabeled, and deleted after startup.
- Preserve the existing initial target discovery and failure behavior when
  `--multicluster` is enabled.
- Keep alert reconciliation ticker-driven and target-scoped.

**Non-Goals:**
- Watching credential Secrets.
- Applying a credential Secret change unless its referencing SpokeCluster is
  reconciled or the adapter restarts.
- Triggering alert reconciliation directly from a SpokeCluster event.
- Changing local-only behavior when `--multicluster` is unset.

## Decisions

### Run a SpokeCluster controller only in multicluster mode

When `--multicluster` is enabled, start a controller-runtime manager configured
to watch the generated `hub.openshift.io/v1alpha1` SpokeCluster type.
`internal/multicluster` owns the SpokeCluster reconciler, spoke-target
construction, and the dynamic spoke-target registry. The command package
remains responsible only for creating the manager, passing dependencies, and
managing process lifetime. The existing synchronous startup list constructs the
initial targets and continues to fail startup if the resource cannot be listed.
The controller handles subsequent changes.

The manager is not started in local-only mode, preserving the current CRD and
RBAC requirements for that mode. A periodic list was considered, but a watch
avoids repeated SpokeCluster and Secret API reads when configuration is stable.

### Reconcile one SpokeCluster into a target-registry entry

A SpokeCluster reconciler in `internal/multicluster` uses the object name as
the registry key. For an existing object, it reads the credential Secret named by the
`hub.openshift.io/alert-credential-secret` label using a direct API reader.
When the label is absent, the Secret cannot be read, its data is invalid, or
the Alertmanager client cannot be constructed, the reconciler removes the
registry entry. Otherwise it constructs a replacement target and stores it.

When the SpokeCluster is no longer found, the reconciler removes the matching
registry entry. This makes delete handling independent of delete-event object
contents.

The multicluster package receives its Kubernetes readers, hub AgenticRun
client, target registry, namespace, and logger explicitly. The Secret is read
directly rather than through the manager cache so this design does not create
a Secret informer or require Secret watch permission.

### Publish immutable target snapshots to the poll loop

Keep local targets fixed and maintain spoke targets in an
`internal/multicluster` mutex-protected registry. The registry implements the
adapter's target-source interface. At the beginning of each poll cycle, the
adapter obtains a copied snapshot consisting of local targets and the current
spoke targets. A SpokeCluster reconciliation constructs a complete replacement
target before publishing it, so a poll cycle in progress continues with its
original snapshot.

The existing target concurrency limit and per-target reconciliation behavior
remain unchanged. SpokeCluster events only alter the target set; they do not
start a poll cycle.

## Risks / Trade-offs

- [A credential Secret changes without a SpokeCluster event] → The active
  target retains its prior credentials until the SpokeCluster is updated or the
  adapter restarts; Secret watching is intentionally out of scope.
- [A SpokeCluster event is delivered while a poll is running] → That poll uses
  its initial target snapshot; the following poll uses the replacement or
  removal.
- [The watch connection is interrupted] → Use the controller-runtime
  list-and-watch mechanism, which restores its watch and reconciles current
  objects after reconnection.
- [The controller manager exits unexpectedly] → Treat it as an adapter fatal
  error and stop the poll loop.
