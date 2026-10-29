package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	hubv1alpha1 "github.com/openshift/lightspeed-hub/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
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

	if err := run(*multicluster, logger); err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run(multicluster bool, logger *slog.Logger) error {
	maxConcurrentTargets, err := multiclusterMaxConcurrentTargets(multicluster)
	if err != nil {
		return err
	}

	httpPort := os.Getenv("HTTP_PORT")
	if httpPort == "" {
		httpPort = "8080"
	}
	port, err := strconv.Atoi(httpPort)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("HTTP_PORT must be an integer between 1 and 65535")
	}

	tlsCertFile := os.Getenv("TLS_CERT_FILE")
	tlsKeyFile := os.Getenv("TLS_KEY_FILE")
	if tlsCertFile == "" || tlsKeyFile == "" {
		return errors.New("TLS_CERT_FILE and TLS_KEY_FILE must be set")
	}
	certificateWatcher, err := certwatcher.New(tlsCertFile, tlsKeyFile)
	if err != nil {
		return fmt.Errorf("initializing TLS certificate watcher: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := config.LoadFromFile(config.DefaultConfigPath, logger)
	if err != nil {
		return err
	}

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("loading kubeconfig: %w", err)
	}
	k8sClient, err := newClientForConfig(restConfig)
	if err != nil {
		return err
	}

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = agenticrun.RunNamespace
	}

	localTarget := ""
	if multicluster {
		localTarget = "local"
	}
	access, err := adapter.NewAgenticRunInvestigationRequest(
		restConfig, k8sClient, namespace, localTarget, logger,
	)
	if err != nil {
		return fmt.Errorf("initializing authentication client: %w", err)
	}

	suspensionClient := agenticolsconfig.NewClient(k8sClient)

	targets, err := newTargets(ctx, k8sClient, namespace, multicluster, logger)
	if err != nil {
		return err
	}

	targetsSource := newTargetRegistry(targets)
	var managerErr <-chan error
	if multicluster {
		managerErr, err = startSpokeClusterController(ctx, stop, k8sClient, namespace, targetsSource, logger)
		if err != nil {
			return err
		}
	}

	var localInvestigationTarget adapter.Target
	for _, target := range targets {
		if target.Name == "local" && target.ID == "" {
			localInvestigationTarget = target
			break
		}
	}
	investigationService := adapter.NewInvestigationService(
		localInvestigationTarget, suspensionClient, cfg, logger,
	)
	alertsAdapter := adapter.NewWithMaxConcurrentTargets(targetsSource, suspensionClient, cfg, maxConcurrentTargets, logger)
	server := adapter.NewInvestigationServer(
		net.JoinHostPort("", strconv.Itoa(port)),
		adapter.AuthMiddleware(access, adapter.NewInvestigationHandler(investigationService)),
	)
	server.TLSConfig.GetCertificate = certificateWatcher.GetCertificate

	watcherErr := make(chan error, 1)
	go func() {
		err := certificateWatcher.Start(ctx)
		watcherErr <- err
		if err != nil {
			stop()
		}
	}()

	serverErr := startServer(ctx, stop, server, alertsAdapter.Run)
	stop()
	if err := errors.Join(serverErr, <-watcherErr); err != nil {
		return err
	}
	if managerErr != nil {
		select {
		case err := <-managerErr:
			return err
		default:
		}
	}
	return nil
}

func startServer(
	ctx context.Context,
	stop context.CancelFunc,
	server *http.Server,
	run func(context.Context) error,
) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("starting HTTP listener: %w", err)
	}
	server.BaseContext = func(net.Listener) context.Context {
		// without cancel: stop() runs before Shutdown and must not cut in-flight requests
		return context.WithoutCancel(ctx)
	}
	serverErr := make(chan error, 1)
	go func() {
		err := server.ServeTLS(listener, "", "")
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErr <- err
		if err != nil {
			stop()
		}
	}()

	runErr := run(ctx)
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		shutdownErr = errors.Join(shutdownErr, server.Close())
	}
	return errors.Join(runErr, shutdownErr, <-serverErr)
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
// cannot be listed.
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

		localTarget := ""
		if multiCluster {
			localTarget = "local"
		}
		targets = append(targets, adapter.Target{
			Name:      "local",
			Alerts:    local,
			ARClient:  agenticrun.NewClient(k8sClient, namespace, localTarget, logger),
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
