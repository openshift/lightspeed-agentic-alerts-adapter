package agenticolsconfig

import (
	"context"
	"fmt"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigName is the name of the cluster-scoped AgenticOLSConfig singleton.
const ConfigName = "cluster"

// Client reads AgenticOLSConfig resources from the cluster.
type Client struct {
	client.Client
}

// NewClient creates a Client that wraps the given controller-runtime client.
func NewClient(c client.Client) *Client {
	return &Client{Client: c}
}

// Suspended returns true when AgenticOLSConfig.spec.suspended is true.
// Missing AgenticOLSConfig CRD or missing singleton object means not suspended.
func (c *Client) Suspended(ctx context.Context) (bool, error) {
	var cfg agenticv1alpha1.AgenticOLSConfig

	if err := c.Get(ctx, types.NamespacedName{Name: ConfigName}, &cfg); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return false, nil
		}
		return false, fmt.Errorf("agenticolsconfig: getting %s: %w", ConfigName, err)
	}

	return cfg.Spec.Suspended, nil
}
