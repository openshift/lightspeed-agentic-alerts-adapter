# AgenticRun Building

Translates AlertManager alerts into AgenticRun custom resources with deterministic naming, stable fingerprinting for deduplication, Kubernetes-safe metadata, and a templated request for the analysis agent.

## Behavioral Rules

### CR Construction

1. The adapter SHALL convert an AlertManager alert into an `AgenticRun` CR with deterministic naming, Kubernetes-safe metadata, and a templated request.
2. Two fingerprint labels SHALL be set on each AgenticRun: `agentic.openshift.io/alert-fingerprint` with the original AlertManager fingerprint (truncated to 8 characters) for UI lookups, and `agentic.openshift.io/alert-group-id` with the stable fingerprint for deduplication.
3. When the alert has a `namespace` label, the AgenticRun name SHALL be `{alertname}-{namespace}-{startsAt_hash}`.
4. When the alert has no `namespace` label, the AgenticRun name SHALL be `{alertname}-{startsAt_hash}`.
4a. In both name formats, `startsAt_hash` SHALL be the first eight lowercase hex characters of SHA-256 over `startsAt` formatted in UTC using RFC 3339. When the reconciliation target identity is non-empty, the hash input SHALL append a null byte (`\0`) followed by that target identity. When the target identity is empty, the hash input SHALL contain only the formatted time.
5. Building the same alert twice with the same target settings SHALL produce AgenticRuns with identical names, enabling Kubernetes 409 deduplication for the exact same alert instance.
6. When equivalent alerts from two reconciliation targets are built with different target identities, their AgenticRuns SHALL have distinct deterministic names, while repeated builds for either target identity SHALL retain the same name.

### Stable Fingerprint (Scope Hashing)

7. The adapter SHALL compute a stable fingerprint by removing a configurable set of ignored labels from the alert's label set and sorting the remaining keys lexicographically. For each key in that order, it SHALL hash the unsigned base-128 varint of the key's byte length, the key bytes, the unsigned base-128 varint of the value's byte length, and the value bytes, in that order, using FNV-64a. The fingerprint SHALL be the first 8 characters of the 16-character lowercase hexadecimal hash.
8. Two alerts differing only in ignored labels (e.g., different pod names) SHALL produce the same stable fingerprint.
9. Non-ignored labels SHALL contribute to the stable fingerprint. The eight-character hash is not a unique identifier; collisions are possible.
10. When the ignored labels list is empty, all alert labels SHALL be included in the hash.
11. When an alert is missing its AlertManager fingerprint, the adapter SHALL return a build error.
11a. When an alert is missing `startsAt`, the adapter SHALL return a build error.

### EmergencyStopped Replacement

12. The adapter SHALL support creating a replacement AgenticRun for a still-firing alert when the original alert-derived name already exists for an EmergencyStopped run and the alert is eligible for creation.
13. A replacement AgenticRun SHALL use the next deterministic retry name, which SHALL remain within the Kubernetes name length limit.
14. The same set of existing AgenticRuns evaluated for the same alert instance SHALL always produce the same next retry name.

### Metadata Sanitization

15. Name components SHALL be lowercased, and characters outside `a-z`, `0-9`, and hyphen SHALL be replaced with hyphens.
16. When the computed name exceeds 63 characters, the current builder SHALL shorten the alertname component while preserving the namespace and hash suffix. It retains at least one alertname character.
16a. [PLANNED] All generated names SHALL fit within 63 characters, including names built from long namespaces. The current algorithm can exceed this limit when the namespace alone leaves too little room. The rule for shortening the namespace is still to be decided.
17. Label values exceeding 63 characters SHALL be truncated to 63 characters and trimmed of trailing non-alphanumeric characters.
18. Invalid characters in label values SHALL be replaced with hyphens; leading/trailing non-alphanumeric characters SHALL be trimmed.

### Request Template

19. The adapter SHALL render `spec.request` with the following fields: the alert name, severity, namespace, description, and runbook URL. Only allow-listed fields SHALL be passed to the template; the full Labels map SHALL NOT be included.
20. When the alert has a description annotation, it SHALL appear in the rendered request. A non-empty summary SHALL be stored in `agentic.openshift.io/alert-summary`, limited to 256 bytes; the current request does not render the summary.
21. Missing description SHALL render as an empty field. Missing or empty summary SHALL omit the summary annotation. Neither case SHALL cause an error.
22. Unicode control characters (except newline), Unicode format characters, and backtick runs of 3 or more SHALL be stripped from alert data before template rendering.
23. Single and double backticks SHALL be preserved.
24. Extra labels beyond allow-listed fields (alertname, severity, namespace) SHALL NOT appear in the rendered request.
25. When shared skills are configured with paths, the rendered request SHALL contain a skill hint listing those paths (prefixed with `/app`).
26. When no run-level skills are configured, the rendered request SHALL contain the generic investigation instruction instead of a skill hint.

### Workflow Steps

27. The adapter SHALL set analysis, execution, and verification steps. Each step SHALL select its agent using the precedence in [configuration](configuration.md#agent-selection): step override, shared override, then `default`.
28. When shared skills are configured, `spec.tools.skills` SHALL contain the configured entries with their images and paths.
29. When no run-level skills are configured, `spec.tools` SHALL be omitted (zero value).

### AgenticRun CRUD

30. `ListAgenticRuns` SHALL list AgenticRun CRs filtered by the `agentic.openshift.io/source=alertmanager` label to support deduplication queries.
31. In single-cluster mode, local deduplication SHALL include only Alertmanager-created runs without a target-identity label. In multicluster mode, the local client SHALL include only runs matching its current local target identity.
31a. [PLANNED] Local deduplication SHALL preserve active-run and cooldown history when switching between single-cluster and multicluster modes. Current listing rules do not include local runs from the other mode.
32. When the Kubernetes API returns an error during listing, `ListAgenticRuns` SHALL return a wrapped error with context.
33. `CreateAgenticRun` SHALL create the AgenticRun on the hub cluster. For spoke targets, the AgenticRun SHALL carry the spoke target identity label.
34. When the Kubernetes API returns 409 AlreadyExists, `CreateAgenticRun` SHALL log at Info level and return `false, nil`.
35. When the Kubernetes API returns a non-409 error, `CreateAgenticRun` SHALL return `false` and a wrapped error.

## Output Metadata

| Field | Value |
|---|---|
| `metadata.namespace` | Adapter namespace from `POD_NAMESPACE`, default `openshift-lightspeed` |
| `agentic.openshift.io/source` label | `alertmanager` |
| `agentic.openshift.io/alert-fingerprint` label | First eight characters of the original fingerprint, sanitized |
| `agentic.openshift.io/alert-group-id` label | Stable fingerprint from rule 7 |
| `agentic.openshift.io/alert-name` label | Lowercase alert name, sanitized |
| `agentic.openshift.io/alert-severity` label | Alert severity, sanitized |
| `agentic.openshift.io/alert-starts-at` annotation | Non-zero start time in UTC RFC 3339 format |
| `agentic.openshift.io/alert-summary` annotation | Non-empty summary, limited to 256 bytes |

Target identity rules remain in [multicluster](multicluster.md#target-identified-agenticruns).

## Constraints

The desired 63-character name limit comes from the operator's use of the name as a label value. Rule 16a records the remaining long-namespace gap. Fingerprint and name hashes can collide; deterministic naming prevents repeated creation of the same name but does not guarantee one run for every distinct alert.
