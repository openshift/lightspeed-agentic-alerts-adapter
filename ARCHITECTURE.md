# lightspeed-agentic-alerts-adapter

## Overview

The **lightspeed-agentic-alerts-adapter** is a standalone component that bridges OpenShift cluster alerts into the Lightspeed Agentic system. It polls the in-cluster AlertManager API for firing alerts and creates `AgenticRun` CRs (`agentic.openshift.io/v1alpha1`) to trigger automated analysis, remediation, and verification workflows.

The adapter is a stateless, single-purpose binary written in Go 1.26. It runs as a Deployment in the same namespace as the Lightspeed Agentic operator (`openshift-lightspeed`).

## Requirements

### Functional

1. **Poll AlertManager** for currently firing alerts at a configurable interval.
2. **Create AgenticRuns** for firing alerts that pass deduplication checks.
3. **Deduplicate** to avoid creating multiple AgenticRuns for the same alert or for intermittent/flapping alerts.
4. **Map alert data** into AgenticRun spec fields using a structured request template.
5. **Handle restarts gracefully** — no missed alerts, no duplicates after restart.

### Non-functional

1. **Stateless** — no persistent storage, no in-memory state required for correctness.
2. **Idempotent** — safe to restart at any time; safe under concurrent execution.
3. **Create-only** — the adapter creates AgenticRuns but never modifies or deletes them. The operator owns the AgenticRun lifecycle.
4. **Observable** — structured logging and health/readiness probes. Prometheus metrics deferred to a future iteration.
5. **Container-ready** — include a `Containerfile` to build the adapter image and deploy it in an OpenShift cluster.

## Architecture

### Component Placement

```
                          openshift-monitoring
                         ┌──────────────────────────────┐
  Prometheus/Thanos      │                              │
  evaluates rules ──────►│  AlertManager                │
                         │  (grouping, silencing,       │
                         │   inhibition, dedup)         │
                         └──────────┬───────────────────┘
                                    │
                      GET /api/v2/alerts (poll every 30s)
                                    │
                          openshift-lightspeed
                         ┌──────────┼───────────────────┐
                         │          ▼                   │
                         │  alerts-adapter              │
                         │  (diff firing vs existing)   │
                         │          │                   │
                         │     CREATE AgenticRun CR       │
                         │          │                   │
                         │          ▼                   │
                         │  Lightspeed Agentic Operator │
                         │  (reconcile → agents)        │
                         └──────────────────────────────┘
```

### Why AlertManager (not Thanos Ruler)

