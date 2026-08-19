# E2E Tests

End-to-end tests that validate the alerts-adapter against a live OpenShift cluster with real AlertManager and the lightspeed-agentic-operator.

## Prerequisites

- `oc` CLI installed and logged in to an OpenShift cluster (`oc login`)
- `yq` CLI installed (for manifest patching)
- `git` CLI installed
- AlertManager available in `openshift-monitoring` namespace (standard OCP deployment)

The deploy script automatically installs the lightspeed-agentic-operator (and its CRD) if the `AgenticRun` CRD is not already present on the cluster.

## Running Locally

The simplest way is `deploy-e2e-local`, which builds the image, pushes it to the cluster's internal registry, and deploys the adapter in one step:

```sh
# 1. Build, push, and deploy
make deploy-e2e-local

# 2. Run E2E tests
make test-e2e

# 3. Clean up
make undeploy-e2e
```

Alternatively, if you want to use an external registry:

```sh
make container-build IMAGE_NAME=quay.io/$USER/alerts-adapter IMAGE_TAG=dev
make container-push  IMAGE_NAME=quay.io/$USER/alerts-adapter IMAGE_TAG=dev
IMAGE=quay.io/$USER/alerts-adapter:dev make deploy-e2e
```

## Running in CI (ci-operator)

ci-operator handles image building automatically. The ci-operator config declares:

```yaml
images:
- dockerfile_path: Containerfile
  to: alerts-adapter
```

The step-registry ref injects the built image via a dependency:

```yaml
dependencies:
- name: "alerts-adapter"
  env: IMAGE
```

The test step receives `IMAGE` as an env var and calls `make deploy-e2e` + `make test-e2e`. No manual build or push is needed.

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `IMAGE` | **(required)** | Adapter container image accessible from the cluster |
| `NAMESPACE` | `openshift-lightspeed` | Namespace for adapter deployment |
| `DEPLOYMENT_NAME` | `lightspeed-agentic-alerts-adapter` | Adapter deployment name |
| `OPERATOR_NAMESPACE` | `openshift-lightspeed` | Namespace where the operator is deployed |
| `OPERATOR_DEPLOYMENT` | `lightspeed-agentic-operator` | Operator deployment name |
| `OPERATOR_REPO` | `https://github.com/openshift/lightspeed-agentic-operator.git` | Operator git repo (for auto-install) |

## Test Configuration

The deploy script (`hack/deploy-e2e.sh`) patches the adapter ConfigMap with test-friendly values:

- `pollInterval: 10s` (faster than default 30s)
- `postRunDelay: 1m` (shorter than default 1h)
- `filtering.allowedReceivers: [default]` (required for the adapter to process alerts)

## What's Tested

- **Happy path**: Firing alert creates AgenticRun with correct labels
- **Deduplication**: Severity filtering (info/none skipped), active run dedup, post-run delay
- **Fingerprint labels**: Both `alert-fingerprint` and `alert-dedup-fingerprint` are set
- **Error handling**: 409 AlreadyExists logged at Info level (not Error)
