package agenticrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	LabelSourceTarget = "agentic.openshift.io/alert-source-target"
	localTargetID     = "local"
	spokeTargetPrefix = "spoke-"
	targetHashLen     = 12
	targetIDMaxLen    = 63
)

// SpokeTargetID returns a label-safe identity for a SpokeCluster target.
// It prefixes short names to avoid colliding with the reserved local target ID.
// Long names are truncated and suffixed with a hash of the full name.
func SpokeTargetID(name string) string {
	if len(spokeTargetPrefix)+len(name) <= targetIDMaxLen {
		return spokeTargetPrefix + name
	}

	hash := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(hash[:])[:targetHashLen]
	nameMaxLen := targetIDMaxLen - len(spokeTargetPrefix) - len(suffix) - 1
	return spokeTargetPrefix + name[:nameMaxLen] + "-" + suffix
}

// Client creates and lists AgenticRun resources in the cluster.
type Client struct {
	client.Client
	namespace string
	target    string
	logger    *slog.Logger
}

// NewClient creates a Client that wraps the given controller-runtime client.
func NewClient(c client.Client, namespace, target string, logger *slog.Logger) *Client {
	return &Client{Client: c, namespace: namespace, target: target, logger: logger}
}

// ListAgenticRuns returns all AgenticRuns created by this adapter for this
// target. The local target also includes legacy runs without a target label.
func (c *Client) ListAgenticRuns(ctx context.Context) ([]agenticv1alpha1.AgenticRun, error) {
	var list agenticv1alpha1.AgenticRunList
	if err := c.List(ctx, &list, client.InNamespace(c.namespace), client.MatchingLabels{
		LabelSource: sourceValue,
	}); err != nil {
		return nil, fmt.Errorf("agenticrun: listing runs: %w", err)
	}

	runs := make([]agenticv1alpha1.AgenticRun, 0, len(list.Items))
	for i := range list.Items {
		target, labeled := list.Items[i].Labels[LabelSourceTarget]
		if target == c.target || (c.target == localTargetID && !labeled) {
			runs = append(runs, list.Items[i])
		}
	}
	return runs, nil
}

// CreateAgenticRun creates an AgenticRun resource in the cluster.
// It returns true if the AgenticRun was created, false if it already existed.
func (c *Client) CreateAgenticRun(ctx context.Context, p *agenticv1alpha1.AgenticRun) (bool, error) {
	if p.Labels == nil {
		p.Labels = map[string]string{}
	}
	p.Labels[LabelSourceTarget] = c.target
	if err := c.Create(ctx, p); err != nil {
		if apierrors.IsAlreadyExists(err) {
			c.logger.Info("run already exists", "name", p.Name, "namespace", p.Namespace)
			return false, nil
		}
		return false, fmt.Errorf("agenticrun: creating %s/%s: %w", p.Namespace, p.Name, err)
	}
	return true, nil
}
