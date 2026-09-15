package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	hubv1alpha1 "github.com/openshift/lightspeed-hub/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	alertCredentialSecretLabel              = "hub.openshift.io/alert-credential-secret"
	alertmanagerURLKey                      = "alertmanager-url"
	tokenKey                                = "token"
	caBundleKey                             = "ca-bundle"
	maxConcurrentTargetsEnv                 = "MULTICLUSTER_MAX_CONCURRENT_TARGETS"
	defaultMulticlusterMaxConcurrentTargets = 4
)

func main() {
	multicluster := flag.Bool("multicluster", false, "enable multicluster alert discovery")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	maxConcurrentTargets, err := multiclusterMaxConcurrentTargets(*multicluster)
	if err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}

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

	targets, err := newTargets(ctx, k8sClient, namespace, *multicluster, logger)
	if err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}

	a := adapter.NewWithMaxConcurrentTargets(targets, suspensionClient, cfg, maxConcurrentTargets, logger)
	if err := a.Run(ctx); err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func multiclusterMaxConcurrentTargets(multicluster bool) (int, error) {
	if !multicluster {
		return 1, nil
	}

	value, set := os.LookupEnv(maxConcurrentTargetsEnv)
	if !set {
		return defaultMulticlusterMaxConcurrentTargets, nil
	}

	maxConcurrentTargets, err := strconv.Atoi(value)
	if err != nil || maxConcurrentTargets < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", maxConcurrentTargetsEnv)
	}
	return maxConcurrentTargets, nil
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
	if err := hubv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("registering hub scheme: %w", err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}
	return c, nil
}

// newTargets returns the local target (unless ALERTMANAGER_URL is explicitly
// set to empty) and, when multicluster is enabled, one target for each labeled
// SpokeCluster whose credential Secret can be loaded and parsed. It returns an
// error if the local Alertmanager client cannot be created, SpokeClusters
// cannot be listed, or no targets are configured.
func newTargets(ctx context.Context, k8sClient client.Client, namespace string, multicluster bool, logger *slog.Logger) ([]adapter.Target, error) {
	var targets []adapter.Target

	amURL, amURLSet := os.LookupEnv("ALERTMANAGER_URL")
	if amURLSet && amURL == "" {
		logger.Info("ALERTMANAGER_URL is explicitly empty; skipping local alertmanager target")
	} else {
		local, err := alertmanager.New(alertmanager.Config{
			URL: amURL,
		})
		if err != nil {
			return nil, fmt.Errorf("creating local alertmanager client: %w", err)
		}

		targets = append(targets, adapter.Target{
			Name:      "local",
			Alerts:    local,
			ARClient:  agenticrun.NewClient(k8sClient, namespace, "local", logger),
			Namespace: namespace,
		})
	}

	if !multicluster {
		if len(targets) == 0 {
			return nil, fmt.Errorf("no targets configured: set ALERTMANAGER_URL")
		}
		return targets, nil
	}

	var spokeClusters hubv1alpha1.SpokeClusterList
	if err := k8sClient.List(ctx, &spokeClusters); err != nil {
		return nil, fmt.Errorf("listing spoke clusters: %w", err)
	}

	for _, spokeCluster := range spokeClusters.Items {
		secretName := spokeCluster.GetLabels()[alertCredentialSecretLabel]
		if secretName == "" {
			continue
		}

		targetLogger := logger.With("target", spokeCluster.GetName())
		targetID := agenticrun.SpokeTargetID(spokeCluster.GetName())

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
			ID:        targetID,
			Alerts:    alerts,
			ARClient:  agenticrun.NewClient(k8sClient, namespace, targetID, targetLogger),
			Namespace: namespace,
		})
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets configured: set ALERTMANAGER_URL or configure spoke clusters")
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
