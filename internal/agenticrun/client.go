package agenticrun

import (
	"context"
	"fmt"
	"log/slog"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Client creates and lists AgenticRun resources in the cluster.
type Client struct {
	client.Client
	logger *slog.Logger
}

// NewClient creates a Client that wraps the given controller-runtime client.
func NewClient(c client.Client, logger *slog.Logger) *Client {
	return &Client{Client: c, logger: logger}
}

// ListAgenticRuns returns all AgenticRuns created by this adapter, filtered by the
// source=alertmanager label.
func (c *Client) ListAgenticRuns(ctx context.Context) ([]agenticv1alpha1.AgenticRun, error) {
	var list agenticv1alpha1.AgenticRunList
	if err := c.List(ctx, &list, client.MatchingLabels{LabelSource: sourceValue}); err != nil {
		return nil, fmt.Errorf("agenticrun: listing runs: %w", err)
	}
	return list.Items, nil
}

// CreateAgenticRun creates an AgenticRun resource in the cluster.
// It returns true if the AgenticRun was created, false if it already existed.
func (c *Client) CreateAgenticRun(ctx context.Context, p *agenticv1alpha1.AgenticRun) (bool, error) {
	if err := c.Create(ctx, p); err != nil {
		if apierrors.IsAlreadyExists(err) {
			c.logger.Info("run already exists", "name", p.Name, "namespace", p.Namespace)
			return false, nil
		}
		return false, fmt.Errorf("agenticrun: creating %s/%s: %w", p.Namespace, p.Name, err)
	}
	return true, nil
}
