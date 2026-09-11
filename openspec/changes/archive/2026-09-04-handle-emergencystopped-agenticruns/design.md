## Context

The adapter lists existing AgenticRuns during each reconcile cycle and skips alerts that already have an active matching run. Terminal runs are handled by the post-run delay check.

`EmergencyStopped` is a terminal phase in the AgenticRun API, but the adapter currently does not include it in its terminal phase handling. As a result, a matching EmergencyStopped run is treated as active and can block future processing indefinitely.

The existing AgenticRun name is deterministic for an alert instance and includes the alert `startsAt` hash. For a still-firing alert, `startsAt` remains unchanged. Therefore, if the adapter tries to create a replacement run for the same still-firing alert, the original name may already be taken by the EmergencyStopped run.

## Goals / Non-Goals

**Goals:**

- Treat `EmergencyStopped` as terminal in adapter lifecycle checks.
- Apply the configured post-run delay to EmergencyStopped runs.
- Allow a new AgenticRun to be created for the same still-firing alert after post-run delay has elapsed.
- Preserve the adapter's create-only behavior.
- Keep the behavior stateless by deriving replacement names from existing AgenticRuns listed during the current cycle.

**Non-Goals:**

- Reading or interpreting global suspension state directly.
- Deleting, updating, or mutating existing EmergencyStopped AgenticRuns.
- Changing the post-run delay configuration model.

## Decisions

### 1. EmergencyStopped is terminal

The adapter will include `EmergencyStopped` in terminal phase handling. Matching EmergencyStopped runs will no longer count as active runs.

### 2. EmergencyStopped uses the normal post-run delay

The adapter will use the EmergencyStopped condition transition time as the terminal time. If the configured post-run delay has not elapsed, the alert remains skipped. If the delay has elapsed, the alert can proceed to creation.

This keeps retry behavior consistent with other terminal phases and avoids immediate repeated creation.

### 3. Replacement runs use deterministic retry suffixes

When the adapter is allowed to create a run for an alert whose original deterministic name is already used by an EmergencyStopped run, it will choose the next deterministic retry name based on existing runs.

The first attempt keeps the existing name format. Replacement attempts append a suffix such as `-retry-1`, `-retry-2`, and so on.

The suffix is chosen from the AgenticRuns returned by the current list call, so the adapter remains stateless.

### 4. Existing deterministic naming remains unchanged for normal first attempts

The existing name format remains the default. Retry suffixes are only used when needed to create a replacement after an EmergencyStopped run.

## Risks / Trade-offs

- **[Risk] Name length constraints** → Retry suffixes consume part of the 63-character name budget. The implementation must preserve the existing name-length guarantees by trimming the base name when needed.
- **[Risk] Concurrent creators** → The adapter is designed as a single replica. If multiple instances run, two instances could choose the same retry suffix. Existing 409 handling remains the fallback.
- **[Trade-off] More naming complexity** → Retry naming adds complexity, but it is required to create a replacement for the same still-firing alert while preserving create-only behavior.
