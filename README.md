# Lightspeed Agentic Alerts Adapter

A component that bridges OpenShift cluster alerts into the [Lightspeed Agentic](https://github.com/openshift/lightspeed-agentic-operator) system. It polls the in-cluster AlertManager API for firing alerts and creates `AgenticRun` custom resources (`agentic.openshift.io/v1alpha1`) to trigger automated analysis and remediation workflows.

## Quick start

### Prerequisites

- Go 1.26+
- Access to an OpenShift cluster (for deployment)
- [golangci-lint](https://golangci-lint.run/) (for linting)

### Build

```sh
make build        # outputs ./bin/alerts-adapter
```

### Test

```sh
make test         # run all tests
make coverage     # generate HTML coverage report
make lint         # run golangci-lint
```

### Run locally

```sh
make run
```

### Container

```sh
make container-build IMAGE_NAME=quay.io/your-org/lightspeed-agentic-alerts-adapter
make container-push  IMAGE_NAME=quay.io/your-org/lightspeed-agentic-alerts-adapter
```

`container-push` depends on `container-build` — it builds and pushes in one step. `IMAGE_TAG` defaults to `latest`.

### Deploy to OpenShift

```sh
kubectl apply -f manifests/
```

The adapter runs as a single-replica Deployment in the `openshift-lightspeed` namespace, using in-cluster authentication.

## Configuration

| Environment variable | Default | Description |
|---|---|---|
| `ALERTMANAGER_URL` | `https://alertmanager-main.openshift-monitoring.svc:9094` | Local AlertManager endpoint; an explicitly empty value disables the local target |
| `POD_NAMESPACE` | `openshift-lightspeed` | Namespace for runs and spoke credential Secrets |
| `MULTICLUSTER_MAX_CONCURRENT_TARGETS` | `4` | Positive target concurrency limit when `--multicluster` is enabled |

### Suspended mode

The adapter checks the cluster-scoped `AgenticOLSConfig` singleton named `cluster` at the start of each poll cycle. When `spec.suspended` is `true`, the adapter skips that poll cycle before polling AlertManager or accessing `AgenticRun` resources.

When the `AgenticOLSConfig` CRD or singleton object is absent, the adapter behaves as if suspended mode is disabled. If reading `AgenticOLSConfig` fails for another reason, the adapter logs the error and skips the current poll cycle; the next poll retries.

Example:

```yaml
apiVersion: agentic.openshift.io/v1alpha1
kind: AgenticOLSConfig
metadata:
  name: cluster
spec:
  suspended: true
```

### ConfigMap

The adapter reads `/etc/alerts-adapter/config.yaml` once at startup. The classic Lightspeed operator enables the adapter through `OLSConfig.spec.ols.deployment.alertsAdapter.configMapRef`, mounts the referenced ConfigMap when present, and restarts the pod on configuration changes. If the file is missing, defaults are used. Invalid YAML, invalid duration syntax, or other read errors fail startup.

The direct deployment in `manifests/` requires the `alerts-adapter-config` ConfigMap. Restart that deployment after changing its data. It has no operator-managed restart. The hub deployment uses `hub-alerts-adapter-config`.

| Field | Default | Description |
|---|---|---|
| `pollInterval` | `30s` | How often to poll AlertManager |
| `preRunDelay` | `0s` | Minimum time an alert must fire before an AgenticRun is created |
| `postRunDelay` | `1h` | Minimum time after a terminal AgenticRun before retrying the same alert |
| `filtering.allowedReceivers` | `[]` | Receiver allowlist — only alerts routed to at least one of these receivers are processed (case-insensitive). Empty by default; no AgenticRuns are created until receivers are explicitly configured |
| `deduplication.ignoredLabels` | `[pod, instance, endpoint, uid]` | Labels stripped before computing the stable fingerprint for dedup matching. When set, fully replaces the defaults. Set to `[]` to include all labels |

#### Tools / Skills

Skills (OCI images with runbook paths) are configured at the run level and are available to every configured AgenticRun step.

| Field | Description |
|---|---|
| `tools.skills` | Skills applied to all configured steps |

Each skills entry requires `paths` (list of paths within the image). When `image` is omitted, the adapter uses `AGENTIC_SKILLS_IMAGE` from its environment. Explicit `image` values override that default; without either an explicit image or the env var, the entry is skipped with an error log (the adapter still starts). The direct-deployment sample ConfigMap includes an explicit image, so it does not exercise the default until you remove that field.

#### Testing the operator-provided skills image

- Run `make test` to cover explicit overrides, missing `AGENTIC_SKILLS_IMAGE`, and the image written to an `AgenticRun`.
- For an in-cluster check, use an adapter image built from this PR and install the agentic operator/`AgenticRun` CRD. The **classic** Lightspeed operator [PR #2133](https://github.com/openshift/lightspeed-operator/pull/2133) injects the env var into its adapter deployment; until that PR is deployed, set `AGENTIC_SKILLS_IMAGE` on the adapter Deployment yourself. The operator currently has no pinned skills related image, so pass a verified, pullable image digest with `--agentic-skills-image` when testing the operator path.
- Configure a skill entry with `paths` but no `image`, add an allowed Alertmanager receiver, and trigger a firing alert routed to that receiver. Inspect the resulting `AgenticRun.spec.tools.skills` for the supplied digest and paths; then check the agentic operator's created workload and events for the skill-image pull and mount. For a negative check, remove the env var, restart the adapter, and verify it logs an error and omits that skill from newly created runs. Use a new alert instance to avoid deduplication.

#### Agents

Each workflow step selects its agent using this order: `agent.<step>`, then `agent.default`, then `default`. The supported step fields are `agent.analysis`, `agent.execution`, and `agent.verification`. Empty values use the fallback. The adapter does not check whether the named Agent exists.

#### Multicluster

Start with `--multicluster` to discover and watch SpokeClusters. The local target remains enabled unless `ALERTMANAGER_URL` is explicitly empty. The hub operator sets it empty for its dedicated adapter. An absent concurrency variable uses four; an explicitly invalid value fails startup. See the [multicluster spec](.ai/spec/what/multicluster.md).

#### Example ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: alerts-adapter-config
  namespace: openshift-lightspeed
data:
  config.yaml: |
    pollInterval: "45s"
    preRunDelay: "10m"
    postRunDelay: "2h"
    filtering:
      allowedReceivers:
        - critical
        - warning
    deduplication:
      ignoredLabels:
        - pod
        - instance
        - endpoint
        - uid
    tools:
      skills:
        - paths:
            - /skills/cluster-troubleshoot/investigate-alert
```

## Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) — design rationale, requirements, deployment model, and future work
- [docs/receivers.md](docs/receivers.md) — what AlertManager receivers are and how the adapter uses them for filtering
- [test/e2e/README.md](test/e2e/README.md) — how to run E2E tests locally and in CI
- [.ai/spec/](.ai/spec/README.md) — current behavior, implementation guides, and planned corrections

## License

[Apache License 2.0](LICENSE)
