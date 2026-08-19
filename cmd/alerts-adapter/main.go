package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/adapter"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/config"
)

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

	amClient, err := alertmanager.New(alertmanager.Config{
		URL: os.Getenv("ALERTMANAGER_URL"),
	})
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

	runClient := agenticrun.NewClient(k8sClient, namespace, logger)

	a := adapter.New(amClient, runClient, cfg, logger)
	if err := a.Run(ctx); err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func newClient() (client.Client, error) {
	scheme := runtime.NewScheme()
	if err := agenticv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("registering agentic scheme: %w", err)
	}

	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}
	return c, nil
}
