## Why

The adapter currently combines alerts from the local cluster and every configured spoke into one alert stream, but performs deduplication and creates AgenticRuns only against the local cluster. This can incorrectly deduplicate alerts across cluster boundaries and cannot trigger remediation in the cluster where the alert is firing.

## What Changes

- Treat the local cluster and each labeled `hub.openshift.io/v1alpha1` `SpokeCluster` as independent reconciliation targets.
- Read the `hub.openshift.io/alert-credential-secret` label from each SpokeCluster and load the named Secret from the adapter namespace.
- Query each remote Alertmanager using the Secret's `alertmanager-url`, `token`, and `ca-bundle` data values.
- Retrieve a target's alerts, list the hub's Alertmanager-created AgenticRuns for that target identity, apply the existing filtering and deduplication rules to that target only, and create eligible AgenticRuns on the hub cluster.
- Preserve local-only behavior when the SpokeCluster CRD is not installed or no SpokeCluster has the credential Secret label.
- Use target-scoped reconciliation so one target failure does not prevent healthy targets from being reconciled.
- Label hub-created AgenticRuns with the related SpokeCluster name so hub-side deduplication remains isolated per target.

## Capabilities

### New Capabilities
- `cluster-target-reconciliation`: Independently reconcile local and configured spoke clusters from alert retrieval through AgenticRun creation.

### Modified Capabilities
- `alert-retrieval`: Add authenticated, TLS-validated Alertmanager retrieval from credential-Secret endpoints while preserving in-cluster retrieval.
- `poll-loop`: Apply alert filtering and deduplication independently to every configured reconciliation target and isolate target failures.
- `agenticrun-building`: Support creating and listing Alertmanager-derived AgenticRuns on the cluster that owns the alert.

## Impact

- Affected code: `cmd/alerts-adapter/main.go`, `internal/adapter`, `internal/alertmanager`, and AgenticRun client construction.
- The `hub.openshift.io/alert-credential-secret` SpokeCluster label is the opt-in source of spoke targets.
- The adapter requires permission to list cluster-scoped SpokeClusters and read the referenced credential Secrets in its namespace.
- The adapter retains one shared runtime configuration loaded on the adapter's local cluster; per-spoke configuration is out of scope.
