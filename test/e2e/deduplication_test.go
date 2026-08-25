package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/test/e2e/framework"
)

var _ = Describe("Deduplication", func() {
	Context("receiver filtering", func() {
		criticalLabels := map[string]string{
			"alertname": "E2ETestReceiverCritical",
			"severity":  "critical",
			"namespace": "openshift-lightspeed",
		}

		AfterEach(func() {
			var errs []error

			if err := framework.ResolveAlert(context.Background(), criticalLabels); err != nil {
				errs = append(errs, fmt.Errorf("resolve critical alert: %w", err))
			}
			// Clean up any AgenticRuns that may have been created if filtering failed
			if _, err := framework.DeleteResourcesByLabel(context.Background(),
				agenticRunAPIResource,
				cfg.Namespace,
				"agentic.openshift.io/alert-name=e2etestreceivercritical",
			); err != nil {
				errs = append(errs, fmt.Errorf("delete receiver test AgenticRuns: %w", err))
			}

			if len(errs) > 0 {
				Fail(fmt.Sprintf("AfterEach cleanup failed: %v", errs))
			}
		})

		It("should not create AgenticRun for alerts routed to a receiver not in allowedReceivers", func() {
			By("Injecting a critical-severity alert (routed to Critical receiver, not in allowedReceivers: [default])")
			err := framework.InjectAlert(context.Background(), framework.Alert{Labels: criticalLabels})
			Expect(err).NotTo(HaveOccurred(), "failed to inject critical-severity alert")

			By("Waiting for multiple poll cycles and verifying no AgenticRun is created")
			Consistently(func() (string, error) {
				return framework.GetResourcesByLabel(context.Background(),
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestreceivercritical",
					"{.items[*].metadata.name}",
				)
			}, 30*time.Second, 5*time.Second).Should(BeEmpty(),
				"no AgenticRun should be created for alerts routed to Critical receiver when allowedReceivers is [default]")
		})
	})

	Context("active run deduplication", func() {
		// Define alert labels that will be used for dedup testing
		alertLabels := map[string]string{
			"alertname": "E2ETestActiveDedup",
			"severity":  "warning",
			"namespace": "openshift-lightspeed",
		}
		// Default ignored labels from adapter configuration
		ignoredLabels := []string{"pod", "instance", "endpoint", "uid"}
		// Compute the dedup fingerprint that the adapter will generate for this alert
		testDedupFP := agenticrun.StableFingerprint(alertLabels, ignoredLabels)

		It("should not create a duplicate AgenticRun when an active run exists for the same dedup fingerprint", func() {
			testRunName := fmt.Sprintf("e2e-active-dedup-test-%d", time.Now().UnixNano()%100000)

			defer func() {
				if err := framework.ResolveAlert(context.Background(), alertLabels); err != nil {
					GinkgoWriter.Printf("Warning: failed to resolve active dedup alert: %v\n", err)
				}
				if _, err := framework.DeleteResource(context.Background(), agenticRunAPIResource, testRunName, cfg.Namespace); err != nil {
					GinkgoWriter.Printf("Warning: failed to delete test AgenticRun: %v\n", err)
				}
			}()

			By("Creating an AgenticRun with a known dedup fingerprint")
			yaml := fmt.Sprintf(`apiVersion: agentic.openshift.io/v1alpha1
kind: AgenticRun
metadata:
  name: %s
  namespace: %s
  labels:
    agentic.openshift.io/source: alertmanager
    agentic.openshift.io/alert-group-id: %s
    agentic.openshift.io/alert-name: e2etestactivededup
    agentic.openshift.io/alert-severity: warning
spec:
  request: "E2E test active dedup"
  analysis:
    agent: default
  execution:
    agent: default
  verification:
    agent: default`, testRunName, cfg.Namespace, testDedupFP)

			_, err := framework.CreateFromYAML(context.Background(), yaml)
			Expect(err).NotTo(HaveOccurred(), "failed to create test AgenticRun")

			By("Injecting an alert with the same dedup fingerprint")
			err = framework.InjectAlert(context.Background(), framework.Alert{Labels: alertLabels})
			Expect(err).NotTo(HaveOccurred(), "failed to inject alert")

			By("Verifying no duplicate AgenticRun is created while the active run exists")
			Consistently(func() int {
				names, err := framework.GetResourcesByLabel(context.Background(),
					agenticRunAPIResource,
					cfg.Namespace,
					fmt.Sprintf("agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-group-id=%s", testDedupFP),
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
				"adapter should not create a duplicate AgenticRun when an active run exists for the same dedup fingerprint")
		})
	})

	Context("ignored labels deduplication", func() {
		baseLabels := map[string]string{
			"alertname": "E2ETestIgnoredLabels",
			"severity":  "warning",
			"namespace": "openshift-lightspeed",
		}

		AfterEach(func() {
			var errs []error

			if err := framework.ResolveAlert(context.Background(), mergeLabels(baseLabels, map[string]string{"pod": "pod-1"})); err != nil {
				errs = append(errs, fmt.Errorf("resolve alert pod-1: %w", err))
			}
			if err := framework.ResolveAlert(context.Background(), mergeLabels(baseLabels, map[string]string{"pod": "pod-2"})); err != nil {
				errs = append(errs, fmt.Errorf("resolve alert pod-2: %w", err))
			}
			if _, err := framework.DeleteResourcesByLabel(context.Background(),
				agenticRunAPIResource,
				cfg.Namespace,
				"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestignoredlabels",
			); err != nil {
				errs = append(errs, fmt.Errorf("delete resources: %w", err))
			}

			if len(errs) > 0 {
				Fail(fmt.Sprintf("AfterEach cleanup failed: %v", errs))
			}
		})

		It("should treat two alerts differing only in an ignored label as the same alert", func() {
			alert1Labels := mergeLabels(baseLabels, map[string]string{"pod": "pod-1"})
			alert2Labels := mergeLabels(baseLabels, map[string]string{"pod": "pod-2"})

			By("Injecting the first alert (pod=pod-1)")
			err := framework.InjectAlert(context.Background(), framework.Alert{Labels: alert1Labels})
			Expect(err).NotTo(HaveOccurred(), "failed to inject first alert with pod=pod-1")

			By("Waiting for the adapter to create an AgenticRun")
			Eventually(func() (string, error) {
				return framework.GetResourcesByLabel(context.Background(),
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestignoredlabels",
					"{.items[0].metadata.name}",
				)
			}, 2*time.Minute, 10*time.Second).ShouldNot(BeEmpty(), "expected AgenticRun to be created for first alert")

			By("Injecting the second alert (pod=pod-2, differs only in ignored label)")
			err = framework.InjectAlert(context.Background(), framework.Alert{Labels: alert2Labels})
			Expect(err).NotTo(HaveOccurred(), "failed to inject second alert with pod=pod-2")

			By("Verifying no additional AgenticRun is created")
			Consistently(func() int {
				names, err := framework.GetResourcesByLabel(context.Background(),
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
			var errs []error

			if err := framework.ResolveAlert(context.Background(), postRunDelayLabels); err != nil {
				errs = append(errs, fmt.Errorf("resolve postRunDelay alert: %w", err))
			}
			if _, err := framework.DeleteResourcesByLabel(context.Background(),
				agenticRunAPIResource,
				cfg.Namespace,
				"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestpostrundelay",
			); err != nil {
				errs = append(errs, fmt.Errorf("delete resources: %w", err))
			}

			if len(errs) > 0 {
				Fail(fmt.Sprintf("AfterEach cleanup failed: %v", errs))
			}
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
    agentic.openshift.io/alert-group-id: %s
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

			_, err := framework.CreateFromYAML(context.Background(), yaml)
			Expect(err).NotTo(HaveOccurred(), "failed to create test AgenticRun for postRunDelay test")

			By("Patching status to Completed (Verified=True) with recent completion time")
			now := time.Now().UTC().Format(time.RFC3339)
			patch := fmt.Sprintf(`{"status":{"conditions":[{"type":"Verified","status":"True","lastTransitionTime":"%s","reason":"E2ETest","message":"completed for postRunDelay test"}]}}`, now)
			_, err = framework.PatchResourceStatus(context.Background(), agenticRunAPIResource, testRunName, cfg.Namespace, patch)
			Expect(err).NotTo(HaveOccurred(), "failed to patch AgenticRun status to Completed")

			By("Injecting an alert that would create an AgenticRun with the same dedup fingerprint")
			err = framework.InjectAlert(context.Background(), framework.Alert{Labels: postRunDelayLabels})
			Expect(err).NotTo(HaveOccurred(), "failed to inject alert for postRunDelay test")

			By("Verifying no new AgenticRun is created within postRunDelay")
			Consistently(func() int {
				names, err := framework.GetResourcesByLabel(context.Background(),
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
			var errs []error

			if err := framework.ResolveAlert(context.Background(), expiredLabels); err != nil {
				errs = append(errs, fmt.Errorf("resolve expired alert: %w", err))
			}
			if _, err := framework.DeleteResourcesByLabel(context.Background(),
				agenticRunAPIResource,
				cfg.Namespace,
				"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestpostrunexpired",
			); err != nil {
				errs = append(errs, fmt.Errorf("delete resources: %w", err))
			}

			if len(errs) > 0 {
				Fail(fmt.Sprintf("AfterEach cleanup failed: %v", errs))
			}
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
    agentic.openshift.io/alert-group-id: %s
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

			_, err := framework.CreateFromYAML(context.Background(), yaml)
			Expect(err).NotTo(HaveOccurred(), "failed to create test AgenticRun for postRunDelay expired test")

			By("Patching status to Failed (Analyzed=False) with old completion time (10m ago)")
			old := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
			patch := fmt.Sprintf(`{"status":{"conditions":[{"type":"Analyzed","status":"False","lastTransitionTime":"%s","reason":"E2ETest","message":"failed for postRunDelay expired test"}]}}`, old)
			_, err = framework.PatchResourceStatus(context.Background(), agenticRunAPIResource, testRunName, cfg.Namespace, patch)
			Expect(err).NotTo(HaveOccurred(), "failed to patch AgenticRun status to Failed with old timestamp")

			By("Injecting an alert with the same dedup fingerprint")
			err = framework.InjectAlert(context.Background(), framework.Alert{Labels: expiredLabels})
			Expect(err).NotTo(HaveOccurred(), "failed to inject alert for postRunDelay expired test")

			By("Waiting for the adapter to create a new AgenticRun")
			Eventually(func() int {
				names, err := framework.GetResourcesByLabel(context.Background(),
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
		alertLabels := map[string]string{
			"alertname": "E2ETest409Handling",
			"severity":  "warning",
			"namespace": "openshift-lightspeed",
		}
		fixedTimestamp := "2026-08-20T10:00:00Z"

		AfterEach(func() {
			var errs []error

			if err := framework.ResolveAlert(context.Background(), alertLabels); err != nil {
				errs = append(errs, fmt.Errorf("resolve 409 test alert: %w", err))
			}
			if _, err := framework.DeleteResourcesByLabel(context.Background(),
				agenticRunAPIResource,
				cfg.Namespace,
				"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etest409handling",
			); err != nil {
				errs = append(errs, fmt.Errorf("delete 409 test resources: %w", err))
			}

			if len(errs) > 0 {
				Fail(fmt.Sprintf("AfterEach cleanup failed: %v", errs))
			}
		})

		It("should log 409 AlreadyExists at Info level and continue reconciliation", func() {
			// Replicate the adapter's name generation: lowercase(alertname)-namespace-sha256(startsAt)[:8]
			t, err := time.Parse(time.RFC3339, fixedTimestamp)
			Expect(err).NotTo(HaveOccurred(), "failed to parse fixed timestamp")
			h := sha256.Sum256([]byte(t.UTC().Format(time.RFC3339)))
			startsAtHash := hex.EncodeToString(h[:])[:8]
			expectedName := fmt.Sprintf("e2etest409handling-%s-%s", "openshift-lightspeed", startsAtHash)

			ignoredLabels := []string{"pod", "instance", "endpoint", "uid"}
			dedupFP := agenticrun.StableFingerprint(alertLabels, ignoredLabels)

			By("Pre-creating an AgenticRun with the exact name the adapter will generate")
			yaml := fmt.Sprintf(`apiVersion: agentic.openshift.io/v1alpha1
kind: AgenticRun
metadata:
  name: %s
  namespace: %s
  labels:
    agentic.openshift.io/source: alertmanager
    agentic.openshift.io/alert-name: e2etest409handling
    agentic.openshift.io/alert-severity: warning
    agentic.openshift.io/alert-group-id: %s
spec:
  request: "E2E test 409 handling"
  analysis:
    agent: default
  execution:
    agent: default
  verification:
    agent: default`, expectedName, cfg.Namespace, dedupFP)

			_, err = framework.CreateFromYAML(context.Background(), yaml)
			Expect(err).NotTo(HaveOccurred(), "failed to pre-create AgenticRun")

			By("Marking the pre-created AgenticRun as Completed so dedup doesn't skip the alert")
			completedPatch := fmt.Sprintf(`{"status":{"conditions":[{"type":"Verified","status":"True","lastTransitionTime":"%s","reason":"Completed","message":"E2E test"}]}}`,
				time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339))
			_, err = framework.PatchResourceStatus(context.Background(),
				agenticRunAPIResource, expectedName, cfg.Namespace, completedPatch)
			Expect(err).NotTo(HaveOccurred(), "failed to patch AgenticRun status to Completed")

			By("Injecting the alert so the adapter tries to create the same-named AgenticRun")
			err = framework.InjectAlert(context.Background(), framework.Alert{
				Labels:   alertLabels,
				StartsAt: fixedTimestamp,
			})
			Expect(err).NotTo(HaveOccurred(), "failed to inject alert")

			By("Waiting for 'already exists' message in adapter logs")
			selector := fmt.Sprintf("app=%s", cfg.DeploymentName)
			Eventually(func() bool {
				pods, err := framework.GetPodNames(context.Background(), cfg.Namespace, selector)
				if err != nil || len(pods) == 0 {
					return false
				}
				logs, err := framework.GetLogs(context.Background(), cfg.Namespace, pods[0], 500)
				if err != nil {
					return false
				}
				return strings.Contains(logs, "already exists")
			}, 2*time.Minute, 10*time.Second).Should(BeTrue(),
				"expected 'already exists' message in logs after 409")

			By("Verifying 409 is logged at Info level, not Error")
			pods, err := framework.GetPodNames(context.Background(), cfg.Namespace, selector)
			Expect(err).NotTo(HaveOccurred(), "failed to get pod names")
			Expect(pods).NotTo(BeEmpty(), "no adapter pods found")

			logs, err := framework.GetLogs(context.Background(), cfg.Namespace, pods[0], 500)
			Expect(err).NotTo(HaveOccurred(), "failed to get logs")

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
