package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/test/e2e/framework"
)

var _ = Describe("Reconciliation", func() {
	It("should have the adapter deployment available and pods running", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		available, err := framework.IsDeploymentAvailable(ctx, cfg.Namespace, cfg.DeploymentName)
		Expect(err).NotTo(HaveOccurred(), "failed to check adapter deployment availability")
		Expect(available).To(BeTrue(), "adapter deployment is not available")

		selector := fmt.Sprintf("app=%s", cfg.DeploymentName)
		running, err := framework.ArePodsRunning(ctx, cfg.Namespace, selector)
		Expect(err).NotTo(HaveOccurred(), "failed to check adapter pod status")
		Expect(running).To(BeTrue(), "adapter pods are not running")
	})

	Context("happy path", func() {
		alertLabels := map[string]string{
			"alertname": "E2ETestReconciliation",
			"severity":  "warning",
			"namespace": "openshift-lightspeed",
		}

		AfterEach(func() {
			var errs []error

			if err := framework.ResolveAlert(context.Background(), alertLabels); err != nil {
				errs = append(errs, fmt.Errorf("resolve alert: %w", err))
			}
			if _, err := framework.DeleteResourcesByLabel(context.Background(),
				agenticRunAPIResource,
				cfg.Namespace,
				"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestreconciliation",
			); err != nil {
				errs = append(errs, fmt.Errorf("delete resources: %w", err))
			}

			if len(errs) > 0 {
				Fail(fmt.Sprintf("AfterEach cleanup failed: %v", errs))
			}
		})

		It("should create an AgenticRun for a firing alert routed to the configured receiver", func() {
			By("Injecting a warning-severity alert (routed to Default receiver)")
			err := framework.InjectAlert(context.Background(), framework.Alert{Labels: alertLabels})
			Expect(err).NotTo(HaveOccurred(), "failed to inject warning alert")

			By("Waiting for the adapter to create an AgenticRun")
			Eventually(func() (string, error) {
				return framework.GetResourcesByLabel(context.Background(),
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestreconciliation",
					"{.items[*].metadata.name}",
				)
			}, 5*time.Minute, 10*time.Second).ShouldNot(BeEmpty(),
				"expected an AgenticRun for the injected alert")
		})

		It("should set alert-fingerprint and alert-group-id labels", func() {
			ignoredLabels := []string{"pod", "instance", "endpoint", "uid"}
			expectedDedupFP := agenticrun.StableFingerprint(alertLabels, ignoredLabels)

			By("Injecting a warning-severity alert")
			err := framework.InjectAlert(context.Background(), framework.Alert{Labels: alertLabels})
			Expect(err).NotTo(HaveOccurred(), "failed to inject alert for fingerprint test")

			By("Waiting for an AgenticRun with both fingerprint labels set to correct values")
			Eventually(func() bool {
				fingerprints, err := framework.GetResourcesByLabel(context.Background(),
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestreconciliation",
					"{range .items[*]}{.metadata.labels.agentic\\.openshift\\.io/alert-fingerprint},{.metadata.labels.agentic\\.openshift\\.io/alert-group-id}{\"\\n\"}{end}",
				)
				if err != nil {
					return false
				}
				for _, line := range strings.Split(fingerprints, "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					parts := strings.SplitN(line, ",", 2)
					if len(parts) != 2 {
						continue
					}
					alertFP := parts[0]
					dedupFP := parts[1]
					// alert-fingerprint must be present and non-empty (AlertManager-assigned)
					// alert-group-id must match the expected computed value
					if alertFP != "" && dedupFP == expectedDedupFP {
						return true
					}
				}
				return false
			}, 5*time.Minute, 10*time.Second).Should(BeTrue(),
				fmt.Sprintf("AgenticRun should have alert-fingerprint set and alert-group-id=%s", expectedDedupFP))
		})
	})
})
