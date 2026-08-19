package framework

import (
	"fmt"
	"os/exec"
	"strings"
)

func Run(args ...string) (string, error) {
	cmd := exec.Command("oc", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("oc %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func IsDeploymentAvailable(namespace, name string) (bool, error) {
	out, err := Run("get", "deployment", name, "-n", namespace,
		"-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
	if err != nil {
		return false, err
	}
	return out == "True", nil
}

func GetPodNames(namespace, labelSelector string) ([]string, error) {
	out, err := Run("get", "pods", "-n", namespace, "-l", labelSelector,
		"-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Fields(out), nil
}

func ArePodsRunning(namespace, labelSelector string) (bool, error) {
	out, err := Run("get", "pods", "-n", namespace, "-l", labelSelector,
		"-o", "jsonpath={.items[*].status.phase}")
	if err != nil {
		return false, err
	}
	if out == "" {
		return false, nil
	}
	for _, phase := range strings.Fields(out) {
		if phase != "Running" {
			return false, nil
		}
	}
	return true, nil
}

func GetLogs(namespace, podName string, tailLines int) (string, error) {
	return Run("logs", podName, "-n", namespace, "--tail", fmt.Sprintf("%d", tailLines))
}

func GetResource(resource, name, namespace, jsonpath string) (string, error) {
	args := []string{"get", resource, name, "-n", namespace}
	if jsonpath != "" {
		args = append(args, "-o", fmt.Sprintf("jsonpath=%s", jsonpath))
	}
	return Run(args...)
}

func GetResourcesByLabel(resource, namespace, labelSelector, jsonpath string) (string, error) {
	args := []string{"get", resource, "-n", namespace, "-l", labelSelector}
	if jsonpath != "" {
		args = append(args, "-o", fmt.Sprintf("jsonpath=%s", jsonpath))
	}
	return Run(args...)
}

func CreateFromYAML(yamlContent string) (string, error) {
	cmd := exec.Command("oc", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yamlContent)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("oc apply: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func DeleteResource(resource, name, namespace string) (string, error) {
	return Run("delete", resource, name, "-n", namespace, "--ignore-not-found")
}

func DeleteResourcesByLabel(resource, namespace, labelSelector string) (string, error) {
	return Run("delete", resource, "-n", namespace, "-l", labelSelector, "--ignore-not-found")
}

func PatchResourceStatus(resource, name, namespace, patch string) (string, error) {
	return Run("patch", resource, name, "-n", namespace, "--type=merge", "--subresource=status", "-p", patch)
}

func GetEvents(namespace string) (string, error) {
	return Run("get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
}
