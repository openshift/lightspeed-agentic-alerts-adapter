package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/adapter"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticolsconfig"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/config"
)

const (
	alertCredentialSecretLabel = "hub.openshift.io/alert-credential-secret"
	alertmanagerURLKey         = "alertmanager-url"
	tokenKey                   = "token"
	caBundleKey                = "ca-bundle"
)

var spokeClusterListGVK = schema.GroupVersionKind{
	Group: "hub.openshift.io", Version: "v1alpha1", Kind: "SpokeClusterList",
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := config.LoadFromFile(config.DefaultConfigPath, logger)
	if err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}

	k8sClient, err := newClient()
	if err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = agenticrun.RunNamespace
	}

	suspensionClient := agenticolsconfig.NewClient(k8sClient)

	targets, err := newTargets(ctx, k8sClient, namespace, logger)
	if err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}

	a := adapter.New(targets, suspensionClient, cfg, logger)
	if err := a.Run(ctx); err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func newClient() (client.Client, error) {
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	return newClientForConfig(cfg)
}

// newClientForConfig returns a controller-runtime client configured for core
// Secrets and AgenticRuns, or an error if the client cannot be created.
func newClientForConfig(cfg *rest.Config) (client.Client, error) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("registering core scheme: %w", err)
	}
	if err := agenticv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("registering agentic scheme: %w", err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}
	return c, nil
}

// newTargets returns the local target followed by one target for each labeled
// SpokeCluster whose credential Secret can be loaded and parsed. It returns an
// error if the local Alertmanager client cannot be created or SpokeClusters
// cannot be listed for a reason other than the SpokeCluster CRD being absent.
func newTargets(ctx context.Context, k8sClient client.Client, namespace string, logger *slog.Logger) ([]adapter.Target, error) {
	local, err := alertmanager.New(alertmanager.Config{
		URL: os.Getenv("ALERTMANAGER_URL"),
	})
	if err != nil {
		return nil, fmt.Errorf("creating local alertmanager client: %w", err)
	}

	targets := []adapter.Target{{
		Name:      "local",
		Alerts:    local,
		ARClient:  agenticrun.NewClient(k8sClient, namespace, "local", logger),
		Namespace: namespace,
	}}

	var spokeClusters unstructured.UnstructuredList
	spokeClusters.SetGroupVersionKind(spokeClusterListGVK)
	if err := k8sClient.List(ctx, &spokeClusters); err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("SpokeCluster CRD is not installed; skipping spoke targets")
			return targets, nil
		}
		return nil, fmt.Errorf("listing spoke clusters: %w", err)
	}

	for _, spokeCluster := range spokeClusters.Items {
		secretName := spokeCluster.GetLabels()[alertCredentialSecretLabel]
		if secretName == "" {
			continue
		}

		targetLogger := logger.With("target", spokeCluster.GetName())

		var secret corev1.Secret
		key := types.NamespacedName{Name: secretName, Namespace: namespace}
		if err := k8sClient.Get(ctx, key, &secret); err != nil {
			targetLogger.Error("skipping spoke target: loading alert credential secret failed", "error", err)
			continue
		}

		url, token, caBundle, err := alertCredentialsFromSecret(&secret)
		if err != nil {
			targetLogger.Error("skipping spoke target: alert credentials unavailable", "error", err)
			continue
		}

		alerts, err := alertmanager.New(alertmanager.Config{
			URL:      url,
			CABundle: caBundle,
			CASource: fmt.Sprintf("Secret %s/%s data %q", namespace, secretName, caBundleKey),
			Token:    token,
		})
		if err != nil {
			targetLogger.Error("skipping spoke target: creating alertmanager client failed", "error", err)
			continue
		}

		targets = append(targets, adapter.Target{
			Name:      spokeCluster.GetName(),
			Alerts:    alerts,
			ARClient:  agenticrun.NewClient(k8sClient, namespace, spokeCluster.GetName(), targetLogger),
			Namespace: namespace,
		})
	}

	return targets, nil
}

// alertCredentialsFromSecret returns the Alertmanager URL, bearer token, and
// CA bundle from secret, or an error when a required data value is absent.
func alertCredentialsFromSecret(secret *corev1.Secret) (string, string, []byte, error) {
	url := string(secret.Data[alertmanagerURLKey])
	if url == "" {
		return "", "", nil, fmt.Errorf("missing %q data key", alertmanagerURLKey)
	}
	token := string(secret.Data[tokenKey])
	if token == "" {
		return "", "", nil, fmt.Errorf("missing %q data key", tokenKey)
	}
	caBundle := secret.Data[caBundleKey]
	if len(caBundle) == 0 {
		return "", "", nil, fmt.Errorf("missing %q data key", caBundleKey)
	}
	return url, token, caBundle, nil
}
