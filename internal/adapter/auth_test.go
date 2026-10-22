package adapter

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	authorizationclient "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type tokenReviewFunc func(
	context.Context, *authenticationv1.TokenReview, metav1.CreateOptions,
) (*authenticationv1.TokenReview, error)

func (f tokenReviewFunc) Create(
	ctx context.Context, review *authenticationv1.TokenReview, opts metav1.CreateOptions,
) (*authenticationv1.TokenReview, error) {
	return f(ctx, review, opts)
}

type accessReviewFunc func(
	context.Context, *authorizationv1.SelfSubjectAccessReview, metav1.CreateOptions,
) (*authorizationv1.SelfSubjectAccessReview, error)

func (f accessReviewFunc) Create(
	ctx context.Context, review *authorizationv1.SelfSubjectAccessReview,
	opts metav1.CreateOptions,
) (*authorizationv1.SelfSubjectAccessReview, error) {
	return f(ctx, review, opts)
}

func TestAuthorize(t *testing.T) {
	tests := []struct {
		name            string
		unauthenticated bool
		tokenError      string
		allowed         bool
		evaluationError string
		accessError     error
		wantErr         error
	}{
		{name: "allowed", allowed: true},
		{
			name: "unauthenticated", unauthenticated: true,
			wantErr: errInvestigationUnauthorized,
		},
		{
			name:       "TokenReview error while authenticated",
			tokenError: "test failure", wantErr: errInternal,
		},
		{
			name:            "TokenReview error while unauthenticated",
			unauthenticated: true, tokenError: "test failure",
			wantErr: errInvestigationUnauthorized,
		},
		{name: "denied", wantErr: errInvestigationForbidden},
		{
			name:            "evaluation error while denied",
			evaluationError: "test failure", wantErr: errInternal,
		},
		{
			name: "evaluation error while allowed", allowed: true,
			evaluationError: "test failure", wantErr: errInternal,
		},
		{
			name:        "caller unauthorized",
			accessError: apierrors.NewUnauthorized("test failure"),
			wantErr:     errInvestigationUnauthorized,
		},
		{
			name: "caller forbidden",
			accessError: apierrors.NewForbidden(
				schema.GroupResource{Resource: "selfsubjectaccessreviews"},
				"", errors.New("test failure"),
			),
			wantErr: errInvestigationForbidden,
		},
		{
			name:        "review request failure",
			accessError: errors.New("test failure"), wantErr: errInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			scheme := runtime.NewScheme()
			if !assert.NoError(t, agenticv1alpha1.AddToScheme(scheme)) {
				return
			}
			shared := fake.NewClientBuilder().WithScheme(scheme).Build()
			cfg := &rest.Config{
				Host:        "https://kubernetes.example",
				BearerToken: "test-only-adapter",
				Impersonate: rest.ImpersonationConfig{
					UserName: "test-only-identity",
				},
			}
			original := rest.CopyConfig(cfg)
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			a, err := NewAgenticRunInvestigationRequest(
				cfg, shared, "test-namespace", "", logger,
			)
			if !assert.NoError(t, err) {
				return
			}
			tokenCalls, factoryCalls, accessCalls := 0, 0, 0
			a.reviews = tokenReviewFunc(func(
				gotCtx context.Context, review *authenticationv1.TokenReview,
				_ metav1.CreateOptions,
			) (*authenticationv1.TokenReview, error) {
				tokenCalls++
				assert.True(t, gotCtx == ctx, "TokenReview must preserve context")
				assert.Equal(t, "test-only-caller", review.Spec.Token)
				assert.Empty(t, review.Spec.Audiences)
				return &authenticationv1.TokenReview{
					Status: authenticationv1.TokenReviewStatus{
						Authenticated: !tt.unauthenticated,
						Error:         tt.tokenError,
					},
				}, nil
			})
			a.selfSubjectAccessReviews = func(
				callerConfig *rest.Config,
			) (authorizationclient.SelfSubjectAccessReviewInterface, error) {
				factoryCalls++
				assert.Equal(t, 1, tokenCalls, "TokenReview must run first")
				expected := rest.AnonymousClientConfig(cfg)
				expected.BearerToken = "test-only-caller"
				assert.NotSame(t, a.anonymous, callerConfig)
				assert.Equal(t, expected, callerConfig)
				return accessReviewFunc(func(
					gotCtx context.Context,
					review *authorizationv1.SelfSubjectAccessReview,
					_ metav1.CreateOptions,
				) (*authorizationv1.SelfSubjectAccessReview, error) {
					accessCalls++
					assert.True(t, gotCtx == ctx, "access review must preserve context")
					assert.Equal(t, authorizationv1.SelfSubjectAccessReviewSpec{
						ResourceAttributes: &authorizationv1.ResourceAttributes{
							Group:     "agentic.openshift.io",
							Resource:  "agenticruns",
							Verb:      "create",
							Namespace: "test-namespace",
						},
					}, review.Spec)
					if tt.accessError != nil {
						return nil, tt.accessError
					}
					return &authorizationv1.SelfSubjectAccessReview{
						Status: authorizationv1.SubjectAccessReviewStatus{
							Allowed:         tt.allowed,
							EvaluationError: tt.evaluationError,
						},
					}, nil
				}), nil
			}
			creator, err := a.Authorize(ctx, "test-only-caller")
			assert.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr == nil {
				assert.NotNil(t, creator)
			} else {
				assert.Nil(t, creator)
			}
			wantAccessCalls := 1
			if tt.unauthenticated || tt.tokenError != "" {
				wantAccessCalls = 0
			}
			assert.Equal(t, 1, tokenCalls)
			assert.Equal(t, wantAccessCalls, factoryCalls)
			assert.Equal(t, wantAccessCalls, accessCalls)
			assert.Equal(t, original, cfg)
			assert.Empty(t, a.anonymous.BearerToken)
		})
	}
}
