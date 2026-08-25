package framework

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// redactedArgs returns a copy of args with sensitive data replaced by <redacted>
func redactedArgs(args []string) []string {
	redacted := make([]string, len(args))
	copy(redacted, args)
	for i := 0; i+1 < len(redacted); i++ {
		// Redact data after flags that carry sensitive payloads
		if redacted[i] == "-d" || redacted[i] == "--data" || redacted[i] == "-p" {
			redacted[i+1] = "<redacted>"
		}
	}
	return redacted
}

func Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "oc", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("oc %s: %w\n%s", strings.Join(redactedArgs(args), " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func IsDeploymentAvailable(ctx context.Context, namespace, name string) (bool, error) {
	out, err := Run(ctx, "get", "deployment", name, "-n", namespace,
		"-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
	if err != nil {
		return false, err
	}
	return out == "True", nil
}

func GetPodNames(ctx context.Context, namespace, labelSelector string) ([]string, error) {
	out, err := Run(ctx, "get", "pods", "-n", namespace, "-l", labelSelector,
		"-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Fields(out), nil
}

func ArePodsRunning(ctx context.Context, namespace, labelSelector string) (bool, error) {
	out, err := Run(ctx, "get", "pods", "-n", namespace, "-l", labelSelector,
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

func GetLogs(ctx context.Context, namespace, podName string, tailLines int) (string, error) {
	return Run(ctx, "logs", podName, "-n", namespace, "--tail", fmt.Sprintf("%d", tailLines))
}

func GetResource(ctx context.Context, resource, name, namespace, jsonpath string) (string, error) {
	args := []string{"get", resource, name, "-n", namespace}
	if jsonpath != "" {
		args = append(args, "-o", fmt.Sprintf("jsonpath=%s", jsonpath))
	}
	return Run(ctx, args...)
}

func GetResourcesByLabel(ctx context.Context, resource, namespace, labelSelector, jsonpath string) (string, error) {
	args := []string{"get", resource, "-n", namespace, "-l", labelSelector}
	if jsonpath != "" {
		args = append(args, "-o", fmt.Sprintf("jsonpath=%s", jsonpath))
	}
	return Run(ctx, args...)
}

func CreateFromYAML(ctx context.Context, yamlContent string) (string, error) {
	cmd := exec.CommandContext(ctx, "oc", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yamlContent)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("oc apply: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func DeleteResource(ctx context.Context, resource, name, namespace string) (string, error) {
	return Run(ctx, "delete", resource, name, "-n", namespace, "--ignore-not-found")
}

func DeleteResourcesByLabel(ctx context.Context, resource, namespace, labelSelector string) (string, error) {
	return Run(ctx, "delete", resource, "-n", namespace, "-l", labelSelector, "--ignore-not-found")
}

func PatchResourceStatus(ctx context.Context, resource, name, namespace, patch string) (string, error) {
	return Run(ctx, "patch", resource, name, "-n", namespace, "--type=merge", "--subresource=status", "-p", patch)
}

func GetEvents(ctx context.Context, namespace string) (string, error) {
	return Run(ctx, "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
}
