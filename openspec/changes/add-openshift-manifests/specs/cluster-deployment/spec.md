## ADDED Requirements

### Requirement: Manifest directory structure
The repository SHALL contain a `manifests/` directory at the root with plain YAML files for Kubernetes resources. Related resources (e.g. ClusterRole and ClusterRoleBinding) MAY be combined in a single file separated by `---`.

#### Scenario: Manifests directory exists
- **WHEN** a user clones the repository
- **THEN** the `manifests/` directory SHALL exist at the repository root containing all deployment resource files

#### Scenario: Direct application
- **WHEN** a user runs `oc apply -f manifests/`
- **THEN** all resources SHALL be created on the cluster

### Requirement: Namespace definition
The manifests SHALL include a Namespace resource for `openshift-lightspeed`.

#### Scenario: Namespace creation
- **WHEN** the manifests are applied to a cluster without the `openshift-lightspeed` namespace
- **THEN** the namespace SHALL be created

### Requirement: ServiceAccount definition
The manifests SHALL include a ServiceAccount named `lightspeed-agentic-alerts-adapter` in the `openshift-lightspeed` namespace.

#### Scenario: ServiceAccount creation
- **WHEN** the manifests are applied
- **THEN** a ServiceAccount named `lightspeed-agentic-alerts-adapter` SHALL exist in the `openshift-lightspeed` namespace

### Requirement: Deployment definition
The manifests SHALL include a Deployment named `lightspeed-agentic-alerts-adapter` in the `openshift-lightspeed` namespace with the following properties:
- Single replica
- Uses the `lightspeed-agentic-alerts-adapter` ServiceAccount
- Container image: `quay.io/openshift-lightspeed/lightspeed-agentic-alerts-adapter:latest`
- Environment variable `ALERTMANAGER_URL` set to the AlertManager in-cluster URL

#### Scenario: Deployment creation
- **WHEN** the manifests are applied
- **THEN** a Deployment with one replica SHALL be created using the specified ServiceAccount and container image

### Requirement: AlertManager RBAC
The manifests SHALL include a RoleBinding in the `openshift-monitoring` namespace that grants the adapter's ServiceAccount the `monitoring-alertmanager-view` Role, enabling read access to AlertManager.

#### Scenario: AlertManager access binding
- **WHEN** the manifests are applied
- **THEN** a RoleBinding named `lightspeed-agentic-alerts-adapter-alertmanager` SHALL exist in `openshift-monitoring` binding the `monitoring-alertmanager-view` Role to the adapter's ServiceAccount

### Requirement: AgenticRun RBAC
The manifests SHALL include a Role and RoleBinding in the `openshift-lightspeed` namespace in a single file that grant the adapter's ServiceAccount permissions to `create`, `list`, and `get` resources of type `agenticruns` in the `agentic.openshift.io` API group.

#### Scenario: AgenticRun Role
- **WHEN** the manifests are applied
- **THEN** a Role named `lightspeed-agentic-alerts-adapter-agenticruns` SHALL exist in the `openshift-lightspeed` namespace with `create`, `list`, `get` verbs on `agenticruns` in the `agentic.openshift.io` API group

#### Scenario: AgenticRun RoleBinding
- **WHEN** the manifests are applied
- **THEN** a RoleBinding in the `openshift-lightspeed` namespace SHALL bind the Role to the adapter's ServiceAccount
