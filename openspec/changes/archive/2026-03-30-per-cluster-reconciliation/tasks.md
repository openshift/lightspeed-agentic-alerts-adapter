## 1. Target-scoped reconciliation

- [x] 1.1 Replace the adapter's single alert source and AgenticRun client inputs with a reconciliation-target collection containing a target name, alert source, AgenticRun client, and AgenticRun namespace.
- [x] 1.2 Refactor the reconcile flow to process one target at a time, applying the current receiver, pre-run, active-run, post-run, and EmergencyStopped rules only to that target's alerts and AgenticRuns.
- [x] 1.3 Log target identity for retrieval/listing failures, creation failures, and poll summaries, and continue reconciling healthy targets after a target failure.
- [x] 1.4 Label hub AgenticRuns with their target identity and filter hub AgenticRun list queries by that identity.

## 2. Spoke target construction

- [x] 2.1 Build the local reconciliation target with the existing in-cluster Alertmanager and hub AgenticRun clients.
- [x] 2.2 When `--multicluster` is set, list cluster-scoped `hub.openshift.io/v1alpha1` `SpokeCluster` resources with the generated Lightspeed Hub API types and construct one spoke target for every resource labeled `hub.openshift.io/alert-credential-secret`.
- [x] 2.3 Read the named credential Secret in the adapter namespace and configure the spoke Alertmanager client from its `alertmanager-url`, `token`, and `ca-bundle` data values.
- [x] 2.4 Log and omit a spoke target whose credential Secret cannot be read or lacks required data while continuing startup with healthy targets.

## 3. Remote Alertmanager authentication

- [x] 3.1 Authenticate remote Alertmanager requests with the bearer token supplied by the credential Secret.
- [x] 3.2 Validate remote Alertmanager TLS certificates with the credential Secret's `ca-bundle` value.

## 4. Validation

- [x] 4.1 Add target-construction tests covering disabled multicluster discovery, enabled SpokeCluster discovery, label filtering, valid credential Secrets including `ca-bundle`, and malformed or unavailable credential Secrets.
- [x] 4.2 Add Alertmanager-client tests covering bearer tokens supplied directly through client configuration and trusted/untrusted credential-Secret CA-bundle TLS certificates.
- [x] 4.3 Run `make fmt` and `make test`; verify both commands complete successfully.
- [x] 4.4 Deploy with a labeled SpokeCluster and verify an eligible remote alert creates an AgenticRun labeled with the label-safe target identity derived from its SpokeCluster name in the hub `openshift-lightspeed` namespace.

## 5. Bounded multicluster concurrency

- [x] 5.1 Read `MULTICLUSTER_MAX_CONCURRENT_TARGETS` only when `--multicluster` is enabled; default it to `4` and reject non-positive or non-integer values at startup.
- [x] 5.2 Reconcile targets with semaphore-bounded goroutines, waiting for every started target reconciliation before completing the poll cycle while retaining sequential alert processing within each target.
- [x] 5.3 Add adapter tests that verify the configured target-concurrency limit is never exceeded and that the poll cycle waits for all target reconciliations.
