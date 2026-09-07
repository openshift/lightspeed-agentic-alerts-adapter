## Purpose
External configuration via a Kubernetes ConfigMap, allowing runtime tuning of operational parameters without restarting the adapter.

## Requirements
### Requirement: Load configuration from a ConfigMap each reconcile cycle
The system SHALL read the `alerts-adapter-config` ConfigMap from the adapter's namespace on each reconcile cycle and apply the configuration values for that cycle.

#### Scenario: ConfigMap exists with valid YAML
- **WHEN** the `alerts-adapter-config` ConfigMap exists and its `config.yaml` key contains valid YAML with valid duration values
- **THEN** the system uses the values from the ConfigMap for that reconcile cycle

#### Scenario: ConfigMap exists with partial YAML
- **WHEN** the `alerts-adapter-config` ConfigMap exists and its `config.yaml` key contains valid YAML with only some fields specified
- **THEN** the system uses the specified values and falls back to defaults for missing fields

#### Scenario: ConfigMap exists with invalid duration value
- **WHEN** the `alerts-adapter-config` ConfigMap exists and its `config.yaml` key contains an unparseable duration string for one or more fields
- **THEN** the system logs an error and returns a config load error, causing the reconcile loop to fail

#### Scenario: ConfigMap exists with explicit zero preRunDelay
- **WHEN** the `alerts-adapter-config` ConfigMap contains `preRunDelay: 0s`
- **THEN** the system uses `preRunDelay=0s` (same as default, no delay)

#### Scenario: ConfigMap exists with explicit zero postRunDelay
- **WHEN** the `alerts-adapter-config` ConfigMap contains `postRunDelay: 0s`
- **THEN** the system uses `postRunDelay=0s` (overrides the 1h default, no delay)

#### Scenario: ConfigMap exists with negative preRunDelay or postRunDelay
- **WHEN** the `alerts-adapter-config` ConfigMap contains a negative duration for `preRunDelay` or `postRunDelay`
- **THEN** the system clamps the value to `0s` (no error logged)

#### Scenario: ConfigMap exists with invalid YAML
- **WHEN** the `alerts-adapter-config` ConfigMap exists and its `config.yaml` key contains content that is not valid YAML
- **THEN** the system logs an error and returns a config load error, causing the reconcile loop to fail

#### Scenario: ConfigMap exists without the config.yaml key
- **WHEN** the `alerts-adapter-config` ConfigMap exists but does not contain a `config.yaml` key
- **THEN** the system falls back to all default values

### Requirement: Operate normally when ConfigMap does not exist
The system SHALL NOT fail or exit when the `alerts-adapter-config` ConfigMap does not exist. It SHALL use default values and continue operating.

#### Scenario: ConfigMap does not exist at startup
- **WHEN** the adapter starts and the `alerts-adapter-config` ConfigMap does not exist
- **THEN** the system starts normally using default values and logs at Info level that no ConfigMap was found

#### Scenario: ConfigMap is deleted while adapter is running
- **WHEN** the `alerts-adapter-config` ConfigMap is deleted while the adapter is running
- **THEN** the system reverts to default values on the next reconcile cycle and logs at Info level

### Requirement: Use well-defined default values
The system SHALL use the following default values when no ConfigMap is present or when individual fields are missing:
- `pollInterval`: 30 seconds
- `preRunDelay`: 0 seconds
- `postRunDelay`: 1 hour
- `allowedReceivers`: empty list (no alerts are processed until explicitly configured)

#### Scenario: All defaults applied
- **WHEN** no ConfigMap exists
- **THEN** the system uses `pollInterval=30s`, `preRunDelay=0s`, `postRunDelay=1h`, `allowedReceivers=[]`

### Requirement: Resolve the adapter namespace from the environment
The system SHALL read the `POD_NAMESPACE` environment variable to determine the namespace for the ConfigMap. If the variable is not set, it SHALL fall back to `openshift-lightspeed`.

#### Scenario: POD_NAMESPACE is set
- **WHEN** the `POD_NAMESPACE` environment variable is set to `my-namespace`
- **THEN** the system reads the ConfigMap from the `my-namespace` namespace

#### Scenario: POD_NAMESPACE is not set
- **WHEN** the `POD_NAMESPACE` environment variable is not set
- **THEN** the system reads the ConfigMap from the `openshift-lightspeed` namespace

### Requirement: Restructure config YAML with filtering and deduplication sections
The system SHALL support a structured config YAML where `allowedReceivers` is nested under a `filtering` section and `ignoredLabels` is nested under a `deduplication` section. Top-level `allowedReceivers` SHALL continue to be accepted for backward compatibility.

#### Scenario: Structured configuration is provided
- **WHEN** the ConfigMap contains `filtering.allowedReceivers` and `deduplication.ignoredLabels`
- **THEN** the system loads both values from their respective structured sections

### Requirement: Support ignored labels configuration
The system SHALL support a `deduplication.ignoredLabels` field in the ConfigMap YAML that specifies which alert labels to exclude when computing the stable fingerprint. When the field is absent, the system SHALL use the default list: `[pod, instance, endpoint, uid]`. When the field is explicitly set, the specified list fully replaces the default (no merging).

#### Scenario: ignoredLabels not specified in ConfigMap
- **WHEN** the ConfigMap does not contain a `deduplication.ignoredLabels` field
- **THEN** the system uses the default ignored labels: `pod`, `instance`, `endpoint`, `uid`

#### Scenario: ignoredLabels set to a custom list
- **WHEN** the ConfigMap contains `deduplication.ignoredLabels: [pod, instance, job]`
- **THEN** the system uses exactly `[pod, instance, job]` as the ignored labels (the defaults `endpoint` and `uid` are not included)

#### Scenario: ignoredLabels set to an empty list
- **WHEN** the ConfigMap contains `deduplication.ignoredLabels: []`
- **THEN** no labels are ignored -- all alert labels are included in the stable fingerprint hash

#### Scenario: No ConfigMap exists
- **WHEN** no ConfigMap exists
- **THEN** the system uses the default ignored labels: `pod`, `instance`, `endpoint`, `uid`

#### Scenario: allowedReceivers under filtering section
- **WHEN** the ConfigMap contains `filtering.allowedReceivers: [Critical]`
- **THEN** the system uses `[Critical]` as the allowed receivers list
