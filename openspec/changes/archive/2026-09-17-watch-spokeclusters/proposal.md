## Why

Multicluster targets are discovered only during adapter startup. A SpokeCluster
created, deleted, or relabeled after startup currently requires an adapter
restart before its target configuration takes effect.

## What Changes

- When `--multicluster` is enabled, watch `hub.openshift.io/v1alpha1`
  SpokeCluster resources after initial discovery.
- Add, replace, or remove a spoke reconciliation target in response to
  SpokeCluster lifecycle and credential-label changes.
- Read the referenced credential Secret when reconciling a SpokeCluster.
- Keep Secret changes independent of SpokeCluster changes out of scope; they
  require an adapter restart or SpokeCluster update.
- Preserve ticker-driven alert reconciliation; resource events do not trigger
  an alert poll.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cluster-target-reconciliation`: Dynamically maintain multicluster spoke
  targets from SpokeCluster lifecycle and credential-label events.

## Impact

- Affected code: adapter target snapshots, `cmd/alerts-adapter/main.go`, and
  a new `internal/multicluster` package for spoke target construction and
  SpokeCluster reconciliation.
- Adds watch permission for cluster-scoped `hub.openshift.io` SpokeClusters.
- Uses the existing generated Lightspeed Hub SpokeCluster API type and
  controller-runtime dependency.
