## 1. Test Infrastructure

- [x] 1.1 Create `test/e2e/` directory structure
- [x] 1.2 Create `test/e2e/framework/config.go`: load config from env vars (NAMESPACE, DEPLOYMENT_NAME, IMAGE, OPERATOR_NAMESPACE)
- [x] 1.3 Create `test/e2e/framework/cluster.go`: `oc`-based cluster utilities (GetSelector, IsDeploymentAvailable, HasPods, ArePodsRunning, GetLogs, GetPodStatus, etc.) — pattern from cluster-health-analyzer
- [x] 1.4 Create `test/e2e/e2e_suite_test.go`: Ginkgo suite setup (BeforeSuite: verify adapter deployed, AfterSuite: collect artifacts on failure)
- [x] 1.5 Add Ginkgo dependency to `go.mod`

## 2. Deploy Script

- [x] 2.1 Create `hack/deploy-e2e.sh`: deploy script following cluster-health-analyzer pattern
- [x] 2.2 Script: verify prerequisites (`oc`, `yq`, cluster login)
- [x] 2.3 Script: read env vars (IMAGE, NAMESPACE, DEPLOYMENT_NAME) with defaults
- [x] 2.4 Script: copy manifests to temp directory
- [x] 2.5 Script: patch `deployment.yaml` with IMAGE using `yq`
- [x] 2.6 Script: patch `configmap.yaml` with test values (pollInterval: 10s, postRunDelay: 5m, filtering.allowedReceivers: [default]) using `yq`
- [x] 2.7 Script: apply manifests with `oc apply -f`
- [x] 2.8 Script: wait for deployment available with `oc wait --for=condition=available --timeout=300s`
- [x] 2.9 Script: auto-install lightspeed-agentic-operator if CRD not present (git clone + install.sh)
- [x] 2.10 Make script executable and add trap for cleanup on error

## 3. Happy Path Tests

- [x] 3.1 Create `test/e2e/reconciliation_test.go`: Ginkgo test file
- [x] 3.2 Test: verify adapter deployment is available and pods are running
- [x] ~3.3 Test: query AlertManager for firing alerts~ — skipped: tests inject alerts explicitly via AlertManager API, making this precondition check redundant
- [x] 3.4 Test: wait for adapter to create AgenticRun for injected alert (poll for AgenticRun with alert-name label)
- [x] 3.5 Test: verify AgenticRun has correct labels (alert-fingerprint, alert-dedup-fingerprint)


## 4. Deduplication Tests

- [x] 4.1 Create `test/e2e/deduplication_test.go`: Ginkgo test file
- [x] 4.2 Test: inject alert with severity `info`, verify no AgenticRun created after multiple poll cycles
- [x] 4.3 Test: inject alert with severity `none`, verify no AgenticRun created
- [x] 4.4 Test: inject alert routed to non-allowed receiver (Critical), verify no AgenticRun created
- [x] 4.5 Test: create AgenticRun manually with known dedup fingerprint, verify adapter skips creating duplicate
- [x] 4.6 Test: create AgenticRun manually with Completed status (Verified=True, recent lastTransitionTime), inject alert with same dedup fingerprint, verify adapter skips creating duplicate (within postRunDelay)
- [x] 4.7 Test: create AgenticRun manually with Failed status (Analyzed=False, old lastTransitionTime >5m), inject alert with same dedup fingerprint, verify adapter creates new AgenticRun (outside postRunDelay)

## 5. Fingerprint Tests

- [x] 5.1 Test: verify AgenticRun has both `alert-fingerprint` and `alert-dedup-fingerprint` labels (covered in 3.5)
- [x] 5.2 Test: inject two alerts differing only in `pod` label (ignored label), verify only one AgenticRun created (same dedup fingerprint)

## 6. Error Handling Tests

- [x] 6.1 Test: check adapter logs for 'already exists' messages at Info level (not Error), verifying 409 AlreadyExists is handled gracefully

## 7. Make Targets

- [x] 7.1 Add `make deploy-e2e` target: runs `hack/deploy-e2e.sh`, sets IMAGE from env
- [x] 7.2 Add `make test-e2e` target: installs Ginkgo if needed, runs `ginkgo -v ./test/e2e/...`
- [x] 7.3 Add `make undeploy-e2e` target: runs `oc delete -f manifests/ --ignore-not-found -n <namespace>`
- [x] 7.4 Update `make test` to exclude E2E tests: `go test $$(go list ./... | grep -v /test/e2e)`

## 8. Documentation

- [x] 8.1 Create `test/e2e/README.md`: how to run E2E tests locally and in CI
- [x] 8.2 Document prerequisites: oc CLI, yq, cluster access, lightspeed-agentic-operator deployed
- [x] 8.3 Document env vars: IMAGE, NAMESPACE, DEPLOYMENT_NAME
- [x] 8.4 Update main README with link to E2E test docs

## 9. CI Integration (separate PR to openshift/release)

- [ ] 9.1 Create step-registry definition: `ci-operator/step-registry/lightspeed-agentic-alerts-adapter/e2e/`
- [ ] 9.2 Add `lightspeed-agentic-alerts-adapter-e2e-commands.sh`: install dependencies (yq), deploy operator, run `make deploy-e2e`, run `make test-e2e`
- [ ] 9.3 Add `lightspeed-agentic-alerts-adapter-e2e-ref.yaml`: step definition (timeout, resources, from: src)
- [ ] 9.4 Add artifact collection (pod logs, events, deployment yaml) on failure
- [ ] 9.5 Configure job as presubmit, optional initially
- [ ] 9.6 Add Slack reporting to team channel (if applicable)
