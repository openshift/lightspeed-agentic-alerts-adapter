## Context

The adapter is a stateless Go binary that polls AlertManager and creates AgenticRun CRs. ARCHITECTURE.md already specifies the intended Kubernetes resources (Deployment, ServiceAccount, RBAC), but no actual manifest files exist. The adapter runs as a single-replica Deployment in the `openshift-lightspeed` namespace, uses in-cluster ServiceAccount authentication, and needs RBAC for both AlertManager reads (cross-namespace) and AgenticRun writes (namespaced to `openshift-lightspeed`).

## Goals / Non-Goals

**Goals:**
- Provide a complete set of plain YAML manifests that deploy the adapter to an OpenShift cluster.
- Match the resource definitions specified in ARCHITECTURE.md exactly.
- Make deployment reproducible with `oc apply -f manifests/`.

**Non-Goals:**
- Helm chart, Kustomize, or Operator-based deployment — plain manifests applied directly are sufficient for now.
- CI/CD pipeline configuration.
- Image build automation — the Containerfile already exists.
- ConfigMap or CR-based runtime configuration — all config is currently hardcoded constants per ARCHITECTURE.md.

## Decisions

### Plain YAML without templating

Use a flat `manifests/` directory with plain YAML files, applied directly with `oc apply -f`. No Kustomize, Helm, or other templating — the deployment is small and single-environment, so the overhead isn't justified. If overlays are needed later, Kustomize can be added without restructuring.

### Group related resources into single files

Tightly coupled resources (e.g. Role + RoleBinding) go in the same file, separated by `---`. This keeps related definitions together and reduces file count without sacrificing clarity. Independent resources (Namespace, ServiceAccount, Deployment) get their own files.

### Namespace resource included

Include a `namespace.yaml` that creates `openshift-lightspeed`. This makes the manifests self-contained — applying them to a fresh cluster works without pre-creating the namespace.

## Risks / Trade-offs

- **Image tag `latest`**: The ARCHITECTURE.md example uses `latest`. This is fine for development but should be pinned to a specific digest or tag for production. This is a future concern, not in scope for this change.
- **`monitoring-alertmanager-view` Role must pre-exist**: The RoleBinding references a Role provided by the OpenShift monitoring stack. If the monitoring stack is not installed, the RoleBinding will fail. This is documented as a prerequisite.
- **AgenticRun CRD must pre-exist**: The Role references `agenticruns` in the `agentic.openshift.io` API group. The CRD must be installed (via the lightspeed-agentic-operator) before the adapter's RBAC is meaningful.
