package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/test/e2e/framework"
)

const agenticRunAPIResource = "agenticruns.agentic.openshift.io"

var cfg framework.Config

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Alerts Adapter E2E Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cfg = framework.LoadConfig()

	By("Checking if AlertManager is available (skip on MicroShift)")
	pods, err := framework.GetPodNames(ctx, "openshift-monitoring", "app.kubernetes.io/name=alertmanager")
	if err != nil || len(pods) == 0 {
		Skip("AlertManager not available - this suite requires the OpenShift Monitoring stack (not available on MicroShift)")
	}

	By("Verifying adapter deployment is available")
	available, err := framework.IsDeploymentAvailable(ctx, cfg.Namespace, cfg.DeploymentName)
	Expect(err).NotTo(HaveOccurred(), "failed to check adapter deployment")
	Expect(available).To(BeTrue(), "adapter deployment is not available")

	By("Verifying adapter pods are running")
	selector := fmt.Sprintf("app=%s", cfg.DeploymentName)
	running, err := framework.ArePodsRunning(ctx, cfg.Namespace, selector)
	Expect(err).NotTo(HaveOccurred(), "failed to check adapter pods")
	Expect(running).To(BeTrue(), "adapter pods are not running")

	By("Verifying operator deployment is available")
	available, err = framework.IsDeploymentAvailable(ctx, cfg.OperatorNamespace, cfg.OperatorDeployment)
	Expect(err).NotTo(HaveOccurred(), "failed to check operator deployment")
	Expect(available).To(BeTrue(), "operator deployment is not available")
})

var _ = ReportAfterSuite("E2E artifact collection and cleanup", func(report Report) {
	// Collect debug artifacts if any test in the suite failed
	// This works correctly even with parallel test execution (-p flag)
	if !report.SuiteSucceeded {
		collectDebugArtifacts()
	}
})

var _ = AfterSuite(func() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	By("Cleaning up E2E AgenticRuns")
	if _, err := framework.DeleteResourcesByLabel(ctx,
		agenticRunAPIResource,
		cfg.Namespace,
		"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestreconciliation",
	); err != nil {
		GinkgoWriter.Printf("Warning: failed to clean up E2E AgenticRuns: %v\n", err)
	}
})

func collectDebugArtifacts() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	GinkgoWriter.Println("=== Collecting debug artifacts ===")

	selector := fmt.Sprintf("app=%s", cfg.DeploymentName)
	pods, err := framework.GetPodNames(ctx, cfg.Namespace, selector)
	if err != nil {
		GinkgoWriter.Printf("Failed to get pod names: %v\n", err)
		return
	}

	for _, pod := range pods {
		GinkgoWriter.Printf("\n--- Logs for pod %s ---\n", pod)
		logs, err := framework.GetLogs(ctx, cfg.Namespace, pod, 200)
		if err != nil {
			GinkgoWriter.Printf("Failed to get logs: %v\n", err)
			continue
		}
		GinkgoWriter.Println(logs)
	}

	GinkgoWriter.Println("\n--- Events ---")
	events, err := framework.GetEvents(ctx, cfg.Namespace)
	if err != nil {
		GinkgoWriter.Printf("Failed to get events: %v\n", err)
		return
	}
	GinkgoWriter.Println(events)

	GinkgoWriter.Println("\n--- Deployment Status ---")
	status, err := framework.GetResource(ctx, "deployment", cfg.DeploymentName, cfg.Namespace, "")
	if err != nil {
		GinkgoWriter.Printf("Failed to get deployment: %v\n", err)
		return
	}
	GinkgoWriter.Println(status)
}
