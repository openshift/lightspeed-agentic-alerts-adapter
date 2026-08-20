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
| `ALERTMANAGER_URL` | `https://alertmanager-main.openshift-monitoring.svc:9094` | AlertManager API endpoint |

### ConfigMap

Runtime-tunable parameters are read from the `alerts-adapter-config` ConfigMap in the `openshift-lightspeed` namespace (key: `config.yaml`), mounted as a volume and read once at startup. The operator restarts the adapter pod when the ConfigMap changes. If the ConfigMap is missing, defaults are used. Invalid YAML or unparseable duration values cause the adapter to fail to start.

| Field | Default | Description |
|---|---|---|
| `pollInterval` | `30s` | How often to poll AlertManager |
| `preRunDelay` | `0s` | Minimum time an alert must fire before an AgenticRun is created |
| `postRunDelay` | `1h` | Minimum time after a terminal AgenticRun before retrying the same alert |
| `filtering.allowedReceivers` | `[]` | Receiver allowlist — only alerts routed to at least one of these receivers are processed (case-insensitive). Empty by default; no AgenticRuns are created until receivers are explicitly configured |
| `deduplication.ignoredLabels` | `[pod, instance, endpoint, uid]` | Labels stripped before computing the stable fingerprint for dedup matching. When set, fully replaces the defaults. Set to `[]` to include all labels |

#### Tools / Skills

Skills (OCI images with runbook paths) can be configured at a shared level or per AgenticRun step (`analysis`, `execution`, `verification`). Per-step skills override shared skills for that step.

| Field | Description |
|---|---|
| `tools.skills` | Shared skills applied to all steps |
| `analysis.tools.skills` | Skills for the analysis step only |
| `execution.tools.skills` | Skills for the execution step only |
| `verification.tools.skills` | Skills for the verification step only |

Each skills entry requires `image` (OCI image reference) and `paths` (list of paths within the image).

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
        - image: quay.io/example/shared-runbooks:latest
          paths:
            - /runbooks/common
    analysis:
      tools:
        skills:
          - image: quay.io/example/analysis-runbooks:latest
            paths:
              - /runbooks/analysis
    execution:
      tools:
        skills:
          - image: quay.io/example/exec-runbooks:latest
            paths:
              - /runbooks/exec
```

## Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) — design rationale, requirements, deployment model, and future work
- [docs/receivers.md](docs/receivers.md) — what AlertManager receivers are and how the adapter uses them for filtering
- [test/e2e/README.md](test/e2e/README.md) — how to run E2E tests locally and in CI
- [openspec/specs/](openspec/specs/) — detailed specs for each subsystem, managed with the [OpenSpec](https://github.com/Fission-AI/OpenSpec) framework

## License

[Apache License 2.0](LICENSE)
