## Context

See proposal.md for motivation. The adapter reconciles an alert source with the hub AgenticRun client through a target value. Remote Alertmanager credentials are supplied by Secrets referenced from cluster-scoped `hub.openshift.io/v1alpha1` `SpokeCluster` resources.

## Goals / Non-Goals

**Goals:**
- Construct an independent reconciliation target for the local cluster and every labeled SpokeCluster.
- Retrieve each spoke's Alertmanager endpoint, bearer credential, and CA bundle from its referenced Secret.
- Preserve target-scoped hub-side AgenticRun deduplication.
- Validate remote Alertmanager TLS certificates without bypassing verification.

**Non-Goals:**
- Reading kubeconfigs or accessing spoke Kubernetes APIs.
- Discovering Alertmanager Routes, requesting ServiceAccount tokens, or caching TokenRequest responses.
- Watching SpokeClusters or credential Secrets for changes after startup.
- Per-spoke adapter configuration, polling intervals, tools, agents, or receiver allowlists.
- Retrying failed target operations within a poll cycle.

## Decisions

### Discover spoke targets from SpokeClusters

At startup, list cluster-scoped `hub.openshift.io/v1alpha1` `SpokeCluster` resources. If the SpokeCluster CRD is not installed, retain only the local target. For every resource with the `hub.openshift.io/alert-credential-secret` label, construct one spoke target. The label value names the credential Secret in the adapter namespace.

This makes the hub's SpokeCluster inventory the source of spoke target configuration, rather than maintaining a separate comma-separated environment variable. The adapter uses an unstructured list because it does not otherwise depend on a Go type for the SpokeCluster API.

Treating an absent CRD as no configured spokes preserves local-only deployments. Other SpokeCluster list failures remain startup errors because they can hide configured spoke targets.

### Read Alertmanager credentials from a hub Secret

For each labeled SpokeCluster, read the referenced Secret from the adapter namespace. Its `alertmanager-url` data value is the remote Alertmanager endpoint, its `token` data value is the bearer credential, and its `ca-bundle` data value is the PEM-encoded CA bundle.

The adapter constructs the existing Alertmanager client directly from these values. A credential Secret that cannot be read or lacks any required value is logged with the SpokeCluster name and omitted; other targets, including local, still initialize.

This avoids granting the adapter access to spoke Kubernetes APIs. It also avoids the prior alternative of a kubeconfig-backed client that would discover the Route, read an ingress CA ConfigMap, and create a TokenRequest on every spoke.

### Use credential-Secret CA bundles for remote Alertmanager endpoints

The remote Alertmanager client uses the credential Secret's `ca-bundle` value as its TLS root CA pool. TLS verification remains enabled.

This avoids requiring remote ingress issuers to be installed in the adapter image's system trust store. The alternative of disabling verification was rejected because it would permit an untrusted endpoint to impersonate Alertmanager.

### Keep target initialization static

Spoke targets and their Alertmanager clients are constructed during adapter startup. The credential token is retained by that client for its lifetime. Changes to SpokeCluster labels or credential Secret data require an adapter restart to take effect.

This retains the adapter's startup-based client construction and avoids adding resource watches or per-poll Secret reads.

### Use the hub client for AgenticRun operations

Every target uses the existing hub controller-runtime client and the hub `openshift-lightspeed` namespace to list and create AgenticRuns. A spoke target uses its SpokeCluster name as the AgenticRun target identity; hub list queries include that identity, so equivalent alerts from different targets do not suppress one another.

## Risks / Trade-offs

- [The SpokeCluster CRD is installed but the adapter cannot list SpokeClusters] → Require a ClusterRole that grants `list` on `hub.openshift.io/spokeclusters`.
- [A referenced credential Secret is unavailable or malformed] → Log the SpokeCluster-specific initialization failure and continue with remaining targets.
- [A remote Alertmanager certificate is not trusted by the credential Secret's CA bundle] → Provide the issuing CA in the Secret's `ca-bundle` value; TLS verification remains enabled.
- [A credential token rotates or is revoked] → Restart the adapter after updating the credential Secret.
- [SpokeCluster labels or credential Secrets change after startup] → Restart the adapter to rebuild spoke targets.
