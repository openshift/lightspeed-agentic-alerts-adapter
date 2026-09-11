## 1. Terminal phase handling

- [x] 1.1 Add `EmergencyStopped` to the adapter's terminal phase handling and verify active-run checks no longer treat it as active with unit tests.
- [x] 1.2 Use the EmergencyStopped condition transition time for post-run delay evaluation and verify recent EmergencyStopped runs are skipped with unit tests.

## 2. Replacement naming

- [x] 2.1 Add deterministic retry-name selection for replacements when the original alert-derived AgenticRun name already exists and verify the next suffix is selected from existing runs with unit tests.
- [x] 2.2 Preserve existing name sanitization and length constraints when adding retry suffixes and verify long names remain within the Kubernetes limit with unit tests.
- [x] 2.3 Keep the existing first-attempt AgenticRun name format unchanged and verify existing builder tests continue to pass.

## 3. Reconcile behavior

- [x] 3.1 Ensure matching EmergencyStopped runs outside post-run delay allow replacement AgenticRun creation and verify with a reconcile-level unit test.
- [x] 3.2 Ensure matching EmergencyStopped runs within post-run delay skip replacement AgenticRun creation and verify with a reconcile-level unit test.
- [x] 3.3 Ensure active non-terminal runs still block creation and normal terminal phases still honor post-run delay by running the existing adapter test suite.

## 4. Validation

- [x] 4.1 Run `go test ./...` and verify all tests pass.
- [x] 4.2 Run `openspec validate handle-emergencystopped-agenticruns --strict` and verify the change is valid.
