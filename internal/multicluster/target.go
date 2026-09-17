// Package multicluster configures reconciliation targets from hub SpokeClusters.
package multicluster

import (
	"context"
	"fmt"
	"log/slog"

	hubv1alpha1 "github.com/openshift/lightspeed-hub/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/adapter"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
)

const (
	// CredentialSecretLabel names the SpokeCluster label containing its
	// Alertmanager credential Secret name.
	CredentialSecretLabel = "hub.openshift.io/alert-credential-secret"

	alertmanagerURLKey = "alertmanager-url"
	tokenKey           = "token"
	caBundleKey        = "ca-bundle"
)

// BuildTarget constructs a reconciliation target from a SpokeCluster and its
// credential Secret. It returns false after logging when the SpokeCluster is
// unlabeled or its credentials cannot construct a usable Alertmanager client.
func BuildTarget(ctx context.Context, reader client.Reader, runClient client.Client, namespace string, spokeCluster *hubv1alpha1.SpokeCluster, logger *slog.Logger) (adapter.Target, bool) {
	secretName := spokeCluster.GetLabels()[CredentialSecretLabel]
	if secretName == "" {
		return adapter.Target{}, false
	}

	targetLogger := logger.With("target", spokeCluster.GetName())
	targetID := agenticrun.SpokeTargetID(spokeCluster.GetName())

	var secret corev1.Secret
	key := types.NamespacedName{Name: secretName, Namespace: namespace}
	if err := reader.Get(ctx, key, &secret); err != nil {
		targetLogger.Error("skipping spoke target: loading alert credential secret failed", "error", err)
		return adapter.Target{}, false
	}

	url := string(secret.Data[alertmanagerURLKey])
	if url == "" {
		targetLogger.Error("skipping spoke target: alert credentials unavailable", "error", fmt.Errorf("missing %q data key", alertmanagerURLKey))
		return adapter.Target{}, false
	}
	token := string(secret.Data[tokenKey])
	if token == "" {
		targetLogger.Error("skipping spoke target: alert credentials unavailable", "error", fmt.Errorf("missing %q data key", tokenKey))
		return adapter.Target{}, false
	}
	caBundle := secret.Data[caBundleKey]
	if len(caBundle) == 0 {
		targetLogger.Error("skipping spoke target: alert credentials unavailable", "error", fmt.Errorf("missing %q data key", caBundleKey))
		return adapter.Target{}, false
	}

	alerts, err := alertmanager.New(alertmanager.Config{
		URL:      url,
		CABundle: caBundle,
		CASource: fmt.Sprintf("Secret %s/%s data %q", namespace, secretName, caBundleKey),
		Token:    token,
	})
	if err != nil {
		targetLogger.Error("skipping spoke target: creating alertmanager client failed", "error", err)
		return adapter.Target{}, false
	}

	return adapter.Target{
		Name:      spokeCluster.GetName(),
		ID:        targetID,
		Alerts:    alerts,
		ARClient:  agenticrun.NewClient(runClient, namespace, targetID, targetLogger),
		Namespace: namespace,
	}, true
}
