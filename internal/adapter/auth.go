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
		anonymous:                rest.AnonymousClientConfig(cfg),
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
		return errors.New("kubernetes endpoint must use HTTPS")
	}
	if server.Host == "" {
		return errors.New("kubernetes endpoint must include a host")
	}
	if server.User != nil {
		return errors.New("kubernetes endpoint must not contain user information")
	}
	if cfg.Insecure {
		return errors.New("kubernetes TLS certificate verification must be enabled")
	}
	return nil
}

func (a *AgenticRunInvestigationRequest) Authorize(
	ctx context.Context, token string,
) (AgenticRunCreator, error) {
	if err := ctx.Err(); err != nil {
		return nil, internalError(ctx, a.logger, "authorize", err)
	}
	if token == "" {
		return nil, errInvestigationUnauthorized
	}
	review, err := a.reviews.Create(ctx, &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{Token: token},
	}, metav1.CreateOptions{})
	if err != nil {
		return nil, internalError(ctx, a.logger, "token_review", err)
	}
	if !review.Status.Authenticated {
		return nil, errInvestigationUnauthorized
	}
	if review.Status.Error != "" {
		return nil, internalError(ctx, a.logger, "token_review_response", errors.New("authenticated with review error"))
	}
	if err := ctx.Err(); err != nil {
		return nil, internalError(ctx, a.logger, "prepare_caller_client", err)
	}

	// Never retain request credentials in the shared config or client.
	callerConfig := rest.CopyConfig(a.anonymous)
	callerConfig.BearerToken = token
	reviews, err := a.selfSubjectAccessReviews(callerConfig)
	if err != nil {
		return nil, internalError(ctx, a.logger, "create_access_review_client", err)
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
		logError(ctx, a.logger, "self_subject_access_review", err)
		return nil, investigationCallerError(err)
	}
	if access.Status.EvaluationError != "" {
		return nil, internalError(ctx, a.logger, "access_review_response", errors.New("evaluation error"))
	}
	if !access.Status.Allowed {
		return nil, errInvestigationForbidden
	}
	if err := ctx.Err(); err != nil {
		return nil, internalError(ctx, a.logger, "create_caller_client", err)
	}
	caller, err := client.New(callerConfig, client.Options{
		Scheme: a.shared.Scheme(), Mapper: a.shared.RESTMapper(),
	})
	if err != nil {
		return nil, internalError(ctx, a.logger, "create_caller_client", err)
	}
	return agenticrun.NewClient(caller, a.namespace, a.target, a.logger), nil
}

// internalError logs a sanitized failure and returns the public API error.
func internalError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	logError(ctx, logger, operation, err)
	return errInternal
}

func logError(ctx context.Context, logger *slog.Logger, operation string, err error) {
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	kind := "dependency_error"
	switch {
	case errors.Is(err, context.Canceled):
		kind = "request_cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		kind = "deadline_exceeded"
	case apierrors.IsUnauthorized(err):
		kind = "unauthorized"
	case apierrors.IsForbidden(err):
		kind = "forbidden"
	}
	fields := []any{"operation", operation, "error_kind", kind}
	var status apierrors.APIStatus
	if errors.As(err, &status) {
		fields = append(fields, "upstream_status", status.Status().Code)
	}
	logger.ErrorContext(ctx, "API operation failed", fields...)
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
