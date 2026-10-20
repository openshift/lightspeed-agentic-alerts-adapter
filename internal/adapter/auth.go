package adapter

import (
	"context"
	"errors"
	"log/slog"
	"net/url"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	authenticationclient "k8s.io/client-go/kubernetes/typed/authentication/v1"
	authorizationclient "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
)

var (
	errInvestigationUnauthorized = errors.New("unauthorized")
	errInvestigationForbidden    = errors.New("forbidden")
	errInternal                  = errors.New("internal error")
)

// AgenticRunCreator creates runs using an authorized caller's identity.
type AgenticRunCreator interface {
	CreateAgenticRun(context.Context, *agenticv1alpha1.AgenticRun) (bool, error)
}

// AgenticRunRequest authenticates and authorizes a request before internal reads.
type AgenticRunRequest interface {
	Authorize(context.Context, string) (AgenticRunCreator, error)
}

// AgenticRunInvestigationRequest keeps ServiceAccount and caller operations separate.
type AgenticRunInvestigationRequest struct {
	reviews                  authenticationclient.TokenReviewInterface
	selfSubjectAccessReviews func(*rest.Config) (authorizationclient.SelfSubjectAccessReviewInterface, error)
	anonymous                *rest.Config
	shared                   client.Client
	namespace                string
	target                   string
	logger                   *slog.Logger
}

func NewAgenticRunInvestigationRequest(
	cfg *rest.Config, shared client.Client, namespace, target string, logger *slog.Logger,
) (*AgenticRunInvestigationRequest, error) {
	if err := validateKubernetesEndpoint(cfg); err != nil {
		return nil, err
	}
	auth, err := authenticationclient.NewForConfig(cfg)
	if err != nil {
		return nil, errInternal
	}
	return &AgenticRunInvestigationRequest{
		reviews:                  auth.TokenReviews(),
		selfSubjectAccessReviews: newSelfSubjectAccessReviews,
		anonymous:                rest.AnonymousClientConfig(rest.CopyConfig(cfg)),
		shared:                   shared,
		namespace:                namespace,
		target:                   target,
		logger:                   logger,
	}, nil
}

func newSelfSubjectAccessReviews(
	cfg *rest.Config,
) (authorizationclient.SelfSubjectAccessReviewInterface, error) {
	authz, err := authorizationclient.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return authz.SelfSubjectAccessReviews(), nil
}

func validateKubernetesEndpoint(cfg *rest.Config) error {
	server, err := url.Parse(cfg.Host)
	if err != nil {
		return errors.New("invalid Kubernetes endpoint URL")
	}
	if server.Scheme != "https" {
		return errors.New("Kubernetes endpoint must use HTTPS")
	}
	if server.Host == "" {
		return errors.New("Kubernetes endpoint must include a host")
	}
	if server.User != nil {
		return errors.New("Kubernetes endpoint must not contain user information")
	}
	if cfg.Insecure {
		return errors.New("Kubernetes TLS certificate verification must be enabled")
	}
	return nil
}

func (a *AgenticRunInvestigationRequest) Authorize(
	ctx context.Context, token string,
) (AgenticRunCreator, error) {
	if err := ctx.Err(); err != nil {
		return nil, errInternal
	}
	if token == "" {
		return nil, errInvestigationUnauthorized
	}
	review, err := a.reviews.Create(ctx, &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{Token: token},
	}, metav1.CreateOptions{})
	if err != nil || review.Status.Error != "" {
		return nil, errInternal
	}
	if !review.Status.Authenticated {
		return nil, errInvestigationUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, errInternal
	}

	// Never retain request credentials in the shared config or client.
	callerConfig := rest.CopyConfig(a.anonymous)
	callerConfig.BearerToken = token
	reviews, err := a.selfSubjectAccessReviews(callerConfig)
	if err != nil {
		return nil, errInternal
	}
	access, err := reviews.Create(ctx,
		&authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Group:     "agentic.openshift.io",
					Resource:  "agenticruns",
					Verb:      "create",
					Namespace: a.namespace,
				},
			},
		}, metav1.CreateOptions{})
	if err != nil {
		return nil, investigationCallerError(err)
	}
	if access.Status.EvaluationError != "" {
		return nil, errInternal
	}
	if !access.Status.Allowed {
		return nil, errInvestigationForbidden
	}
	if err := ctx.Err(); err != nil {
		return nil, errInternal
	}
	caller, err := client.New(callerConfig, client.Options{
		Scheme: a.shared.Scheme(), Mapper: a.shared.RESTMapper(),
	})
	if err != nil {
		return nil, errInternal
	}
	return agenticrun.NewClient(caller, a.namespace, a.target, a.logger), nil
}

func investigationCallerError(err error) error {
	switch {
	case apierrors.IsUnauthorized(err):
		return errInvestigationUnauthorized
	case apierrors.IsForbidden(err):
		return errInvestigationForbidden
	default:
		return errInternal
	}
}
