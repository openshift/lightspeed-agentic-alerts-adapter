package e2e_test

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/test/e2e/framework"
)

var _ = Describe("Reconciliation", func() {
	It("should have the adapter deployment available and pods running", func() {
		available, err := framework.IsDeploymentAvailable(cfg.Namespace, cfg.DeploymentName)
		Expect(err).NotTo(HaveOccurred())
		Expect(available).To(BeTrue())

		selector := fmt.Sprintf("app=%s", cfg.DeploymentName)
		running, err := framework.ArePodsRunning(cfg.Namespace, selector)
		Expect(err).NotTo(HaveOccurred())
		Expect(running).To(BeTrue())
	})

	Context("happy path", func() {
		alertLabels := map[string]string{
			"alertname": "E2ETestReconciliation",
			"severity":  "warning",
			"namespace": "openshift-lightspeed",
		}

		AfterEach(func() {
			framework.ResolveAlert(alertLabels) //nolint:errcheck
			framework.DeleteResourcesByLabel(   //nolint:errcheck
				agenticRunAPIResource,
				cfg.Namespace,
				"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestreconciliation",
			)
		})

		It("should create an AgenticRun for a firing alert routed to the configured receiver", func() {
			By("Injecting a warning-severity alert (routed to Default receiver)")
			err := framework.InjectAlert(framework.Alert{Labels: alertLabels})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the adapter to create an AgenticRun")
			Eventually(func() (string, error) {
				return framework.GetResourcesByLabel(
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestreconciliation",
					"{.items[*].metadata.name}",
				)
			}, 5*time.Minute, 10*time.Second).ShouldNot(BeEmpty(),
				"expected an AgenticRun for the injected alert")
		})

		It("should set alert-fingerprint and alert-dedup-fingerprint labels", func() {
			By("Injecting a warning-severity alert")
			err := framework.InjectAlert(framework.Alert{Labels: alertLabels})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for an AgenticRun with both fingerprint labels set")
			Eventually(func() bool {
				fingerprints, err := framework.GetResourcesByLabel(
					agenticRunAPIResource,
					cfg.Namespace,
					"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestreconciliation",
					"{range .items[*]}{.metadata.labels.agentic\\.openshift\\.io/alert-fingerprint},{.metadata.labels.agentic\\.openshift\\.io/alert-dedup-fingerprint}{\"\\n\"}{end}",
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
					if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
						return false
					}
					return true
				}
				return false
			}, 5*time.Minute, 10*time.Second).Should(BeTrue(),
				"AgenticRun should have both fingerprint labels set")
		})
	})
})
