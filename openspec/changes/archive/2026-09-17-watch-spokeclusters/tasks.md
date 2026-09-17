## 1. Dynamic target source

- [x] 1.1 Replace the adapter’s fixed target slice with a safe target-source snapshot interface and verify adapter unit tests cover target replacement and removal between poll cycles.
- [x] 1.2 Move the mutex-protected spoke-target registry and its unit tests into `internal/multicluster`, preserving fixed local targets and independent snapshots.

## 2. SpokeCluster reconciliation

- [x] 2.1 Move spoke target construction and the multicluster-only SpokeCluster controller into `internal/multicluster`; verify create, label update, label removal, invalid Secret, and delete cases with table-driven unit tests.
- [x] 2.2 Initialize the registry from the existing startup discovery, start the controller-runtime manager only in multicluster mode, and stop polling when the manager exits unexpectedly; verify startup and manager lifecycle behavior with tests.

## 3. Deployment permissions and validation

- [x] 3.1 Add `get` and `watch` permissions for `hub.openshift.io/spokeclusters` and verify the rendered RBAC manifest grants the required verbs.
- [x] 3.2 Run `make fmt` and `make test` and verify both commands complete successfully.
