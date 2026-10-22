# Configuration

The adapter reads application settings from `/etc/alerts-adapter/config.yaml` once at startup. A deployment can mount a ConfigMap at `/etc/alerts-adapter`. HTTPS listener settings come from environment variables. Changes take effect after a process restart.

## Behavioral Rules

### File Loading

1. The adapter SHALL read `/etc/alerts-adapter/config.yaml` at startup and use the loaded settings for the lifetime of the process.
2. When the file contains valid YAML and valid duration values, the adapter SHALL use those values.
3. When only some fields are specified, the adapter SHALL use defaults for missing fields.
4. An invalid duration string SHALL cause a config load error. The adapter SHALL log the error and exit with status 1 before starting the poll loop.
5. Invalid YAML or a file read error other than a missing file SHALL also cause startup to fail with status 1.
6. An empty file, or a mounted ConfigMap without a `config.yaml` key, SHALL result in default settings.
7. When the file is missing at startup, the adapter SHALL use defaults and log at Info level.
8. Changing or deleting the file while the adapter is running SHALL leave the active settings unchanged. The next process start SHALL load the file again.

### Default Values

9. Defaults are `pollInterval=30s`, `preRunDelay=0s`, `postRunDelay=1h`, and `filtering.allowedReceivers=[]`.

### Duration Clamping

10. An explicit `preRunDelay=0s` SHALL disable the pre-run delay.
11. An explicit `postRunDelay=0s` SHALL override the one-hour default and disable cooldown.
12. Negative pre-run or post-run delays SHALL be clamped to zero without an error log.
12a. A zero or negative `pollInterval` SHALL cause an error log and use the default interval of 30 seconds.

### Namespace Resolution

13. `POD_NAMESPACE` SHALL select the namespace for AgenticRun creation and listing and for spoke credential Secrets. An unset or empty value SHALL use `openshift-lightspeed`. The mounted configuration path SHALL be independent of this value.

### Structured Configuration Sections

14. The adapter SHALL accept `filtering.allowedReceivers` and `deduplication.ignoredLabels` in their respective sections.
15. Top-level `allowedReceivers` SHALL remain accepted for compatibility. When both forms are present, `filtering.allowedReceivers` SHALL take precedence, including an explicit empty list. Receiver names SHALL be compared without case sensitivity.
15a. Receiver allowlists, pre-run delay, and post-run delay SHALL govern polling only. API investigations SHALL use the same configured ignored labels, tools, and agents, but bypass those polling filters and delays.

### Ignored Labels

16. When `deduplication.ignoredLabels` is absent, it SHALL default to `[pod, instance, endpoint, uid]`. An explicit list SHALL replace the default without merging.
17. An explicit empty list SHALL include all alert labels in the stable fingerprint.

### Skills Configuration

18. Each optional `tools.skills` entry SHALL provide paths within an OCI image. The adapter SHALL resolve the image from the entry's non-empty `image` first, otherwise from `AGENTIC_SKILLS_IMAGE` read at startup. Valid resolved entries SHALL map to `spec.tools.skills` on generated AgenticRuns. An explicit image SHALL override the environment default.
19. An absent `tools` key SHALL produce empty skills without an error.
20. An entry with neither an explicit image nor a non-empty `AGENTIC_SKILLS_IMAGE` SHALL be skipped with an Error-level log; this SHALL NOT fail config loading or prevent valid entries from being used.
21. An entry with a resolved image SHALL have empty strings removed from `paths`. If no paths remain, the entry SHALL be skipped with a warning.
22. A list containing valid and invalid entries SHALL retain the valid entries.

### Agent Selection

23. The adapter SHALL accept `agent.default`, `agent.analysis`, `agent.execution`, and `agent.verification` as agent names.
24. Each step SHALL use its non-empty step override, then the non-empty `agent.default`, then the name `default`.
25. Missing or empty agent fields SHALL use this fallback order. The configuration loader SHALL not check whether the named Agent resources exist.

### HTTPS Listener

26. `HTTP_PORT` SHALL select the HTTPS listener port. An unset or empty value SHALL use `8080`. A value that cannot be parsed as an integer from 1 through 65535 SHALL fail startup. The listener SHALL bind with an empty host on all available interfaces; there is no `HTTP_HOST` setting.
27. `TLS_CERT_FILE` and `TLS_KEY_FILE` SHALL both be non-empty. The process SHALL load their certificate/key pair before creating Kubernetes clients or starting the poller. Missing settings or a pair that cannot be loaded SHALL fail startup.
28. HTTPS SHALL require TLS 1.2 or newer, with no plaintext fallback. Certificates SHALL be loaded once at startup; mounted certificate updates SHALL require a process restart.

