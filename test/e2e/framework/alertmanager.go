package framework

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	alertmanagerNamespace = "openshift-monitoring"
	alertmanagerPodLabel  = "app.kubernetes.io/name=alertmanager"
	alertmanagerAPIURL    = "http://localhost:9093/api/v2/alerts"
)

type Alert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations,omitempty"`
	StartsAt    string            `json:"startsAt,omitempty"`
	EndsAt      string            `json:"endsAt,omitempty"`
}

func InjectAlert(alert Alert) error {
	if alert.StartsAt == "" {
		alert.StartsAt = time.Now().UTC().Format(time.RFC3339)
	}
	if alert.EndsAt == "" {
		alert.EndsAt = time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)
	}

	payload, err := json.Marshal([]Alert{alert})
	if err != nil {
		return fmt.Errorf("marshaling alert: %w", err)
	}

	pods, err := getAlertManagerPods()
	if err != nil {
		return err
	}

	for _, pod := range pods {
		_, err = Run("exec", pod, "-n", alertmanagerNamespace, "-c", "alertmanager", "--",
			"curl", "-s", "-X", "POST", alertmanagerAPIURL,
			"-H", "Content-Type: application/json",
			"-d", string(payload),
		)
		if err != nil {
			return fmt.Errorf("injecting alert into pod %s: %w", pod, err)
		}
	}

	return nil
}

func ResolveAlert(labels map[string]string) error {
	now := time.Now().UTC()
	alert := Alert{
		Labels:   labels,
		StartsAt: now.Add(-1 * time.Hour).Format(time.RFC3339),
		EndsAt:   now.Add(-1 * time.Second).Format(time.RFC3339),
	}

	payload, err := json.Marshal([]Alert{alert})
	if err != nil {
		return fmt.Errorf("marshaling alert: %w", err)
	}

	pods, err := getAlertManagerPods()
	if err != nil {
		return err
	}

	for _, pod := range pods {
		_, err = Run("exec", pod, "-n", alertmanagerNamespace, "-c", "alertmanager", "--",
			"curl", "-s", "-X", "POST", alertmanagerAPIURL,
			"-H", "Content-Type: application/json",
			"-d", string(payload),
		)
		if err != nil {
			return fmt.Errorf("resolving alert on pod %s: %w", pod, err)
		}
	}

	return nil
}

func getAlertManagerPods() ([]string, error) {
	pods, err := GetPodNames(alertmanagerNamespace, alertmanagerPodLabel)
	if err != nil {
		return nil, fmt.Errorf("getting alertmanager pods: %w", err)
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no alertmanager pods found in namespace %s", alertmanagerNamespace)
	}
	return pods, nil
}
