# Multicluster

Configures independent reconciliation targets from hub SpokeClusters so alerts are evaluated per originating OpenShift cluster and eligible AgenticRuns are created on the hub.

For cross-repo multicluster behavioral rules (credential flow, hub-spoke interaction model), see the parent spec at `ols/.ai/spec/what/alerts-adapter-multicluster.md`.

## Behavioral Rules

### Target Configuration

1. The adapter SHALL configure a local reconciliation target unless `ALERTMANAGER_URL` is explicitly set to empty.
2. When `--multicluster` is set, the adapter SHALL list cluster-scoped `hub.openshift.io/v1alpha1` `SpokeCluster` resources during startup and continuously observe them thereafter.
3. The adapter SHALL configure one spoke target for every observed SpokeCluster bearing the `hub.openshift.io/alert-credential-secret` label. The label value names a credential Secret in the adapter namespace.
4. When `--multicluster` is not set, the adapter SHALL reconcile only the local cluster.
5. When `--multicluster` is set and the cluster does not serve the `SpokeCluster` resource, the adapter SHALL fail startup with an error identifying SpokeCluster discovery.

### Dynamic Target Updates

6. When a SpokeCluster bearing the credential label is created after startup, the adapter SHALL configure its spoke target after reading the referenced credential Secret.
7. When the credential label value of a configured SpokeCluster changes, the adapter SHALL replace its spoke target using the newly referenced Secret.
8. When the credential label is removed from a configured SpokeCluster, the adapter SHALL remove its spoke target.
9. When a configured SpokeCluster is deleted, the adapter SHALL remove its spoke target from future target snapshots. Work already included in a cycle may continue until it finishes, times out, or the process is stopped. This describes current target removal, not a race-safe drain gate; planned rules 18–23 add suppression before final deletion and coordinated execution enforcement.
10. When a referenced credential Secret cannot be read or does not contain non-empty `alertmanager-url`, `token`, and `ca-bundle` data values, the adapter SHALL report the failure with the SpokeCluster name and SHALL NOT configure that spoke target.

### Credential Secrets

11. For each spoke target, the adapter SHALL read the Secret named by the `hub.openshift.io/alert-credential-secret` label. The `alertmanager-url` data value is the remote AlertManager endpoint, `token` is the bearer credential, and `ca-bundle` is the PEM-encoded CA bundle for TLS validation.

11a. The adapter SHALL refresh a target's credentials when the SpokeCluster is reconciled. Restarting the adapter also reloads credentials. A Secret-only change or an AlertManager connection error SHALL NOT directly trigger a credential refresh in the current implementation.
11b. [PLANNED] The adapter SHALL re-fetch changed credentials and retry after an authentication or connection failure. This recovery path is not implemented.

### Independent Reconciliation

12. The adapter SHALL retrieve alerts for each target, list hub-cluster Alertmanager-created AgenticRuns bearing that target's identity, evaluate filtering and deduplication, and create eligible AgenticRuns on the hub cluster.
13. The adapter SHALL NOT use an AgenticRun bearing one target identity when reconciling another target.
14. When equivalent alerts fire on two targets, each SHALL independently determine eligibility and may create a distinct hub-cluster AgenticRun.

### Target Failure Isolation

15. The adapter SHALL continue reconciling healthy targets when alert retrieval, AgenticRun listing, or AgenticRun creation fails for another target. Failure logs SHALL identify the affected target.

### Target-Identified AgenticRuns

16. Every spoke-derived AgenticRun SHALL carry a deterministic, label-safe target identity derived from its SpokeCluster name. The identity SHALL differ from the local target identity and SHALL not exceed the Kubernetes label-value length limit.
17. Long SpokeCluster names are truncated and suffixed with a SHA-256 hash fragment to stay within the 63-character label value limit while remaining unique.

### Protected Fleet/Spoke Drain [PLANNED: OLS-4660, OLS-4785]

18. The adapter SHALL consume the reviewed shared fleet/per-spoke gate and suppress polling and new AgenticRun creation for affected targets during HubConfig/SpokeCluster deletion or registry-mode-change unmanagement (OLS-4781). A credential label/Secret on a staged registration without active HubConfig SHALL NOT authorize polling. These are planned requirements, not claims that current label-based discovery enforces activation.
19. Fleet drain SHALL suppress all spoke targets. Individual deletion/mode-change drain SHALL suppress only affected targets; other healthy eligible spokes SHALL retain their existing behavior. During any drain, the local reconciliation target SHALL retain its existing behavior. Graceful fleet drain SHALL NOT be implemented by globally suspending AgenticOLSConfig; the existing suspension behavior in [poll-loop.md](poll-loop.md#suspended-mode) remains separate.
20. When gate closure races an in-flight target cycle, the adapter SHALL suppress new AgenticRun submissions. A target snapshot or pre-submit check alone does not establish end-to-end drain safety; execution gating, manual submissions and the admitted-run boundary are governed by the [parent drain contract](https://github.com/openshift/ols/blob/main/.ai/spec/what/multicluster-ops.md#shared-interfaces-and-delivery-ownership-planned-ols-4659-ols-4660). The adapter SHALL NOT cancel, update or delete previously admitted workflows.
21. Stopping polling SHALL NOT request early credential removal. Existing workflows and credential-consuming cleanup retain necessary access until shared completion evidence permits cleanup. Credential presence SHALL NOT reopen polling while the gate is closed.
22. Suppression SHALL survive adapter restart, target refresh and concurrent label/Secret observations, and reject stale object identity/freshness state. S0 review with OLS-4659/4660 finalizes the gate API, admitted-run boundary and evidence; this specification does not invent condition names or wire identifiers. Missing fleet configuration never authorizes new spoke work; the local target is unaffected by fleet absence/drain.
23. Integration fixtures SHALL cover closure after alert retrieval but before creation, concurrent target replacement, stale identity, restart and refresh. OLS-4788 real-spoke tests SHALL prove new-work suppression while admitted workflows finish, approval blockers remain visible, credentials survive cleanup, and unaffected spokes/local work continue. Hub-only/fake-client tests are not proof of remote drain safety. Prerequisite configuration deletion (OLS-4782) and durable MCE exclusion remain outside this phase.
