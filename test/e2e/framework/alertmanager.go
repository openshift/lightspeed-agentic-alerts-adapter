package framework

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/alertmanager/api/v2/models"
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

func InjectAlert(ctx context.Context, alert Alert) error {
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

	pods, err := getAlertManagerPods(ctx)
	if err != nil {
		return err
	}

	for _, pod := range pods {
		out, err := Run(ctx, "exec", pod, "-n", alertmanagerNamespace, "-c", "alertmanager", "--",
			"curl", "-s", "-w", "\n%{http_code}", "-X", "POST", alertmanagerAPIURL,
			"-H", "Content-Type: application/json",
			"-d", string(payload),
			"--max-time", "30",
			"--connect-timeout", "10",
		)
		if err != nil {
			return fmt.Errorf("injecting alert into pod %s: %w", pod, err)
		}
		if len(out) < 3 {
			return fmt.Errorf("unexpected curl output for pod %s", pod)
		}
		httpCode := out[len(out)-3:]
		if httpCode[0] != '2' {
			return fmt.Errorf("alertmanager returned non-2xx status %s for pod %s", httpCode, pod)
		}
	}

	return nil
}

func ResolveAlert(ctx context.Context, labels map[string]string) error {
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

	pods, err := getAlertManagerPods(ctx)
	if err != nil {
		return err
	}

	for _, pod := range pods {
		out, err := Run(ctx, "exec", pod, "-n", alertmanagerNamespace, "-c", "alertmanager", "--",
			"curl", "-s", "-w", "\n%{http_code}", "-X", "POST", alertmanagerAPIURL,
			"-H", "Content-Type: application/json",
			"-d", string(payload),
			"--max-time", "30",
			"--connect-timeout", "10",
		)
		if err != nil {
			return fmt.Errorf("resolving alert on pod %s: %w", pod, err)
		}
		if len(out) < 3 {
			return fmt.Errorf("unexpected curl output for pod %s", pod)
		}
		httpCode := out[len(out)-3:]
		if httpCode[0] != '2' {
			return fmt.Errorf("alertmanager returned non-2xx status %s for pod %s", httpCode, pod)
		}
	}

	return nil
}

// FindAlertViaExec queries AlertManager directly inside the pod using oc exec,
// bypassing the OAuth-protected route. Returns the first active alert matching
// all supplied labels.
func FindAlertViaExec(ctx context.Context, labels map[string]string) (*models.GettableAlert, error) {
	if len(labels) == 0 {
		return nil, errors.New("test alert labels must not be empty")
	}
	pods, err := getAlertManagerPods(ctx)
	if err != nil {
		return nil, err
	}
	url := alertmanagerAPIURL + "?active=true&silenced=false&inhibited=false"
	out, err := Run(ctx, "exec", pods[0], "-n", alertmanagerNamespace, "-c", "alertmanager", "--",
		"curl", "-s", url, "--max-time", "10", "--connect-timeout", "5")
	if err != nil {
		return nil, fmt.Errorf("querying alerts via exec: %w", err)
	}
	var alerts models.GettableAlerts
	if err := json.Unmarshal([]byte(out), &alerts); err != nil {
		return nil, fmt.Errorf("decoding alerts response: %w", err)
	}
	var matched *models.GettableAlert
	for _, alert := range alerts {
		if alert == nil {
			continue
		}
		matches := true
		for name, value := range labels {
			actual, present := alert.Labels[name]
			if !present || actual != value {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		if alert.Fingerprint == nil || *alert.Fingerprint == "" {
			return nil, errors.New("matching test alert has no fingerprint")
		}
		if matched == nil || *alert.Fingerprint < *matched.Fingerprint {
			matched = alert
		}
	}
	return matched, nil
}

func getAlertManagerPods(ctx context.Context) ([]string, error) {
	pods, err := GetPodNames(ctx, alertmanagerNamespace, alertmanagerPodLabel)
	if err != nil {
		return nil, fmt.Errorf("getting alertmanager pods: %w", err)
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no alertmanager pods found in namespace %s", alertmanagerNamespace)
	}
	return pods, nil
}
