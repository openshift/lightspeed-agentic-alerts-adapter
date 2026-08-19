## 1. Namespace and ServiceAccount

- [x] 1.1 Create `manifests/namespace.yaml` with the `openshift-lightspeed` Namespace resource
- [x] 1.2 Create `manifests/serviceaccount.yaml` with the `lightspeed-agentic-alerts-adapter` ServiceAccount in `openshift-lightspeed`

## 2. RBAC

- [x] 2.1 Create `manifests/rolebinding-alertmanager.yaml` with the RoleBinding in `openshift-monitoring` granting `monitoring-alertmanager-view` to the adapter's ServiceAccount
- [x] 2.2 Create `manifests/rbac.yaml` with the Role and RoleBinding in `openshift-lightspeed` (separated by `---`) granting create/list/get on `agenticruns` in `agentic.openshift.io` and binding it to the adapter's ServiceAccount

## 3. Deployment

- [x] 3.1 Create `manifests/deployment.yaml` with the Deployment resource: single replica, `lightspeed-agentic-alerts-adapter` ServiceAccount, container image, liveness and readiness probes on port 8081
