# On-Demand Investigation API

Creates a local alert investigation on behalf of an authenticated Kubernetes caller. Design rationale lives in [decision 0001](../decisions/0001-on-demand-investigation-api.md). Listener configuration and numerical limits live in [configuration](configuration.md#https-listener).

## Behavioral Rules

### HTTP Interface

1. The adapter SHALL expose `POST /api/v1/investigations` over HTTPS.
2. The request SHALL contain one JSON object with a non-empty string `fingerprint`. The handler SHALL reject malformed JSON, unknown fields, missing/empty/non-string fingerprints, and non-whitespace trailing data.
3. The caller SHALL NOT supply alert labels, annotations, timestamps, target selection, or requester identity.
4. Authentication and authorization SHALL run before body parsing and all internal reads. An unauthenticated request SHALL therefore return `401` even when its body is invalid.
5. Bearer authentication SHALL use exactly one Authorization header. Missing, malformed, or duplicate headers SHALL return `401 unauthorized`; the Bearer scheme SHALL be case-insensitive.

### Authentication and Authorization

6. TokenReview SHALL use the adapter's ServiceAccount, with `spec.audiences` unset to validate against the Kubernetes API-server default audience. Call failures SHALL return generic `500 internal_error`, including failures of the adapter's own credentials.
7. A completed TokenReview with `status.authenticated=false` SHALL return `401 unauthorized` without an error log, regardless of `status.error`. The adapter SHALL NOT proceed to authorization or internal reads.
8. A completed TokenReview with `status.authenticated=true` and non-empty `status.error` SHALL return generic `500 internal_error`. Only authenticated results with an empty error SHALL proceed.
9. Authorization SHALL submit a SelfSubjectAccessReview using the caller's token, with resource attributes `group=agentic.openshift.io`, `resource=agenticruns`, `verb=create`, and the adapter namespace.
10. A non-empty review evaluation error SHALL return `500`, even when allowed is true. Without an evaluation error, allowed=false SHALL return `403 forbidden`. Caller-authenticated review request failures SHALL map `401`/`403` to caller errors and all other failures to generic `500`.
11. Caller clients SHALL start from `rest.AnonymousClientConfig`, retain only trusted API-server connection settings with verified TLS, and authenticate only with the request token. ServiceAccount token files, client credentials, auth/exec providers, impersonation, and credential-bearing transports SHALL NOT be retained. Schema and REST mapping SHALL be reused independently of caller authentication.
12. Caller clients SHALL be request-scoped, never cached across requests or substituted with ServiceAccount clients after failure. Tokens SHALL NOT be persisted, logged, or forwarded to AlertManager.
13. The middleware SHALL place the authorized AgenticRun creator in the request context under a private key. Missing creators SHALL fail closed; the handler SHALL NOT create its own ServiceAccount-backed creator.

### Investigation Flow

14. After authorization and body validation, the service SHALL check cluster suspension before alert retrieval or run listing. Missing suspension CRD/singleton SHALL mean not suspended; other read failures SHALL return generic `500`. Enabled suspension SHALL return `403 investigations_suspended`.
15. The service SHALL support only the fixed local target. After the suspension check passes, an absent or invalid local target SHALL return generic `500`; disabling local polling SHALL NOT enable spoke routing for API requests.
16. The service SHALL retrieve active, non-silenced, non-inhibited alerts using the existing local AlertManager client and its own credentials. Fingerprints SHALL match exactly; no matching alert SHALL return `404 alert_not_found`.
17. The service SHALL call `BuildForTarget` using the matched canonical alert snapshot, adapter namespace, configured ignored labels, tools, and agents. It SHALL NOT refetch the alert before creation. An alert resolving after matching SHALL NOT cancel the selected investigation.
18. API investigations SHALL bypass polling receiver filters, pre-run/post-run delays, and creation backoff, including after prior polling creation failures. API requests SHALL NOT read or mutate the poller's backoff.
19. Suspension reads, AlertManager retrieval, and run listing SHALL use internal clients. Run listing SHALL retain the existing source and local-target scoping rules.
20. Final creation SHALL use only the request-scoped caller creator in the adapter namespace. Kubernetes SHALL enforce the caller's current authorization and admission policy at creation time.
21. Prior failures, cancellation, or deadline expiry SHALL prevent subsequent internal operations and creation. All operations SHALL receive the request context.

### Episode Matching and Naming

22. An episode SHALL be identified by the original deterministic base name from the builder and the stable group ID. Different original bases in the same group SHALL NOT block each other.
23. Matching SHALL search both original and group-qualified naming families and their retries, always requiring the same group ID, even if the original conflicting run has been deleted.
24. Hash-preserving retries and legacy whole-base-truncated retries SHALL be recognized. When legacy truncation removed any hash characters, the existing start-time annotation SHALL also equal the canonical alert's UTC RFC 3339 start time. Name and group alone SHALL NOT attribute an ambiguous legacy retry.
25. Retry eligibility SHALL consider every matching attempt at the maximum creation timestamp. Any non-terminal attempt at that timestamp SHALL return `409 investigation_in_progress`; earlier attempts SHALL NOT affect eligibility. List order and retry numbering SHALL NOT break timestamp ties.
26. Completed, Failed, Denied, Escalated, and EmergencyStopped SHALL be terminal. Analysis with reason NoActionRequired derives as Completed and SHALL permit a retry when all latest attempts are terminal.
27. With no matching attempt, an unused original base SHALL be selected. If another group occupies it, the service SHALL select `g-<group-ID>-<prefix>-<hash>`, preserving the full group ID and eight-character episode hash and shortening only the readable prefix to fit 63 characters.
28. Existing attempts SHALL retain their naming family. If matching attempts exist in both families, eligibility SHALL consider both and naming SHALL prefer the original family.
29. The selected unsuffixed base and all returned run names SHALL be passed to `NextAvailableName`. An unused base SHALL remain unchanged; otherwise the first free hash-preserving `-retry-N` candidate SHALL be selected. Retry suffixes SHALL NOT be nested.
30. Kubernetes name uniqueness SHALL arbitrate concurrent creation without a shared API/poller lock. AlreadyExists, including `created=false, err=nil`, SHALL return `409`; only `created=true, err=nil` SHALL produce `201`.

### Responses and Metadata

31. Successful creation SHALL return `201 Created` with only `namespace` and `name` as JSON strings.
32. Handler-generated errors SHALL use the JSON structure and fixed values below. All handler-generated JSON responses SHALL set `Content-Type: application/json`; `401` responses SHALL also set `WWW-Authenticate: Bearer`.
33. Dependency failures SHALL NOT expose diagnostic text, upstream response bodies, identifiers, or bearer tokens. Caller creation `401`/`403` SHALL map to caller errors; other creation failures SHALL return generic `500`, except AlreadyExists.
34. Request/upstream deadline expiry SHALL return generic `500` when a response remains possible. Body-read timeouts SHALL return `408`. A `500` during creation SHALL NOT imply that no run was created.
35. New runs SHALL retain `agentic.openshift.io/source=alertmanager` and add `agentic.openshift.io/trigger=api`; polling runs SHALL use `trigger=poller`. Existing resources SHALL NOT be renamed or backfilled.

## Response Shapes

Success:

```json
{"namespace":"openshift-lightspeed","name":"highcpu-abcdef12"}
```

Error:

```json
{"error":{"code":"internal_error","message":"An internal error occurred"}}
```

| HTTP | Code | Message |
|---|---|---|
| 400 | `invalid_request` | Request must contain only a non-empty fingerprint string |
| 401 | `unauthorized` | A valid bearer token is required |
| 403 | `forbidden` | You are not allowed to trigger investigations |
| 403 | `investigations_suspended` | Investigations are suspended |
| 404 | `alert_not_found` | No eligible alert matches the fingerprint |
| 408 | `request_timeout` | Timed out while reading the request |
| 409 | `investigation_in_progress` | An investigation already exists |
| 413 | `request_too_large` | Request body exceeds the size limit |
| 500 | `internal_error` | An internal error occurred |

## Constraints and Verification

- Callers need permission to create SelfSubjectAccessReviews and AgenticRuns in the adapter namespace. The adapter needs cluster-scoped TokenReview create permission; impersonation and SubjectAccessReview permissions are not required.
- Native AgenticRun create permission also permits direct Kubernetes creation. Adapter-specific restrictions do not govern those direct requests without equivalent admission controls.
- Body/header limits, deadline propagation, connection timeouts, mandatory TLS, and certificate-restart behavior are defined in [configuration](configuration.md#fixed-https-limits).
- Errors generated before the handler may use standard HTTP responses rather than the JSON structure. Disconnected clients may receive no response.
- UI routing and external transport deployment are outside this component's API contract.
- Focused mock-based tests cover application behavior. Service CA trust, real authorization/admission, auditing, credential isolation across concurrent callers, and concurrent API/poller creation still require integration verification.