### Fixed HTTPS Limits

29. The HTTP server SHALL use the fixed header, read, write, idle, and shutdown settings below. These values are not configurable through YAML or environment variables.
30. After authentication and before JSON decoding, the handler SHALL read the body through a size-limited reader enforcing the fixed body limit below.
31. Route middleware SHALL derive one request deadline from the incoming context before authentication. All authentication and investigation operations SHALL inherit it, preserving caller cancellation and shorter incoming deadlines. This application deadline is separate from connection I/O timeouts.

| Setting | Value |
|---|---|
| `http.Server.MaxHeaderBytes` | 16 KiB |
| Request body limit | 4 KiB |
| Application request deadline | 20 seconds |
| Header read timeout | 5 seconds |
| Request read timeout | 10 seconds |
| Response write timeout | 30 seconds |
| Idle timeout | 60 seconds |
| Graceful HTTP shutdown deadline after poller exit | 5 seconds |

## Configuration Surface

| Field | Default | Description |
|---|---|---|
| `pollInterval` | `30s` | Interval between poll cycles |
| `preRunDelay` | `0s` | Polling-only minimum alert firing duration |
| `postRunDelay` | `1h` | Polling-only cooldown after a terminal run |
| `filtering.allowedReceivers` | `[]` | Polling receiver allowlist; empty skips every polled alert |
| `allowedReceivers` | (none) | Older form; the nested field takes precedence |
| `deduplication.ignoredLabels` | `[pod, instance, endpoint, uid]` | Labels excluded from the group fingerprint |
| `tools.skills[].image` | `AGENTIC_SKILLS_IMAGE` when omitted | OCI image containing skills; explicit values override the environment |
| `tools.skills[].paths` | (none) | Source paths within the skill image |
| `AGENTIC_SKILLS_IMAGE` | (none) | Optional process environment default for entries without an image; does not create skills entries by itself |
| `agent.default` | `default` through fallback | Shared agent name |
| `agent.analysis` | Shared fallback | Analysis agent override |
| `agent.execution` | Shared fallback | Execution agent override |
| `agent.verification` | Shared fallback | Verification agent override |
| `POD_NAMESPACE` | `openshift-lightspeed` | Namespace for runs and credential Secrets |
| `HTTP_PORT` | `8080` | HTTPS listener port on all available interfaces |
| `TLS_CERT_FILE` | Required | Server certificate path |
| `TLS_KEY_FILE` | Required | Server private-key path |

## Deployment Integration

The classic Lightspeed operator enables the local adapter through `OLSConfig.spec.ols.deployment.alertsAdapter.configMapRef`. It mounts that user-managed ConfigMap when present and restarts the pod on configuration changes. The hub mounts its own `hub-alerts-adapter-config` ConfigMap. The adapter process does not select a ConfigMap by name or read it through the Kubernetes API. Classic operator [PR #2133](https://github.com/openshift/lightspeed-operator/pull/2133) proposes injection of `AGENTIC_SKILLS_IMAGE` into the adapter deployment; until it is available, the deployment must provide the env var directly to exercise default-image resolution. The operator image must be supplied explicitly until an official skills image is pinned as a related image.

The direct deployment in `manifests/` mounts `alerts-adapter-config` and sets an illustrative `AGENTIC_SKILLS_IMAGE`. Its volume requires that ConfigMap to exist. The sample ConfigMap specifies an explicit image, which overrides the environment default; to test the default, omit the entry's `image`. Replace the illustrative image with a verified, pullable digest before production use. After changing the file or environment, restart the direct deployment to load the new settings. A process can use defaults when its configuration file is missing, but this does not make a required Kubernetes volume optional.

The direct deployment exposes an HTTPS ClusterIP Service on port `8080`. The Service annotation `service.beta.openshift.io/serving-cert-secret-name` requests `lightspeed-agentic-alerts-adapter-tls` from OpenShift Service CA. The Deployment mounts that Secret read-only at `/etc/tls/private`, with `TLS_CERT_FILE=/etc/tls/private/tls.crt` and `TLS_KEY_FILE=/etc/tls/private/tls.key`.

Clients must trust the Service CA and verify the Service DNS name. Certificate renewal requires a pod restart; no automatic restart or hot reload is implemented. Other deployment owners must provide equivalent TLS configuration.
