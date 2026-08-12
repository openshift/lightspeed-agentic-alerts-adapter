## 1. Test Infrastructure

- [ ] 1.1 Create `test/e2e/` directory structure
- [ ] 1.2 Create `test/e2e/framework/config.go`: load config from env vars (NAMESPACE, DEPLOYMENT_NAME, IMAGE, OPERATOR_NAMESPACE)
- [ ] 1.3 Create `test/e2e/framework/cluster.go`: `oc`-based cluster utilities (GetSelector, IsDeploymentAvailable, HasPods, ArePodsRunning, GetLogs, GetPodStatus, etc.) — pattern from cluster-health-analyzer
- [ ] 1.4 Create `test/e2e/e2e_suite_test.go`: Ginkgo suite setup (BeforeSuite: verify adapter deployed, AfterSuite: collect artifacts on failure)
- [ ] 1.5 Add Ginkgo dependency to `go.mod`

## 2. Deploy Script

- [ ] 2.1 Create `hack/deploy-e2e.sh`: deploy script following cluster-health-analyzer pattern
- [ ] 2.2 Script: verify prerequisites (`oc`, `yq`, cluster login)
- [ ] 2.3 Script: read env vars (IMAGE, NAMESPACE, DEPLOYMENT_NAME) with defaults
- [ ] 2.4 Script: copy manifests to temp directory
- [ ] 2.5 Script: patch `deployment.yaml` with IMAGE using `yq`
- [ ] 2.6 Script: patch `configmap.yaml` with test values (pollInterval: 10s, postRunDelay: 1m, allowedReceivers: [Default]) using `yq`
- [ ] 2.7 Script: apply manifests with `oc apply -f`
- [ ] 2.8 Script: wait for deployment available with `oc wait --for=condition=available --timeout=300s`
- [ ] 2.9 Script: verify operator is deployed and running (check `openshift-lightspeed` namespace for operator deployment)
- [ ] 2.10 Make script executable and add trap for cleanup on error

## 3. Happy Path Tests

- [ ] 3.1 Create `test/e2e/reconciliation_test.go`: Ginkgo test file
- [ ] 3.2 Test: verify adapter deployment is available and pods are running
- [ ] 3.3 Test: query AlertManager for firing alerts (verify at least one exists routed to Default receiver)
- [ ] 3.4 Test: wait for adapter to create AgenticRun (poll for AgenticRun with alert-fingerprint label matching a known alert)
- [ ] 3.5 Test: verify AgenticRun has correct labels (alert-fingerprint, alert-dedup-fingerprint)
- [ ] 3.6 Test: wait for operator to reconcile AgenticRun (status phase transitions from empty/Pending to another phase)

## 4. Deduplication Tests

- [ ] 4.1 Create `test/e2e/deduplication_test.go`: Ginkgo test file
- [ ] 4.2 Test: verify alert with severity `info` does not create AgenticRun (check AlertManager for info-severity alert, verify no AgenticRun created after 2+ poll cycles)
- [ ] 4.3 Test: verify alert with severity `none` does not create AgenticRun
- [ ] 4.4 Test: verify alert routed to non-allowed receiver does not create AgenticRun
- [ ] 4.5 Test: create AgenticRun manually with phase `Analyzing`, verify adapter skips creating duplicate for same alert fingerprint
- [ ] 4.6 Test: create AgenticRun manually with phase `Completed` and recent completion time, verify adapter skips creating duplicate (within postRunDelay)
- [ ] 4.7 Test: create AgenticRun manually with phase `Failed` and old completion time, verify adapter creates new AgenticRun (outside postRunDelay)

## 5. Fingerprint Tests

- [ ] 5.1 Test: verify AgenticRun has both `alert-fingerprint` and `alert-dedup-fingerprint` labels
- [ ] 5.2 Test: verify two alerts differing only in `pod` label produce same `alert-dedup-fingerprint` (check for alerts with same alertname/namespace but different pod, verify both create AgenticRuns with same dedup fingerprint OR second is skipped due to dedup)

## 6. Config Reload Tests

- [ ] 6.1 Create `test/e2e/config_test.go`: Ginkgo test file
- [ ] 6.2 Test: update ConfigMap `allowedReceivers` to a different receiver, wait for next poll cycle, verify adapter processes alerts from new receiver and skips old receiver (no pod restart)
- [ ] 6.3 Helper: get adapter pod start time before config change, verify same pod still running after (no restart)

## 7. Error Handling Tests

- [ ] 7.1 Test: create AgenticRun manually with specific name, trigger alert that would create same name, verify adapter logs 409 and continues (check logs for "already exists" message)
- [ ] 7.2 Test: verify adapter continues after poll cycle errors (may be hard to trigger in live cluster; document as manual test or skip for MVP)

## 8. Make Targets

- [ ] 8.1 Add `make deploy-e2e` target: runs `hack/deploy-e2e.sh`, sets IMAGE from env or default
- [ ] 8.2 Add `make test-e2e` target: installs Ginkgo if needed, runs `ginkgo -v ./test/e2e/...`
- [ ] 8.3 Add `make undeploy-e2e` target: runs `oc delete -f manifests/ --ignore-not-found -n <namespace>`
- [ ] 8.4 Update `make test` to exclude E2E tests: `go test $$(go list ./... | grep -v /test/e2e)`

## 9. Documentation

- [ ] 9.1 Create `test/e2e/README.md`: how to run E2E tests locally (oc login, make deploy-e2e, make test-e2e)
- [ ] 9.2 Document prerequisites: oc CLI, yq, cluster access, lightspeed-agentic-operator deployed
- [ ] 9.3 Document env vars: IMAGE, NAMESPACE, DEPLOYMENT_NAME
- [ ] 9.4 Update main README or CONTRIBUTING.md with link to E2E test docs

## 10. CI Integration (separate PR to openshift/release)

- [ ] 10.1 Create step-registry definition: `ci-operator/step-registry/lightspeed-agentic-alerts-adapter/e2e/`
- [ ] 10.2 Add `lightspeed-agentic-alerts-adapter-e2e-commands.sh`: install dependencies (yq), deploy operator, run `make deploy-e2e`, run `make test-e2e`
- [ ] 10.3 Add `lightspeed-agentic-alerts-adapter-e2e-ref.yaml`: step definition (timeout, resources, from: src)
- [ ] 10.4 Add artifact collection (pod logs, events, deployment yaml) on failure
- [ ] 10.5 Configure job as presubmit, optional initially
- [ ] 10.6 Add Slack reporting to team channel (if applicable)
