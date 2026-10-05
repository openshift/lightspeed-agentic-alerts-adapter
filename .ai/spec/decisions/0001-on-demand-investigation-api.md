# On-Demand Investigation API

**Status:** Proposed
**Date:** 2026-08-27

## Context

The adapter currently polls Alertmanager and creates `AgenticRun` resources. It has no inbound HTTP API. The UI needs an explicit action to request analysis for a firing alert, including alerts the poller would skip because of its receiver allowlist or delay settings.

The API must use Alertmanager as the source of truth for alert details. The UI supplies only the Alertmanager fingerprint; it does not supply alert labels, annotations, or timestamps.

## Proposed design

Add a small HTTP server to the existing adapter process. It uses the standard library HTTP server, the existing Alertmanager client, the existing `AgenticRun` client, and the existing alert-to-run builder. Add an in-cluster Kubernetes Service for the listener; UI routing and transport configuration are outside this design.

The API authenticates the request's bearer token by submitting it to the Kubernetes `TokenReview` API. The adapter's ServiceAccount needs `create` permission on `tokenreviews.authentication.k8s.io`. Do not log the bearer token. Token validation establishes identity; it does not decide whether that identity is authorized to trigger an investigation. Authorization remains unresolved and must be decided before the endpoint is exposed for production use.

The existing Kubernetes client writes runs with the adapter's ServiceAccount identity. Whether the API should keep that creator identity or create as the requesting user remains unresolved. The TokenReview response provides the authenticated user identity; how to use it beyond token validation also remains open.

## HTTP contract

- `POST /api/v1/investigations`
- Request body: `{"fingerprint":"<Alertmanager fingerprint>"}`
- On creation, return `201 Created` with `{"namespace":"<run namespace>","name":"<run name>"}`.
- Reject a request with a 4xx response if the cluster is suspended or the fingerprint does not match an eligible current alert. The exact error codes and response body are open.

Only the fingerprint is accepted from the caller. The adapter retrieves and uses the canonical alert returned by Alertmanager.

## Request flow

1. Validate the caller's bearer token with Kubernetes `TokenReview`.
2. Read the cluster-wide `AgenticOLSConfig` suspension state. If suspension is enabled, return 4xx without creating a run. If the suspension state cannot be read, fail closed and do not create a run.
3. Retrieve alerts from the local Alertmanager using the existing v2 client (`GET /api/v2/alerts`). Its query selects active alerts and excludes silenced and inhibited alerts.
4. Find an alert whose Alertmanager fingerprint exactly equals the request fingerprint. If none is returned, reject without creating a run.
5. Use the matched alert snapshot, including `startsAt`, labels, and annotations, with `BuildForTarget` and the adapter's existing configuration. Do not fetch Alertmanager a second time before creation. If the alert resolves after it was matched, still create the run; the analysis agent can determine that no action remains.
6. Apply group and episode deduplication, then create the `AgenticRun` in the adapter namespace.

## Selection and deduplication

The API intentionally bypasses the poller's receiver allowlist, pre-run delay, and post-run delay. It retains Alertmanager's active, non-silenced, non-inhibited selection and the adapter's stable group ID calculation.

- If any non-terminal run has the same group ID, do not create another run, regardless of episode.
- Episode identity comes from the deterministic run name, which includes the alert start time. If a run already exists for that episode, do not create another run, except when its prior run is `EmergencyStopped`.
- For `EmergencyStopped`, create a retry run using the existing deterministic `-retry-N` naming behavior, unless cluster suspension is enabled.
- A new alert episode in the same group may be created immediately after prior runs are terminal; the post-run cooldown does not apply to this API path.

The list/check/create decision must be serialized across API requests and poller work. The serialized section must use a fresh AgenticRun list so a stale poll-cycle snapshot cannot bypass group deduplication. This is required because deterministic names protect the same episode, but do not atomically enforce group-ID uniqueness for different episodes.

Whether the API path should use the poller's in-memory creation backoff after a failed Kubernetes create remains open.

## Run metadata

Keep `agentic.openshift.io/source=alertmanager` so existing Alertmanager-run listing and deduplication continue to work. Add a separate low-cardinality label, `agentic.openshift.io/trigger`, with value `api` or `poller`.

This distinguishes the creation path without changing the alert source. Existing runs created before this label is introduced will not have it; no backfill is proposed.

## Scope

- Initial implementation supports the local Alertmanager target only.
- Future spoke-cluster support remains open; this design does not add spoke routing.
- The design does not specify how the UI reaches the service or how external routing is configured.
- No new API operations beyond triggering an investigation are proposed.

## Open points

1. **Authorization:** Which authenticated users may trigger investigations, what scope applies, and how authorization is enforced. TokenReview alone is not authorization.
2. **Creator attribution:** Whether Kubernetes should record the adapter service account or the requesting user as the creator, and how to retain the validated user identity for audit if creation uses the service account.
3. **Spoke support:** Whether and how the API will support alerts from spoke clusters after the single-cluster implementation.
4. **Error contract:** Exact HTTP status codes and response bodies for authentication failures, suspension, malformed requests, unmatched fingerprints, duplicate group/episode requests, Alertmanager failures, suspension-read failures, and Kubernetes create failures. The 4xx class for suspension and unmatched fingerprints is agreed; exact codes are not.
5. **Creation backoff:** Whether an explicit user request should be subject to the poller's in-memory backoff after a create failure.

## Verification outline

Implementation should test TokenReview outcomes; exact fingerprint matching and missing alerts; the post-match resolve race; suspension; excluded receiver and delay settings; silenced/inhibited alert exclusion; active group deduplication; same-episode idempotency; `EmergencyStopped` retries; new episodes after terminal runs; serialized API/poller creation; and the trigger metadata label.
