package e2e_test

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/test/e2e/framework"
)

var _ = Describe("Deduplication", func() {
	Context("severity filtering", func() {
		infoLabels := map[string]string{
			"alertname": "E2ETestSeverityInfo",
			"severity":  "info",
			"namespace": "openshift-lightspeed",
		}

		noneLabels := map[string]string{
			"alertname": "E2ETestSeverityNone",
			"severity":  "none",
			"namespace": "openshift-lightspeed",
		}

		AfterEach(func() {
			framework.ResolveAlert(infoLabels)  //nolint:errcheck
			framework.ResolveAlert(noneLabels)  //nolint:errcheck
		})

		It("should not create AgenticRun for info-severity alerts", func() {
			By("Injecting an info-severity alert")
			err := framework.InjectAlert(framework.Alert{Labels: infoLabels})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for multiple poll cycles and verifying no AgenticRun is created")
			Consistently(func() (string, error) {
				return framework.GetResourcesByLabel(
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestseverityinfo",
					"{.items[*].metadata.name}",
				)
			}, 30*time.Second, 5*time.Second).Should(BeEmpty(),
				"no AgenticRun should be created for info-severity alerts")
		})

		It("should not create AgenticRun for none-severity alerts", func() {
			By("Injecting a none-severity alert")
			err := framework.InjectAlert(framework.Alert{Labels: noneLabels})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for multiple poll cycles and verifying no AgenticRun is created")
			Consistently(func() (string, error) {
				return framework.GetResourcesByLabel(
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestseveritynone",
					"{.items[*].metadata.name}",
				)
			}, 30*time.Second, 5*time.Second).Should(BeEmpty(),
				"no AgenticRun should be created for none-severity alerts")
		})
	})

	Context("receiver filtering", func() {
		criticalLabels := map[string]string{
			"alertname": "E2ETestReceiverCritical",
			"severity":  "critical",
			"namespace": "openshift-lightspeed",
		}

		AfterEach(func() {
			framework.ResolveAlert(criticalLabels) //nolint:errcheck
		})

		It("should not create AgenticRun for alerts routed to a receiver not in allowedReceivers", func() {
			By("Injecting a critical-severity alert (routed to Critical receiver, not in allowedReceivers: [default])")
			err := framework.InjectAlert(framework.Alert{Labels: criticalLabels})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for multiple poll cycles and verifying no AgenticRun is created")
			Consistently(func() (string, error) {
				return framework.GetResourcesByLabel(
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestreceivercriti",
					"{.items[*].metadata.name}",
				)
			}, 30*time.Second, 5*time.Second).Should(BeEmpty(),
				"no AgenticRun should be created for alerts routed to Critical receiver when allowedReceivers is [default]")
		})
	})

	Context("active run deduplication", func() {
		const testDedupFP = "e2etest01"

		It("should not create a duplicate AgenticRun when an active run exists for the same dedup fingerprint", func() {
			testRunName := fmt.Sprintf("e2e-active-dedup-test-%d", time.Now().UnixNano()%100000)

			defer func() {
				framework.DeleteResource(agenticRunAPIResource, testRunName, cfg.Namespace) //nolint:errcheck
			}()

			By("Creating an AgenticRun with a known dedup fingerprint")
			yaml := fmt.Sprintf(`apiVersion: agentic.openshift.io/v1alpha1
kind: AgenticRun
metadata:
  name: %s
  namespace: %s
  labels:
    agentic.openshift.io/source: alertmanager
    agentic.openshift.io/alert-dedup-fingerprint: %s
    agentic.openshift.io/alert-name: e2e-test-alert
    agentic.openshift.io/alert-severity: critical
spec:
  request: "E2E test active dedup"
  analysis:
    agent: default
  execution:
    agent: default
  verification:
    agent: default`, testRunName, cfg.Namespace, testDedupFP)

			_, err := framework.CreateFromYAML(yaml)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for multiple poll cycles")
			time.Sleep(15 * time.Second)

			By("Verifying no additional AgenticRun was created with the same dedup fingerprint")
			names, err := framework.GetResourcesByLabel(
				agenticRunAPIResource,
				cfg.Namespace,
				fmt.Sprintf("agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-dedup-fingerprint=%s", testDedupFP),
				"{.items[*].metadata.name}",
			)
			Expect(err).NotTo(HaveOccurred())
			nameList := strings.Fields(names)
			Expect(nameList).To(HaveLen(1), "expected exactly one AgenticRun with dedup fingerprint %s, got: %v", testDedupFP, nameList)
			Expect(nameList[0]).To(Equal(testRunName))
		})
	})

	Context("ignored labels deduplication", func() {
		baseLabels := map[string]string{
			"alertname": "E2ETestIgnoredLabels",
			"severity":  "warning",
			"namespace": "openshift-lightspeed",
		}

		AfterEach(func() {
			framework.ResolveAlert(mergeLabels(baseLabels, map[string]string{"pod": "pod-1"})) //nolint:errcheck
			framework.ResolveAlert(mergeLabels(baseLabels, map[string]string{"pod": "pod-2"})) //nolint:errcheck
			framework.DeleteResourcesByLabel(                                                   //nolint:errcheck
				agenticRunAPIResource,
				cfg.Namespace,
				"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestignoredlabels",
			)
		})

		It("should treat two alerts differing only in an ignored label as the same alert", func() {
			alert1Labels := mergeLabels(baseLabels, map[string]string{"pod": "pod-1"})
			alert2Labels := mergeLabels(baseLabels, map[string]string{"pod": "pod-2"})

			By("Injecting the first alert (pod=pod-1)")
			err := framework.InjectAlert(framework.Alert{Labels: alert1Labels})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the adapter to create an AgenticRun")
			Eventually(func() (string, error) {
				return framework.GetResourcesByLabel(
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestignoredlabels",
					"{.items[0].metadata.name}",
				)
			}, 2*time.Minute, 10*time.Second).ShouldNot(BeEmpty())

			By("Injecting the second alert (pod=pod-2, differs only in ignored label)")
			err = framework.InjectAlert(framework.Alert{Labels: alert2Labels})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying no additional AgenticRun is created")
			Consistently(func() int {
				names, err := framework.GetResourcesByLabel(
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestignoredlabels",
					"{.items[*].metadata.name}",
				)
				if err != nil {
					return -1
				}
				if names == "" {
					return 0
				}
				return len(strings.Fields(names))
			}, 30*time.Second, 5*time.Second).Should(Equal(1),
				"alerts differing only in ignored label 'pod' should produce the same dedup fingerprint")
		})
	})

	Context("post-run delay", func() {
		postRunDelayLabels := map[string]string{
			"alertname": "E2ETestPostRunDelay",
			"severity":  "warning",
			"namespace": "openshift-lightspeed",
		}
		ignoredLabels := []string{"pod", "instance", "endpoint", "uid"}

		AfterEach(func() {
			framework.ResolveAlert(postRunDelayLabels) //nolint:errcheck
			framework.DeleteResourcesByLabel(          //nolint:errcheck
				agenticRunAPIResource,
				cfg.Namespace,
				"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestpostrundelay",
			)
		})

		It("should not create a new AgenticRun when a completed run exists within postRunDelay", func() {
			dedupFP := agenticrun.StableFingerprint(postRunDelayLabels, ignoredLabels)
			testRunName := fmt.Sprintf("e2e-postrundelay-test-%d", time.Now().UnixNano()%100000)

			By("Creating a fake Completed AgenticRun with matching dedup fingerprint")
			yaml := fmt.Sprintf(`apiVersion: agentic.openshift.io/v1alpha1
kind: AgenticRun
metadata:
  name: %s
  namespace: %s
  labels:
    agentic.openshift.io/source: alertmanager
    agentic.openshift.io/alert-dedup-fingerprint: %s
    agentic.openshift.io/alert-name: e2etestpostrundelay
    agentic.openshift.io/alert-severity: warning
spec:
  request: "E2E postRunDelay test"
  analysis:
    agent: default
  execution:
    agent: default
  verification:
    agent: default`, testRunName, cfg.Namespace, dedupFP)

			_, err := framework.CreateFromYAML(yaml)
			Expect(err).NotTo(HaveOccurred())

			By("Patching status to Completed (Verified=True) with recent completion time")
			now := time.Now().UTC().Format(time.RFC3339)
			patch := fmt.Sprintf(`{"status":{"conditions":[{"type":"Verified","status":"True","lastTransitionTime":"%s","reason":"E2ETest","message":"completed for postRunDelay test"}]}}`, now)
			_, err = framework.PatchResourceStatus(agenticRunAPIResource, testRunName, cfg.Namespace, patch)
			Expect(err).NotTo(HaveOccurred())

			By("Injecting an alert that would create an AgenticRun with the same dedup fingerprint")
			err = framework.InjectAlert(framework.Alert{Labels: postRunDelayLabels})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying no new AgenticRun is created within postRunDelay")
			Consistently(func() int {
				names, err := framework.GetResourcesByLabel(
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestpostrundelay",
					"{.items[*].metadata.name}",
				)
				if err != nil {
					return -1
				}
				if names == "" {
					return 0
				}
				return len(strings.Fields(names))
			}, 30*time.Second, 5*time.Second).Should(Equal(1),
				"no additional AgenticRun should be created within postRunDelay")
		})
	})

	Context("post-run delay expired", func() {
		expiredLabels := map[string]string{
			"alertname": "E2ETestPostRunExpired",
			"severity":  "warning",
			"namespace": "openshift-lightspeed",
		}
		ignoredLabelsExpired := []string{"pod", "instance", "endpoint", "uid"}

		AfterEach(func() {
			framework.ResolveAlert(expiredLabels) //nolint:errcheck
			framework.DeleteResourcesByLabel(     //nolint:errcheck
				agenticRunAPIResource,
				cfg.Namespace,
				"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestpostrunexpired",
			)
		})

		It("should create a new AgenticRun when the previous terminal run is outside postRunDelay", func() {
			dedupFP := agenticrun.StableFingerprint(expiredLabels, ignoredLabelsExpired)
			testRunName := fmt.Sprintf("e2e-postrun-expired-%d", time.Now().UnixNano()%100000)

			By("Creating a fake Failed AgenticRun with matching dedup fingerprint")
			yaml := fmt.Sprintf(`apiVersion: agentic.openshift.io/v1alpha1
kind: AgenticRun
metadata:
  name: %s
  namespace: %s
  labels:
    agentic.openshift.io/source: alertmanager
    agentic.openshift.io/alert-dedup-fingerprint: %s
    agentic.openshift.io/alert-name: e2etestpostrunexpired
    agentic.openshift.io/alert-severity: warning
spec:
  request: "E2E postRunDelay expired test"
  analysis:
    agent: default
  execution:
    agent: default
  verification:
    agent: default`, testRunName, cfg.Namespace, dedupFP)

			_, err := framework.CreateFromYAML(yaml)
			Expect(err).NotTo(HaveOccurred())

			By("Patching status to Failed (Analyzed=False) with old completion time (10m ago)")
			old := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
			patch := fmt.Sprintf(`{"status":{"conditions":[{"type":"Analyzed","status":"False","lastTransitionTime":"%s","reason":"E2ETest","message":"failed for postRunDelay expired test"}]}}`, old)
			_, err = framework.PatchResourceStatus(agenticRunAPIResource, testRunName, cfg.Namespace, patch)
			Expect(err).NotTo(HaveOccurred())

			By("Injecting an alert with the same dedup fingerprint")
			err = framework.InjectAlert(framework.Alert{Labels: expiredLabels})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the adapter to create a new AgenticRun")
			Eventually(func() int {
				names, err := framework.GetResourcesByLabel(
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestpostrunexpired",
					"{.items[*].metadata.name}",
				)
				if err != nil {
					return 0
				}
				if names == "" {
					return 0
				}
				return len(strings.Fields(names))
			}, 2*time.Minute, 10*time.Second).Should(BeNumerically(">=", 2),
				"adapter should create a new AgenticRun after postRunDelay expires")
		})
	})

	Context("409 AlreadyExists handling", func() {
		It("should handle 409 gracefully and continue reconciliation", func() {
			By("Checking adapter logs for 'already exists' messages at Info level (not Error)")
			selector := fmt.Sprintf("app=%s", cfg.DeploymentName)
			pods, err := framework.GetPodNames(cfg.Namespace, selector)
			Expect(err).NotTo(HaveOccurred())
			Expect(pods).NotTo(BeEmpty())

			logs, err := framework.GetLogs(cfg.Namespace, pods[0], 500)
			Expect(err).NotTo(HaveOccurred())

			for _, line := range strings.Split(logs, "\n") {
				if strings.Contains(line, "already exists") {
					Expect(line).NotTo(ContainSubstring(`"level":"ERROR"`),
						"409 AlreadyExists should be logged at Info, not Error")
				}
			}
		})
	})
})

func mergeLabels(base, extra map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}
