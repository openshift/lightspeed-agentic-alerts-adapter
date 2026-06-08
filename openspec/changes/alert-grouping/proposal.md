## Why

The adapter currently creates one Proposal CR per individual alert. When multiple instances of the same alert fire simultaneously (e.g., 5 pods crash-looping in the same namespace), this produces 5 independent Proposals that each attempt to remediate the same root cause. This creates noise, wastes agentic operator resources, and prevents the agent from seeing the full symptom picture.

AlertManager already groups related alerts based on the user's `group_by` route configuration. The adapter should leverage this grouping to create one Proposal per alert group, giving the agentic operator a holistic view of related symptoms for better root-cause analysis and remediation.

## What Changes

- Switch from AlertManager's `/api/v2/alerts` (flat list) to `/api/v2/alerts/groups` (pre-grouped) endpoint.
- Create one Proposal per alert group instead of one per alert.
- For groups with empty labels (cluster-scoped alerts that AlertManager cannot meaningfully group), fall back to per-alert Proposal creation.
- Replace fingerprint-based deduplication with a uniform group-hash-based scheme.
- Update the request template to present all alerts in a group to the analysis agent.
- Update Proposal labels and annotations to reflect group-level metadata.

## Capabilities

### Modified Capabilities
- `alert-retrieval`: Retrieve alert groups instead of individual alerts from AlertManager.
- `poll-loop`: Reconcile loop iterates over alert groups. Dedup, initial delay, and severity filtering adapted for groups. Empty-label groups fall back to per-alert processing.
- `proposal-building`: Build Proposals from alert groups. New naming, labeling, and request template for grouped alerts.

## Impact

- `internal/alertmanager/client.go` — switch from `GetAlerts` to `GetAlertGroups` API call; return type changes to `models.AlertGroups`.
- `internal/adapter/adapter.go` — `AlertSource` interface changes return type; `reconcile` iterates groups, applies severity filtering per-alert within groups, initial delay passes if any alert qualifies, dedup by `alert-group-hash` label.
- `internal/proposal/build.go` — new `BuildFromGroup` function alongside updated `Build` for single-alert fallback; group hash computation; new label/annotation scheme.
- `internal/proposal/request.tmpl` — multi-alert template listing all alerts in the group with their details.
- `internal/adapter/adapter_test.go` — tests updated for group-based flow.
- `internal/proposal/build_test.go` — tests for group building, hash computation, fallback path.
