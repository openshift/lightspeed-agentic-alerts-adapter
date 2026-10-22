# System Overview

The alerts adapter polls OpenShift AlertManager and accepts authenticated HTTPS requests for local on-demand investigations. Both paths create `AgenticRun` resources to start analysis and remediation through the Lightspeed Agentic operator. It runs as one replica, only creates runs, and has no persistent storage. It keeps target information and polling creation backoff in memory.

## Behavioral Rules

### System Role

1. The adapter SHALL poll AlertManager at a configurable interval (default 30 seconds), fetch active alerts, and create runs that pass filtering and deduplication.
1a. The adapter SHALL expose `POST /api/v1/investigations` over HTTPS for on-demand investigation of local AlertManager alerts.
1b. API requests SHALL authenticate through TokenReview and authorize through caller-authenticated SelfSubjectAccessReview before body parsing or internal reads. Final creation SHALL use a request-scoped caller-authenticated client, with no ServiceAccount fallback.
2. The adapter SHALL run as a single-replica deployment. The default namespace SHALL be `openshift-lightspeed`.
3. The adapter SHALL run an immediate poll on startup so it can see alerts still firing after a restart. Alerts that resolve during downtime may not be seen.
4. All AgenticRuns SHALL be created in the adapter namespace selected by `POD_NAMESPACE`.
5. Kubernetes AlreadyExists on creation SHALL be a no-op for polling and SHALL return `409 investigation_in_progress` for the API. The adapter SHALL not update or delete existing runs, including when alerts resolve.

### Component Responsibilities

6. Alert retrieval SHALL request active, non-silenced, non-inhibited alerts. Local requests SHALL read the ServiceAccount token on each call and use the service CA for TLS.
7. Run construction SHALL produce deterministic names, alert metadata, and an investigation request using the rules in [AgenticRun building](agenticrun-building.md).
8. Polling filtering and deduplication SHALL apply receiver selection, pre-run delay, active-run checks, cooldown, and creation backoff in that order. Several alerts in one stable group can share an active run.
9. Configuration SHALL be loaded once at startup from `/etc/alerts-adapter/config.yaml`. File changes SHALL require a process restart.
10. The suspension check SHALL read the `AgenticOLSConfig` singleton named `cluster` before each cycle's alert and run operations, and before each authorized, valid API request's alert and run operations.
11. Multicluster discovery SHALL list and watch `SpokeCluster` resources and update the set of targets used by later cycles.

### Integration Points

12. The adapter SHALL create `agentic.openshift.io/v1alpha1` `AgenticRun` resources. The agentic operator owns their workflow and cleanup after creation.
13. Multicluster mode SHALL discover cluster-scoped `hub.openshift.io/v1alpha1` `SpokeCluster` resources.
14. Alert retrieval SHALL use the AlertManager v2 alerts API.

### Lifecycle

15. SIGTERM or SIGINT SHALL cancel or complete in-flight work and stop the process cleanly.
15a. The HTTPS server SHALL run alongside the poller. Listener startup failures SHALL fail startup; serving failures SHALL cancel the shared process context. After the poller exits, HTTP shutdown SHALL have a five-second deadline, followed by forced close if graceful shutdown fails.
16. The adapter SHALL write structured JSON logs for startup, shutdown, target failures, and creation results.
16a. Deduplication SHALL read Kubernetes state each poll cycle and each API investigation attempt. Polling creation backoff SHALL be kept in memory and cleared on restart; API requests SHALL not use it.

## Configuration Surface

File fields and defaults are defined in [configuration](configuration.md#configuration-surface).

| Startup setting | Default | Description |
|---|---|---|
| `ALERTMANAGER_URL` | `https://alertmanager-main.openshift-monitoring.svc:9094` | Local endpoint; an explicitly empty value disables the local target |
| `POD_NAMESPACE` | `openshift-lightspeed` | Namespace for runs and spoke credential Secrets |
| `--multicluster` | `false` | Enable spoke discovery alongside any local target |
| `MULTICLUSTER_MAX_CONCURRENT_TARGETS` | `4` | Positive target concurrency limit in multicluster mode; ignored in single-cluster mode |
| `HTTP_PORT` | `8080` | HTTPS listener port on all available interfaces |
| `TLS_CERT_FILE` | Required | Server certificate path, loaded at startup |
| `TLS_KEY_FILE` | Required | Server private-key path, loaded at startup |

## Deployment Ownership

The classic Lightspeed operator deploys the local adapter when `OLSConfig.spec.ols.deployment.alertsAdapter.configMapRef` is set. The hub operator deploys a separate multicluster adapter with local polling disabled. This repo also provides manifests for direct deployment. See the parent specs `ols/.ai/spec/what/deployment-lifecycle.md` (deployment lifecycle) and `ols/.ai/spec/what/alerts-adapter-multicluster.md` (multicluster contract).

## Constraints

- Terminal phases are Completed, Failed, Denied, Escalated, and EmergencyStopped. `NoActionRequired` is a reason on the Analyzed condition that derives as Completed.
- The desired name limit is 63 characters because the operator uses run names as label values. The long-namespace gap is recorded in [building rule 16a](agenticrun-building.md#metadata-sanitization).
- Cooldown for analysis-only completion remains a [planned correction](poll-loop.md#post-run-delay-cooldown).
- An empty receiver allowlist skips all polled alerts, not manual API requests. There is no separate severity filter.
- The API supports only the local target; when that target is disabled, requests return a generic `500 internal_error`.
- HTTPS is mandatory. Missing TLS settings or an unloadable certificate/key pair fail startup. Certificates are not hot-reloaded; renewal requires a process restart.
- There is no persistent database, HTTP health endpoint, or Prometheus metrics endpoint in the current adapter.
