package framework

import "fmt"

// PrometheusRuleManifest generates a synthetic alert through cluster Prometheus.
func PrometheusRuleManifest(name, namespace, expression string) string {
	return fmt.Sprintf(`apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: %[1]q
  namespace: %[4]q
  labels:
    prometheus: k8s
    role: alert-rules
spec:
  groups:
    - name: %[1]q
      interval: 5s
      rules:
        - alert: %[1]q
          expr: %[3]q
          for: 0s
          labels:
            severity: critical
            namespace: %[2]q
            e2e_test_id: %[1]q
          annotations:
            summary: Synthetic alert for investigation API E2E tests
`, name, namespace, expression, alertmanagerNamespace)
}
