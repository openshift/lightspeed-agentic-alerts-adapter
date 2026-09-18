package multicluster

import (
	"context"
	"fmt"
	"log/slog"

	hubv1alpha1 "github.com/openshift/lightspeed-hub/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reconciler maintains dynamic spoke targets from SpokeCluster lifecycle events.
type Reconciler struct {
	Client    client.Client
	APIReader client.Reader
	RunClient client.Client
	Namespace string
	Targets   *Registry
	Logger    *slog.Logger
}

// Reconcile adds, replaces, or removes the target represented by req.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var spokeCluster hubv1alpha1.SpokeCluster
	if err := r.Client.Get(ctx, req.NamespacedName, &spokeCluster); err != nil {
		if apierrors.IsNotFound(err) {
			if r.Targets.RemoveSpoke(req.Name) {
				r.Logger.Info("spoke target removed",
					"target", req.Name,
					"reason", "SpokeCluster deleted",
				)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting SpokeCluster %q: %w", req.Name, err)
	}

	if spokeCluster.GetLabels()[CredentialSecretLabel] == "" {
		if r.Targets.RemoveSpoke(spokeCluster.Name) {
			r.Logger.Info("spoke target removed",
				"target", spokeCluster.Name,
				"reason", "credential label removed",
			)
		}
		return ctrl.Result{}, nil
	}

	target, ok := BuildTarget(ctx, r.APIReader, r.RunClient, r.Namespace, &spokeCluster, r.Logger)
	if !ok {
		if r.Targets.RemoveSpoke(spokeCluster.Name) {
			r.Logger.Info("spoke target removed",
				"target", spokeCluster.Name,
				"reason", "credentials unavailable",
			)
		}
		return ctrl.Result{}, nil
	}
	if r.Targets.SetSpoke(target) {
		r.Logger.Info("spoke target discovered", "target", target.Name)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager configures reconciliation for SpokeCluster events.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hubv1alpha1.SpokeCluster{}).
		Complete(r)
}
