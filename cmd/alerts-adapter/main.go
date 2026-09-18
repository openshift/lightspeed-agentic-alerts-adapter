package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/go-logr/logr"
	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	hubv1alpha1 "github.com/openshift/lightspeed-hub/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/adapter"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticolsconfig"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/config"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/multicluster"
)

const (
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
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))

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

	targetsSource := newTargetRegistry(targets)
	var managerErr <-chan error
	if *multicluster {
		managerErr, err = startSpokeClusterController(ctx, stop, k8sClient, namespace, targetsSource, logger)
		if err != nil {
			logger.Error("fatal error", "error", err)
			os.Exit(1)
		}
	}

	a := adapter.NewWithMaxConcurrentTargets(targetsSource, suspensionClient, cfg, maxConcurrentTargets, logger)
	if err := a.Run(ctx); err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}
	if managerErr != nil {
		select {
		case err := <-managerErr:
			logger.Error("fatal error", "error", err)
			os.Exit(1)
		default:
		}
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
func newTargets(ctx context.Context, k8sClient client.Client, namespace string, multiCluster bool, logger *slog.Logger) ([]adapter.Target, error) {
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

	if !multiCluster {
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
		target, ok := multicluster.BuildTarget(ctx, k8sClient, k8sClient, namespace, &spokeCluster, logger)
		if !ok {
			continue
		}
		targets = append(targets, target)
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets configured: set ALERTMANAGER_URL or configure spoke clusters")
	}

	return targets, nil
}

// newTargetRegistry separates fixed local targets from dynamic spoke targets.
func newTargetRegistry(targets []adapter.Target) *multicluster.Registry {
	var local, spokes []adapter.Target
	for _, target := range targets {
		if target.ID == "" {
			local = append(local, target)
			continue
		}
		spokes = append(spokes, target)
	}
	return multicluster.NewRegistry(local, spokes)
}

// startSpokeClusterController starts the multicluster SpokeCluster controller.
func startSpokeClusterController(ctx context.Context, stop context.CancelFunc, k8sClient client.Client, namespace string, targets *multicluster.Registry, logger *slog.Logger) (<-chan error, error) {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig for SpokeCluster controller: %w", err)
	}
	manager, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:  k8sClient.Scheme(),
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		return nil, fmt.Errorf("creating SpokeCluster controller manager: %w", err)
	}
	reconciler := &multicluster.Reconciler{
		Client:    manager.GetClient(),
		APIReader: manager.GetAPIReader(),
		RunClient: k8sClient,
		Namespace: namespace,
		Targets:   targets,
		Logger:    logger,
	}
	if err := reconciler.SetupWithManager(manager); err != nil {
		return nil, fmt.Errorf("setting up SpokeCluster controller: %w", err)
	}

	return runControllerManager(ctx, stop, manager.Start), nil
}

// runControllerManager starts a controller manager and stops the poll loop
// when it exits before context cancellation.
func runControllerManager(ctx context.Context, stop context.CancelFunc, start func(context.Context) error) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		err := start(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("SpokeCluster controller manager stopped unexpectedly")
		}
		errCh <- err
		stop()
	}()
	return errCh
}
