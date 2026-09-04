## 1. AgenticOLSConfig Suspension Check

- [x] 1.1 Add an `AgenticOLSConfig` client that gets the cluster-scoped singleton named `cluster` and returns `spec.suspended`
- [x] 1.2 Treat missing `AgenticOLSConfig` CRD or singleton object as unsuspended, and return other read errors to the caller
- [x] 1.3 Add a `SuspensionSource` to the adapter and check it before AlertManager or `AgenticRun` access in every reconcile cycle
- [x] 1.4 Wire the `AgenticOLSConfig` client into `main` and remove the `SUSPENDED` startup gate

## 2. Tests

- [x] 2.1 Add table-driven adapter tests verifying suspended and suspension-read-error cycles do not call AlertManager or list or create `AgenticRun` resources
- [x] 2.2 Add table-driven `AgenticOLSConfig` client tests for suspended, unsuspended, and missing singleton states
- [x] 2.3 Run `go test ./...` and verify all tests pass

## 3. Manifests and Documentation

- [x] 3.1 Grant the adapter service account cluster-scoped `get` permission for `agenticolsconfigs`
- [x] 3.2 Remove the `SUSPENDED` deployment example and document `AgenticOLSConfig.spec.suspended` behavior in the README and architecture documentation

## 4. Validation

- [x] 4.1 Update active and archived OpenSpec records to describe per-cycle `AgenticOLSConfig` suspension checks
- [x] 4.2 Run `openspec validate --specs --strict` and verify the specs pass validation
