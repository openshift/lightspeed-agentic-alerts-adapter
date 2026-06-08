## Context

The adapter polls AlertManager for firing alerts and creates Proposal CRs for automated remediation. Currently it uses the `/api/v2/alerts` endpoint which returns a flat list of individual alerts, and creates one Proposal per alert. AlertManager's `/api/v2/alerts/groups` endpoint returns alerts pre-grouped according to the user's `group_by` route configuration (typically `['alertname', 'namespace']`). Switching to this endpoint allows the adapter to create one Proposal per group of related alerts.

The go-swagger client already provides `c.api.Alertgroup.GetAlertGroups()` returning `models.AlertGroups` (`[]*AlertGroup`), where each `AlertGroup` contains `Labels` (the shared group labels), `Receiver`, and `Alerts` (`[]*GettableAlert` — the same type used today).

## Goals / Non-Goals

**Goals:**
- Replace the flat alerts endpoint with the groups endpoint.
- Create one Proposal per alert group, with a request template that presents all alerts in the group.
- Fall back to per-alert Proposal creation for groups with empty labels (cluster-scoped alerts).
- Replace fingerprint-based deduplication with a uniform group-hash scheme.
- Adapt severity filtering, initial delay, and dedup checks for group-level processing.
- Collect all distinct namespaces from alerts in the group into `spec.targetNamespaces`.

**Non-Goals:**
- Adapter-side re-grouping or sub-grouping of AlertManager groups.
- Max group size caps or other safeguards against broad grouping — the user is expected to configure `group_by` appropriately.
- Backward compatibility with existing Proposals (not yet deployed to production).
- Making the grouping behavior configurable or toggleable.

## Decisions

### 1. Use AlertManager's groups endpoint, trust user's `group_by` config

The adapter switches from `GetAlerts()` to `GetAlertGroups()` and uses the groups as-is. The assumption is that the user configures AlertManager's `group_by` setting appropriately (e.g., `['alertname', 'namespace']`). The adapter does not re-group or validate the grouping.

**Alternative considered:** Fetching flat alerts and grouping on the adapter side by configurable labels. Rejected because it duplicates AlertManager's existing grouping logic and ignores the user's routing configuration.

### 2. Empty group labels → per-alert fallback

When a group has empty labels (typically cluster-scoped alerts with no `namespace` label under the default OpenShift config of `group_by: ['namespace']`), each alert in that group is processed individually, producing one Proposal per alert. This prevents unrelated cluster-scoped alerts from being bundled into a single unfocused Proposal.

**Alternative considered:** Skipping empty-label groups entirely. Rejected because cluster-scoped alerts still need remediation.

### 3. Uniform group hash for dedup

All dedup matching uses a single label: `agentic.openshift.io/alert-group-hash`.

- **Grouped Proposals:** hash is `sha256(sorted "key=value" pairs from group labels)[:8]`.
- **Fallback per-alert Proposals:** hash is `fingerprint[:8]` (the alert's existing fingerprint, which is already a hash of the alert's label set).

This replaces the current `agentic.openshift.io/alert-fingerprint` label. One code path handles all dedup.

**Alternative considered:** Keeping both `alert-fingerprint` and `alert-group-hash` labels for a transition period. Rejected because the system is not yet in production.

### 4. Severity filtering: per-alert within group

Alerts with severity `none` or `info` are filtered out from the group before Proposal creation. If all alerts in a group are filtered out, the entire group is skipped. The Proposal is built from the remaining alerts only.

### 5. Initial delay: any alert in group passes

A group passes the initial delay check if any alert in the group has been firing longer than the threshold. Rationale: the group exists because AlertManager considers the alerts related, and if at least one alert has persisted, the situation warrants a Proposal.

### 6. Target namespaces from all alerts in the group

The Proposal's `spec.targetNamespaces` is populated with all distinct namespace values collected from the individual alerts' `namespace` labels. Currently the field is set from a single alert; with grouping, alerts in the same group may span multiple namespaces (depending on the `group_by` configuration). If no alert in the group has a `namespace` label, `targetNamespaces` is omitted.

### 7. Proposal metadata

**Labels:**
- `agentic.openshift.io/source: alertmanager`
- `agentic.openshift.io/alert-group-hash: <hash>` — dedup key
- `agentic.openshift.io/alert-severity: <highest>` — highest severity across all alerts in the group

**Annotations:**
- `agentic.openshift.io/alert-names: '["AlertA","AlertB"]'` — JSON array of distinct alert names in the group
- `agentic.openshift.io/alert-starts-at: <earliest>` — earliest `startsAt` across all alerts in the group
- `agentic.openshift.io/alert-summary: <summary>` — summary from the first alert (or combined)

The `alert-names` annotation uses a JSON array format since annotation values have no length or character restrictions, unlike labels.

### 8. Proposal naming

- **Grouped:** `{first-alertname}-{namespace}-{hash}` or `{first-alertname}-{hash}` if no namespace in group labels. Uses the first alert name from sorted distinct names for readability.
- **Fallback:** Same as current: `{alertname}-{namespace}-{fingerprint[:8]}` or `{alertname}-{fingerprint[:8]}`.

### 9. Request template

The template changes from describing a single alert to listing all alerts in the group, prefixed with the group labels. Each alert's full details (severity, runbook URL, namespace, summary, description, labels) are included so the analysis agent has the complete symptom picture.

## Risks / Trade-offs

- **[Risk] User has not configured `group_by` properly** — With the default OpenShift config (`group_by: ['namespace']`), unrelated alert types in the same namespace are grouped together. The adapter trusts the user's configuration. This should be documented as a prerequisite.
- **[Trade-off] No adapter-side grouping control** — Simpler implementation, but the adapter has no way to override AlertManager's grouping. Acceptable because the grouping is an AlertManager concern.
- **[Risk] Large groups produce long request templates** — If a group contains many alerts, the rendered request could be very long. No cap is applied; the agent is expected to handle it.