AlertManager is the alert **notification router** in the OpenShift monitoring stack. It sits downstream of Prometheus/Thanos Ruler. It provides a stable API (`GET /api/v2/alerts`) that returns all alerts with their metadata (labels, annotations, status, timestamps). AlertManager also offers features like grouping, silencing, and inhibition — the adapter does not leverage these in the initial implementation, but they provide a path for future refinement (see [Future Work](#future-work)).

Thanos Ruler evaluates alerting rules and forwards firing alerts *to* AlertManager. Integrating at the Thanos Ruler level would require reimplementing AlertManager's notification logic.

### Why Polling (not Webhooks)

The adapter polls AlertManager's `GET /api/v2/alerts` endpoint rather than receiving webhooks. This choice is driven by **resilience to downtime**:

- **Webhook risk**: If the adapter is down when AlertManager delivers a webhook, the notification is lost. AlertManager retries with backoff but has a finite retry window. Recovery depends on `repeat_interval` (typically 1–4 hours).
- **Polling resilience**: When the adapter restarts, the next poll immediately sees all currently firing alerts. Zero missed alerts, zero catch-up delay.

The polling approach also requires no AlertManager configuration changes (no `AlertmanagerConfig` CR, no receiver setup).

### Stateless Design

The adapter maintains **no internal state**. On every poll cycle, it computes a fresh diff between two authoritative sources:

1. **AlertManager API** — what's currently firing.
2. **Kubernetes API** — what AgenticRuns already exist (filtered by labels).

This means restarts, upgrades, and pod rescheduling are inherently safe. The adapter rebuilds its understanding of the world on every poll cycle.

## Design

### Alert-to-AgenticRun Cardinality

The adapter maintains a **1:1 relationship** between alert occurrences and AgenticRuns. Each firing alert occurrence (identified by its AlertManager fingerprint and `startsAt` timestamp) maps to exactly one AgenticRun. There is no alert grouping — if 10 alerts are firing, 10 AgenticRuns are created (subject to deduplication checks). If the same alert resolves and fires again (new `startsAt`), it produces a new AgenticRun with a distinct name.

### Poll Loop

The adapter runs a single loop:

```
every <pollInterval> (default 30s):
    1. GET /api/v2/alerts?active=true&silenced=false&inhibited=false → firing alerts
    2. LIST AgenticRuns (label: source=alertmanager) → existing AgenticRuns
    3. For each firing alert:
        a. Receivers not in allowedReceivers?               → skip (not routed to allowed receiver)
        b. Severity is "none" or "info"?                    → skip (low severity)
        c. now - alert.startsAt < preRunDelay?              → skip (too transient)
        d. Active AgenticRun with same stable fingerprint?    → skip (already handling)
        e. Terminal AgenticRun within postRunDelay?          → skip (too soon to retry)
        f. Else → CREATE AgenticRun
```

**Poll interval**: 30 seconds by default, configurable via ConfigMap. The poll interval is fixed for the lifetime of the process; changes require a pod restart (triggered by the operator). The pre-run delay (when configured) dominates response latency, so the poll interval doesn't need to be aggressive.

The query parameters ensure the adapter only processes alerts that are actively firing and not suppressed by AlertManager's silencing or inhibition rules.

### Deduplication

Two configurable parameters control deduplication. Both are configurable via the `alerts-adapter-config` ConfigMap, with the defaults shown below.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `PreRunDelay` | Minimum time an alert must be firing before the adapter creates an AgenticRun. Filters transient alerts that resolve on their own. Uses the alert's `startsAt` field from AlertManager — no in-memory tracking needed. | 0 |
| `PostRunDelay` | After an AgenticRun reaches a terminal phase (Completed, Failed, Escalated, Denied), minimum time before the adapter creates a new AgenticRun for the same alert (matched by fingerprint label). Avoids repeated analysis of an alert that has already been investigated. Uses the terminal AgenticRun's condition timestamps. | 1 hour |

### Race Condition Prevention

The adapter uses **deterministic AgenticRun naming** derived from alert metadata. The name includes an 8-character SHA-256 hash of the alert's `startsAt` timestamp (RFC 3339 UTC), ensuring each alert occurrence gets a unique AgenticRun name.

```
{alertname}-{namespace}-{startsAtHash}
```

Examples:
- `kubepodcrashlooping-production-895c8977`
- `etcdhighfsyncdurations-a3f1b2c4` (no namespace for cluster-scoped alerts)

Components are sanitized to conform to DNS subdomain rules (RFC 1123): lowercased, non-alphanumeric characters replaced, truncated to fit the 63-character limit.

Deduplication uses the `alert-group-id` label (stable FNV-64a hash of labels minus configurable ignored labels, truncated to 8 hex characters) to match alerts to existing AgenticRuns. The original AlertManager fingerprint is stored separately in the `alert-fingerprint` label for UI lookups. Neither fingerprint is part of the AgenticRun name — they are only stored as labels for matching.

### Alert to AgenticRun Mapping

#### AgenticRun Name

Deterministic: `{alertname}-{namespace}-{startsAtHash}` (see above).

#### Namespace

AgenticRuns are created in the alert's source namespace — i.e., the namespace from the alert's `namespace` label. For cluster-scoped alerts with no namespace label, AgenticRuns are created in the operator namespace (`openshift-lightspeed`) as a fallback. The operator controller watches AgenticRuns across all namespaces, so this works without additional configuration. The adapter's ServiceAccount needs AgenticRun create/list/get RBAC across namespaces (ClusterRole instead of a namespace-scoped Role).

#### spec.targetNamespaces

Set to the alert's `namespace` label (matching the namespace where the AgenticRun is created). If the alert has no namespace label (cluster-scoped alerts), `targetNamespaces` is left empty. The operator grants cluster-scoped RBAC based on the analysis agent's output.

#### spec.request

Built from a Go `text/template` that combines English instructions with structured alert data:

```go
const requestTemplate = `
A Kubernetes alert is firing in the cluster.
Investigate the root cause and propose a remediation.

Alert: {{ .AlertName }}
Severity: {{ .Severity }}
Namespace: {{ .Namespace }}
Summary: {{ .Summary }}
Description: {{ .Description }}

Labels:
{{ range $k, $v := .Labels }}  {{ $k }}: {{ $v }}
{{ end }}
`
```

The template input is populated from the AlertManager alert payload:
- `AlertName`: `alert.labels["alertname"]`
- `Severity`: `alert.labels["severity"]`
- `Namespace`: `alert.labels["namespace"]` (may be empty)
- `Summary`: `alert.annotations["summary"]`
- `Description`: `alert.annotations["description"]`
- `Labels`: all `alert.labels`

#### spec.analysis / execution / verification

All three steps configured with the `default` agent:

```yaml
spec:
  analysis:
    agent: default
  execution:
    agent: default
  verification:
    agent: default
```

The `default` Agent CR is expected to exist in the cluster, configured by the operator installation.

#### spec.analysisOutput

Default mode, no adapter-specific schema:

```yaml
spec:
  analysisOutput:
    mode: Default
```

#### Labels and Annotations

```yaml
metadata:
  labels:
    agentic.openshift.io/source: alertmanager
    agentic.openshift.io/alert-fingerprint: <AlertManager fingerprint[:8]>
    agentic.openshift.io/alert-group-id: <stable fingerprint[:8]>
    agentic.openshift.io/alert-name: <alertname, lowercased>
    agentic.openshift.io/alert-severity: <severity>
  annotations:
    agentic.openshift.io/alert-starts-at: "<RFC3339 timestamp>"
    agentic.openshift.io/alert-summary: "<summary annotation, truncated>"
```

Labels are used for filtering and dedup queries. Annotations carry non-selectable metadata for UI and debugging.

### Alert Resolution Behavior

When an alert resolves while its AgenticRun is still active (Analyzing, Executing, Verifying), the adapter does nothing. The AgenticRun continues to completion. Rationale:

- A self-resolved alert doesn't mean the underlying issue is fixed.
- The analysis and remediation may still be valuable.
- Keeping the adapter create-only simplifies the design and avoids lifecycle coupling with the operator.

### AlertManager Authentication

The adapter authenticates to the AlertManager API using the pod's auto-mounted ServiceAccount token:

- **Endpoint**: `https://alertmanager-main.openshift-monitoring.svc:9094/api/v2/alerts`
- **Authentication**: `Authorization: Bearer <ServiceAccount token>`
- **TLS**: Verified against the cluster CA bundle (`/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt`)

The AlertManager URL is defined as a constant, with a path to make it configurable.

### Logging

The adapter uses Go's standard library `log/slog` package with JSON output. Log levels:

| Level | What gets logged |
|-------|-----------------|
| `Info` | Poll cycle start/end, AgenticRun created (with alert name and namespace), adapter startup/shutdown |
| `Error` | AlertManager unreachable, Kubernetes API errors, AgenticRun creation failures (non-409) |
| `Debug` | Alerts skipped due to pre-run delay, existing AgenticRun, or post-run delay (including the skip reason and alert fingerprint) |

### Error Handling

- **AlertManager unreachable**: Log the error, skip the poll cycle, retry on the next interval.
- **Kubernetes API unreachable**: Log the error, skip AgenticRun creation, retry on the next interval.
- **AgenticRun creation fails (non-409)**: Log the error with alert details. The alert will be retried on the next poll cycle since no AgenticRun exists for it.
- **Invalid alert data** (missing alertname, template rendering failure): Log and skip the individual alert. Do not block processing of other alerts.

## Deployment

### Kubernetes Resources

**Deployment** — single replica in `openshift-lightspeed`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: lightspeed-agentic-alerts-adapter
  namespace: openshift-lightspeed
spec:
  replicas: 1
  selector:
    matchLabels:
      app: lightspeed-agentic-alerts-adapter
  template:
    metadata:
      labels:
        app: lightspeed-agentic-alerts-adapter
    spec:
      serviceAccountName: lightspeed-agentic-alerts-adapter
      volumes:
        - name: config
          configMap:
            name: alerts-adapter-config
      containers:
        - name: adapter
          image: quay.io/openshift-lightspeed/lightspeed-agentic-alerts-adapter:latest
          volumeMounts:
            - name: config
              mountPath: /etc/alerts-adapter
              readOnly: true
          env:
            - name: ALERTMANAGER_URL
              value: https://alertmanager-main.openshift-monitoring.svc:9094
```

Single replica is sufficient because:
- Stateless design handles restarts gracefully.
- Polling catches up immediately after downtime.
- Deterministic naming prevents duplicates even under concurrent execution.

### RBAC

The adapter's ServiceAccount needs two sets of permissions:

**1. AlertManager access** — read alerts from `openshift-monitoring`:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: lightspeed-agentic-alerts-adapter-alertmanager
  namespace: openshift-monitoring
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: monitoring-alertmanager-view
subjects:
  - kind: ServiceAccount
    name: lightspeed-agentic-alerts-adapter
    namespace: openshift-lightspeed
```

**2. AgenticRun management** — create and list AgenticRuns across namespaces:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: lightspeed-agentic-alerts-adapter-agenticruns
rules:
  - apiGroups: ["agentic.openshift.io"]
    resources: ["agenticruns"]
    verbs: ["create", "list", "get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: lightspeed-agentic-alerts-adapter-agenticruns
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: lightspeed-agentic-alerts-adapter-agenticruns
subjects:
  - kind: ServiceAccount
    name: lightspeed-agentic-alerts-adapter
    namespace: openshift-lightspeed
```

### Dependencies

| Dependency | Purpose |
|-----------|---------|
| `github.com/openshift/lightspeed-agentic-operator/api` | Typed AgenticRun CRD Go types |
| `k8s.io/client-go` | In-cluster config, ServiceAccount auth |

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ALERTMANAGER_URL` | `https://alertmanager-main.openshift-monitoring.svc:9094` | AlertManager API endpoint |
| `POD_NAMESPACE` | `openshift-lightspeed` | Adapter's namespace (set via downward API in the deployment manifest) |

### ConfigMap

The `alerts-adapter-config` ConfigMap is mounted as a volume at `/etc/alerts-adapter/` and read once at startup from the `config.yaml` key. If the file is missing or malformed, defaults are used. The operator watches the ConfigMap and restarts the adapter pod when the config changes.

| Field | Default | Description |
|-------|---------|-------------|
| `pollInterval` | `30s` | How often to poll AlertManager |
| `preRunDelay` | `0s` | Alert must fire this long before creating an AgenticRun |
| `postRunDelay` | `1h` | Minimum time after a terminal AgenticRun before re-proposing for the same alert |
| `filtering.allowedReceivers` | `[]` | Receiver allowlist — only alerts routed to at least one of these receivers are processed (case-insensitive). Empty by default; no AgenticRuns are created until receivers are explicitly configured |
| `deduplication.ignoredLabels` | `[pod, instance, endpoint, uid]` | Labels stripped before computing the stable fingerprint for dedup matching. When set, fully replaces the defaults. Set to `[]` to include all labels |

Tools/skills configuration is also supported — see [README.md](README.md#configuration) for the full ConfigMap example including shared and per-step skills.

### Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `DefaultNamespace` | `openshift-lightspeed` | Namespace for AgenticRuns from cluster-scoped alerts (no namespace label) |
| `DefaultAgent` | `default` | Agent name for analysis, execution, and verification steps |

## Future Work

- **AlertManager-aware filtering**: Leverage AlertManager's grouping features to reduce noise. The adapter already filters out silenced and inhibited alerts via query parameters, but does not use grouping metadata. Future iterations could create a single AgenticRun per alert group instead of per individual alert.
- **Custom fingerprinting and alert grouping**: Consider replacing AlertManager's fingerprint (hash of all labels) with a custom fingerprint computed from a chosen subset of labels. This could potentially enable custom alert grouping to handle alert storms — e.g., multiple related alerts might map to a single AgenticRun instead of creating one per alert. A custom fingerprint would also decouple the adapter from AlertManager's fingerprint format, making it easier to switch to other alert sources (e.g., Thanos Ruler) in the future. To be evaluated based on real-world usage patterns.
- **Per-alert-group configuration**: Allow a configuration resource (ConfigMap or CRD) to define customized settings per alert group — including pre-run delay, post-run delay, workflow pattern (full remediation / advisory / assisted), and prompt template. This would enable different handling strategies for different classes of alerts (e.g., shorter delay for critical infrastructure alerts, advisory-only for capacity warnings).
- **Label-selector alert filtering**: The adapter currently filters by receiver allowlist and severity. A future iteration could add configurable label selectors for more fine-grained alert filtering.
- **Prometheus metrics**: `alerts_adapter_polls_total`, `agenticruns_created_total`, `errors_total`, `poll_duration_seconds`.
- **Adapter-specific analysis output schema**: Inject custom fields into `analysisOutput.schema` for alert correlation, affected services topology.
- **Workflow selection**: Choose different workflow patterns (advisory, assisted, full remediation) based on alert labels.
- **Multi-replica support**: Leader election or sharded alert processing for high availability. With deterministic AgenticRun naming (based on alert identity and startsAt), concurrent replicas attempting to create the same AgenticRun would result in one succeeding and the other receiving `409 Conflict (AlreadyExists)` — the adapter already treats 409 as success, so basic multi-replica operation works without coordination, though leader election would reduce redundant API calls.
- **Token budgets**: Protect against alert storms hitting model rate limits. At minimum add jitter to AgenticRun creation; consider an adapter-level or OLS-level token budget to prevent runaway costs.
- **Retry clarity for unparseable alerts**: When an alert cannot be parsed, the adapter skips it but will keep retrying on each subsequent poll until the alert disappears. Consider explicit retry-on-next-interval semantics with backoff or a skip list.
