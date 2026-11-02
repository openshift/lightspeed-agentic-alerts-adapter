# Poll Loop

The core reconcile loop that continuously polls AlertManager, applies receiver filtering and stateless deduplication, and creates AgenticRun CRs for qualifying alerts. Includes creation backoff to limit repeated failures.

## Behavioral Rules

### Polling

1. The adapter SHALL use the configuration loaded at startup for every cycle. The default poll interval is 30 seconds.
2. The adapter SHALL run an immediate reconcile at startup, then poll on a fixed interval until shutdown. Configuration changes SHALL require a process restart.
3. For each configured reconciliation target, the filter order SHALL be: receiver allowlist, pre-run delay, active AgenticRun, post-run delay, creation backoff.

### Suspended Mode

4. At the start of each reconcile cycle, the adapter SHALL read the cluster-scoped `AgenticOLSConfig` singleton named `cluster`; when `spec.suspended` is true, the adapter SHALL skip that cycle before polling AlertManager, listing AgenticRuns, or creating AgenticRuns.
5. When the `AgenticOLSConfig` CRD or singleton object is absent, the adapter SHALL behave as if suspended mode is disabled.
6. When reading the `AgenticOLSConfig` suspension state fails for a reason other than absent CRD or absent singleton, the adapter SHALL log the error and skip the reconcile cycle; the next poll retries.

### Target Reconciliation

7. When suspended mode is disabled and the poll interval elapses, the adapter SHALL fetch alerts from every configured reconciliation target, list hub AgenticRuns matching that target's identity, apply receiver filtering then dedup rules independently for that target, and create target-identified AgenticRuns on the hub for qualifying alerts.
8. When AlertManager returns an error for one target, the adapter SHALL log the target-specific error, skip that target's reconciliation, and continue for remaining targets.
9. When the hub Kubernetes API returns an error during AgenticRun listing or creation for one target, the adapter SHALL log the error, skip the failed operation for that target, and continue for remaining targets.

### Concurrent Target Reconciliation

10. When `--multicluster` is set, the adapter SHALL reconcile independent targets concurrently while limiting simultaneous reconciliations to `MULTICLUSTER_MAX_CONCURRENT_TARGETS` (default 4, must be a positive integer).
11. Alert processing within a target SHALL remain sequential, and the adapter SHALL wait for all started target reconciliations before beginning another poll cycle.
12. When `--multicluster` is not set, the adapter SHALL reconcile the local target sequentially and ignore `MULTICLUSTER_MAX_CONCURRENT_TARGETS`.
12a. When `--multicluster` is set and `MULTICLUSTER_MAX_CONCURRENT_TARGETS` is absent, the adapter SHALL use four concurrent targets.
13. When `--multicluster` is set and `MULTICLUSTER_MAX_CONCURRENT_TARGETS` is explicitly set to a value that is not a positive integer, the adapter SHALL fail startup with an error.

### Target Timeout

14. The adapter SHALL apply the configured `pollInterval` as a deadline to every target reconciliation. AlertManager and hub Kubernetes operations for a target SHALL use that deadline while preserving cancellation from the parent context.
15. When an operation for a target does not complete within `pollInterval`, the target reconciliation context SHALL be cancelled and remaining targets SHALL continue.

### Receiver Filtering

16. The adapter SHALL skip any alert whose AlertManager receivers do not include at least one entry from the configured `allowedReceivers` list. Comparison SHALL be case-insensitive.
17. When an alert has no receivers or an empty receivers list, the alert SHALL be skipped and logged at Debug level.
18. When the allowlist is empty (explicitly `[]`), all alerts are skipped.
19. The adapter SHALL log the effective `allowedReceivers` list at Info level at startup.

### Pre-Run Delay (Skip Transient Alerts)

20. The adapter SHALL not create an AgenticRun for an alert that has been firing for less than the configured `preRunDelay`, to filter out transient alerts that resolve on their own.
21. When `preRunDelay` is 0, all alerts pass the pre-run delay check regardless of firing duration.

### Active AgenticRun Check

22. The adapter SHALL not create an AgenticRun for an alert that already has an active (non-terminal) AgenticRun, identified by matching the `alert-group-id` label.
23. `EmergencyStopped` SHALL be treated as terminal, not active.

### Post-Run Delay (Cooldown)

24. The adapter SHALL not create an AgenticRun for an alert that has a terminal AgenticRun (Completed, Failed, Denied, Escalated, EmergencyStopped) within the configured `postRunDelay` (default 1h), to avoid repeated analysis of recently handled alerts. Rule 25a records the current gap for analysis-only completion.
25. The terminal time SHALL come from the `LastTransitionTime` of the condition that caused termination. Current mappings are Completed → Verified, Denied → Denied, Escalated → Escalated, EmergencyStopped → EmergencyStopped, and Failed → the first condition with status False.
25a. [PLANNED] For Completed runs with `Analyzed=True` and reason `NoActionRequired`, cooldown SHALL use the Analyzed transition time. The current implementation looks only for Verified for Completed runs, so it does not apply cooldown to these analysis-only completions.
26. When `postRunDelay` is 0, all alerts pass the post-run delay check.

### Creation Backoff

27. The adapter SHALL defer repeated AgenticRun creation attempts after a creation error. Backoff state SHALL be isolated by reconciliation target and stable alert-group identity, and SHALL increase exponentially from an initial delay (1 minute) to a bounded maximum (10 minutes).
28. When a creation attempt succeeds after a target and alert group entered backoff, the adapter SHALL clear its backoff state so a future failure begins at the initial delay.
29. An AlreadyExists response SHALL remain a no-op for creation and SHALL clear any existing backoff for that target and alert group.
30. Errors outside AgenticRun creation, including retrieval and build failures, SHALL NOT use creation backoff. Creation failures caused by target timeout or parent cancellation SHALL end target processing without advancing backoff.
31. The adapter SHALL log when an alert group enters creation backoff, when its delay increases, and when backoff is cleared, including the target, alert identity, and applicable duration.

31a. Creation backoff SHALL be kept in memory and reset on process restart. Unused entries may expire.
31b. A newly created AgenticRun SHALL be included in later deduplication checks in the same target cycle.
31c. Passing cooldown SHALL permit a creation attempt; it SHALL NOT guarantee a new run. An unchanged deterministic name can still return AlreadyExists. Retry names are used for the EmergencyStopped conflict described in [AgenticRun building](agenticrun-building.md#retry-names-and-polling-replacement).

### Graceful Shutdown

32. The adapter SHALL exit cleanly on SIGTERM or SIGINT, completing or cancelling any in-flight poll cycle before stopping, and exit with status code 0.

## Constraints

Receiver filtering is the only configured alert-selection filter. There is no separate severity gate: an `info` or `none` alert can pass when its receiver is allowed. Each target processes alerts in sequence; targets may run concurrently. A poll cycle waits for all started target tasks.
