## 1. Creation backoff

- [x] 1.1 Add adapter-owned keyed `flowcontrol.Backoff` state with fixed initial and maximum delays, and verify the adapter constructs it for production reconciliation.
- [x] 1.2 Gate AgenticRun creation by the target-and-stable-alert-group backoff key; advance all non-cancellation create errors, clear state on no-error responses, and garbage-collect stale entries each reconciliation cycle; verify existing non-creation error paths remain unchanged.
- [x] 1.3 Add transition and skip logging for creation backoff entry, increase, active delay, and exit; verify logs include target, alert identity, and duration.

## 2. Tests

- [x] 2.1 Add deterministic backoff tests using a fake clock for initial delay, exponential increase, skip behavior, success reset, and target/alert-group isolation.
- [x] 2.2 Add compatibility tests confirming AlreadyExists does not advance backoff, non-creation errors do not create state, and canceled reconciliation errors do not create state.
- [x] 2.3 Run `go test ./...` and verify all tests pass.
