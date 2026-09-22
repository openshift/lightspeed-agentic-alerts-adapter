## Context

The adapter reconciles each target's firing alerts on a periodic poll and currently attempts every eligible AgenticRun creation on every cycle. `AgenticRunClient.CreateAgenticRun` maps AlreadyExists responses to `(false, nil)` and otherwise returns wrapped Kubernetes client errors. Target reconciliation can run concurrently, so creation backoff state must be safe for concurrent access.

## Goals / Non-Goals

**Goals:**
- Suppress repeated failed creation requests per target and stable alert group across poll cycles.
- Recover automatically when a creation request completes without an error.
- Keep retry state bounded and observable.

**Non-Goals:**
- Persist retry state across adapter restarts.
- Change retry behavior for alert retrieval, run listing, run construction, or suspension lookup.
- Add user-configurable backoff parameters.
- Classify creation errors by Kubernetes status other than retaining the existing AlreadyExists behavior.

## Decisions

### Use `flowcontrol.Backoff` for keyed state

`k8s.io/client-go/util/flowcontrol.Backoff` supplies thread-safe per-key exponential delays, reset, and garbage collection. The adapter initializes it with a one-minute initial delay and ten-minute maximum delay. Its built-in factor of two provides the sequence `1m, 2m, 4m, 8m, 10m`.

`wait.ExponentialBackoff` was rejected because it performs synchronous retries within one reconciliation call, which blocks later alerts and produces multiple requests in the same poll cycle. A local map was rejected because it would duplicate keyed timing, synchronization, reset, and cleanup behavior already provided by `flowcontrol.Backoff`.

### Key state by target and stable alert group

The state key is the target name joined with the stable alert-group fingerprint using a NUL separator. The stable fingerprint is already used for deduplication. Including the target prevents failure on one spoke or local target from affecting another target's matching alert group.

### Advance on every actual creation error

Every non-nil error from `CreateAgenticRun` advances the key's backoff. This covers persistent authorization, availability, authentication, validation, and operator-side failures without error classification. A creation call that returns without error resets any state for that key. Since the production client represents AlreadyExists as `(false, nil)`, it preserves the existing no-op behavior and clears stale failure state.

If the target reconciliation context has been canceled, reconciliation stops without recording a backoff entry. This avoids treating adapter shutdown or the poll-cycle deadline as an alert-specific creation failure.

### Check before each create and collect stale keys per cycle

After current receiver, pre-run, active-run, post-run, and build checks, the adapter skips a create while the key remains in backoff. It invokes backoff garbage collection once per active reconciliation cycle to discard inactive keys.

### Log state transitions instead of duplicate failures

A creation error is logged once as either entering or increasing backoff, with the target, alert identity, error, and resulting duration. A successful or AlreadyExists response that clears state logs backoff exit with the prior duration. Skips within an active delay are debug-logged.

## Risks / Trade-offs

- [Backoff is reset on adapter restart] → The adapter remains stateless with respect to durable cluster state; a restarted instance can make one immediate attempt before beginning a new delay.
- [Transient creation errors retry less often than today] → This is intentional scope: all create errors use the same bounded per-key schedule, while unrelated reconciliation errors remain unchanged.
- [Distinct failures for a key share one delay] → The key represents the alert group whose creation is failing; a later successful/no-op result clears it.

## Migration Plan

Deploy the adapter normally. No API, manifest, or configuration migration is required. To roll back, deploy the prior adapter version; its previous per-poll creation behavior resumes after restart.
