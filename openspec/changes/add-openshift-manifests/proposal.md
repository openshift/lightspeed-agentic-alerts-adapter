## Why

The adapter has no deployment manifests yet. The ARCHITECTURE.md defines the intended Kubernetes resources (Deployment, ServiceAccount, RBAC), but there are no actual YAML files to apply to a cluster. Without manifests, deploying the adapter requires manually creating each resource, which is error-prone and not reproducible.

## What Changes

- Add a `manifests/` directory with all Kubernetes/OpenShift resource definitions needed to deploy the adapter.
- Include: Namespace, ServiceAccount, Deployment, RBAC (RoleBinding for AlertManager access, Role + RoleBinding for AgenticRun management in `openshift-lightspeed`).
- Follow the resource specifications already defined in ARCHITECTURE.md.

## Capabilities

### New Capabilities

- `cluster-deployment`: Kubernetes/OpenShift manifests for deploying the adapter to a cluster, including all RBAC resources.

### Modified Capabilities

## Impact

- New `manifests/` directory at the repository root.
- No code changes — manifests only.
- Depends on the container image being available at the registry path specified in the Deployment.
- Depends on the `monitoring-alertmanager-view` Role existing in `openshift-monitoring` (provided by the OpenShift monitoring stack).
- Depends on the `agentic.openshift.io` AgenticRun CRD being installed on the cluster.
