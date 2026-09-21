## MODIFIED Requirements

### Requirement: Configure independent cluster targets
The system SHALL configure a local reconciliation target unless
`ALERTMANAGER_URL` is explicitly empty. When `--multicluster` is set, it SHALL
list cluster-scoped `hub.openshift.io/v1alpha1` `SpokeCluster` resources during
startup and continuously observe them thereafter. The system SHALL configure one
spoke target for every observed SpokeCluster bearing the
`hub.openshift.io/alert-credential-secret` label. The label value SHALL name a
credential Secret in the adapter namespace.

#### Scenario: Local-only deployment
- **WHEN** `--multicluster` is not set
- **THEN** the system SHALL reconcile only the local cluster using existing in-cluster behavior

#### Scenario: SpokeCluster CRD is unavailable in multicluster mode
- **WHEN** `--multicluster` is set and the cluster does not serve the `hub.openshift.io/v1alpha1` `SpokeCluster` resource
- **THEN** the system SHALL fail startup with an error identifying SpokeCluster discovery

#### Scenario: Multiple SpokeClusters are configured
- **WHEN** `--multicluster` is set and multiple SpokeClusters bear the `hub.openshift.io/alert-credential-secret` label
- **THEN** the system SHALL configure one independent spoke target for each labeled SpokeCluster

#### Scenario: SpokeCluster is added after startup
- **WHEN** a SpokeCluster bearing the `hub.openshift.io/alert-credential-secret` label is created after multicluster startup
- **THEN** the system SHALL configure its spoke target after reading the referenced credential Secret

#### Scenario: SpokeCluster credential reference changes after startup
- **WHEN** the `hub.openshift.io/alert-credential-secret` label value of a configured SpokeCluster changes
- **THEN** the system SHALL replace its spoke target using the newly referenced credential Secret

#### Scenario: SpokeCluster credential reference is removed after startup
- **WHEN** the `hub.openshift.io/alert-credential-secret` label is removed from a configured SpokeCluster
- **THEN** the system SHALL remove its spoke target

#### Scenario: SpokeCluster is deleted after startup
- **WHEN** a configured SpokeCluster is deleted
- **THEN** the system SHALL remove its spoke target

#### Scenario: Spoke Secret cannot be loaded
- **WHEN** a referenced credential Secret cannot be read or does not contain non-empty `alertmanager-url`, `token`, and `ca-bundle` data values
- **THEN** the system SHALL report the target initialization failure with the SpokeCluster name and SHALL not configure that spoke target
