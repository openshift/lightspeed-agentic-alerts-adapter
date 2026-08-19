package e2e_test

import (
	"fmt"
	"testing"

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
	cfg = framework.LoadConfig()

	By("Verifying adapter deployment is available")
	available, err := framework.IsDeploymentAvailable(cfg.Namespace, cfg.DeploymentName)
	Expect(err).NotTo(HaveOccurred(), "failed to check adapter deployment")
	Expect(available).To(BeTrue(), "adapter deployment is not available")

	By("Verifying adapter pods are running")
	selector := fmt.Sprintf("app=%s", cfg.DeploymentName)
	running, err := framework.ArePodsRunning(cfg.Namespace, selector)
	Expect(err).NotTo(HaveOccurred(), "failed to check adapter pods")
	Expect(running).To(BeTrue(), "adapter pods are not running")

	By("Verifying operator deployment is available")
	available, err = framework.IsDeploymentAvailable(cfg.OperatorNamespace, cfg.OperatorDeployment)
	Expect(err).NotTo(HaveOccurred(), "failed to check operator deployment")
	Expect(available).To(BeTrue(), "operator deployment is not available")
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		collectDebugArtifacts()
	}

	By("Cleaning up E2E AgenticRuns")
	framework.DeleteResourcesByLabel( //nolint:errcheck
		agenticRunAPIResource,
		cfg.Namespace,
		"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=e2etestreconciliation",
	)
})

func collectDebugArtifacts() {
	GinkgoWriter.Println("=== Collecting debug artifacts ===")

	selector := fmt.Sprintf("app=%s", cfg.DeploymentName)
	pods, err := framework.GetPodNames(cfg.Namespace, selector)
	if err != nil {
		GinkgoWriter.Printf("Failed to get pod names: %v\n", err)
		return
	}

	for _, pod := range pods {
		GinkgoWriter.Printf("\n--- Logs for pod %s ---\n", pod)
		logs, err := framework.GetLogs(cfg.Namespace, pod, 200)
		if err != nil {
			GinkgoWriter.Printf("Failed to get logs: %v\n", err)
			continue
		}
		GinkgoWriter.Println(logs)
	}

	GinkgoWriter.Println("\n--- Events ---")
	events, err := framework.GetEvents(cfg.Namespace)
	if err != nil {
		GinkgoWriter.Printf("Failed to get events: %v\n", err)
		return
	}
	GinkgoWriter.Println(events)

	GinkgoWriter.Println("\n--- Deployment Status ---")
	status, err := framework.GetResource("deployment", cfg.DeploymentName, cfg.Namespace, "")
	if err != nil {
		GinkgoWriter.Printf("Failed to get deployment: %v\n", err)
		return
	}
	GinkgoWriter.Println(status)
}
