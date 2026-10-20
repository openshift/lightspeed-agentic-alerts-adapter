package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type investigateFunc func(
	context.Context, string, AgenticRunCreator,
) (*agenticv1alpha1.AgenticRun, error)

func (f investigateFunc) Investigate(
	ctx context.Context, fingerprint string, creator AgenticRunCreator,
) (*agenticv1alpha1.AgenticRun, error) {
	return f(ctx, fingerprint, creator)
}

type authorizeFunc func(context.Context, string) (AgenticRunCreator, error)

func (f authorizeFunc) Authorize(
	ctx context.Context, token string,
) (AgenticRunCreator, error) {
	return f(ctx, token)
}

func TestInvestigationHandlerInvalidRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty body"},
		{name: "malformed JSON", body: `{"fingerprint":`},
		{name: "missing fingerprint", body: `{}`},
		{name: "empty fingerprint", body: `{"fingerprint":""}`},
		{name: "null fingerprint", body: `{"fingerprint":null}`},
		{name: "numeric fingerprint", body: `{"fingerprint":123}`},
		{name: "boolean fingerprint", body: `{"fingerprint":true}`},
		{name: "object fingerprint", body: `{"fingerprint":{}}`},
		{name: "array fingerprint", body: `{"fingerprint":[]}`},
		{name: "unknown field", body: `{"fingerprint":"abc","extra":true}`},
		{name: "null body", body: `null`},
		{name: "array body", body: `[]`},
		{name: "second object", body: `{"fingerprint":"abc"} {}`},
		{name: "trailing garbage", body: `{"fingerprint":"abc"} garbage`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost, "/api/v1/investigations", strings.NewReader(tt.body),
			)
			response := httptest.NewRecorder()

			NewInvestigationHandler(nil).ServeHTTP(response, request)

			assert.Equal(t, http.StatusBadRequest, response.Code)
			assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
			assert.JSONEq(t, `{"error":{
				"code":"invalid_request",
				"message":"Request must contain only a non-empty fingerprint string"
			}}`, response.Body.String())
		})
	}
}

func TestInvestigationHandlerBodyRead(t *testing.T) {
	valid := `{"fingerprint":"abc"}`
	tests := []struct {
		name    string
		body    io.Reader
		status  int
		code    string
		message string
	}{
		{
			name: "valid with whitespace", body: strings.NewReader(valid + "\n\t "),
			status: http.StatusInternalServerError,
			code:   "internal_error", message: "An internal error occurred",
		},
		{
			name:   "exactly at limit",
			body:   strings.NewReader(valid + strings.Repeat(" ", 4096-len(valid))),
			status: http.StatusInternalServerError,
			code:   "internal_error", message: "An internal error occurred",
		},
		{
			name:   "one byte over limit",
			body:   strings.NewReader(valid + strings.Repeat(" ", 4097-len(valid))),
			status: http.StatusRequestEntityTooLarge,
			code:   "request_too_large", message: "Request body exceeds the size limit",
		},
		{
			name: "oversized malformed body", body: strings.NewReader(strings.Repeat("x", 4097)),
			status: http.StatusRequestEntityTooLarge,
			code:   "request_too_large", message: "Request body exceeds the size limit",
		},
		{
			name: "read timeout", body: iotest.ErrReader(os.ErrDeadlineExceeded),
			status: http.StatusRequestTimeout,
			code:   "request_timeout", message: "Timed out while reading the request",
		},
		{
			name: "read failure", body: iotest.ErrReader(errors.New("test read failure")),
			status: http.StatusInternalServerError,
			code:   "internal_error", message: "An internal error occurred",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost, "/api/v1/investigations", tt.body,
			)
			response := httptest.NewRecorder()

			NewInvestigationHandler(nil).ServeHTTP(response, request)

			assert.Equal(t, tt.status, response.Code)
			assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
			assert.JSONEq(t, fmt.Sprintf(
				`{"error":{"code":%q,"message":%q}}`, tt.code, tt.message,
			), response.Body.String())
		})
	}
}

func TestAuthMiddleware(t *testing.T) {
	const unauthorized = `{"error":{"code":"unauthorized","message":"A valid bearer token is required"}}`
	const forbidden = `{"error":{"code":"forbidden","message":"You are not allowed to trigger investigations"}}`
	const internal = `{"error":{"code":"internal_error","message":"An internal error occurred"}}`

	tests := []struct {
		name          string
		headers       []string
		authErr       error
		cancelDuring  bool
		wantCalls     int
		wantNextCalls int
		invalidBody   bool
		nilCreator    bool
		status        int
		body          string
	}{
		{
			name:   "missing token",
			status: http.StatusUnauthorized, body: unauthorized,
		},
		{
			name: "auth precedes JSON validation", invalidBody: true,
			status: http.StatusUnauthorized, body: unauthorized,
		},
		{
			name: "wrong scheme", headers: []string{"Basic test-only-caller"},
			status: http.StatusUnauthorized, body: unauthorized,
		},
		{
			name: "empty token", headers: []string{"Bearer "},
			status: http.StatusUnauthorized, body: unauthorized,
		},
		{
			name: "invalid token characters", headers: []string{"Bearer invalid,token"},
			status: http.StatusUnauthorized, body: unauthorized,
		},
		{
			name:    "duplicate headers",
			headers: []string{"Bearer test-only-caller", "Bearer test-only-other"},
			status:  http.StatusUnauthorized, body: unauthorized,
		},
		{
			name: "authentication rejected", headers: []string{"Bearer test-only-caller"},
			authErr: errInvestigationUnauthorized, wantCalls: 1,
			status: http.StatusUnauthorized, body: unauthorized,
		},
		{
			name: "authorization denied", headers: []string{"Bearer test-only-caller"},
			authErr: errInvestigationForbidden, wantCalls: 1,
			status: http.StatusForbidden, body: forbidden,
		},
		{
			name: "dependency failure", headers: []string{"Bearer test-only-caller"},
			authErr: errors.New("test diagnostic must not escape"), wantCalls: 1,
			status: http.StatusInternalServerError, body: internal,
		},
		{
			name: "authorized", headers: []string{"Bearer test-only-caller"},
			wantCalls: 1, wantNextCalls: 1,
			status: http.StatusInternalServerError, body: internal,
		},
		{
			name: "case insensitive scheme", headers: []string{"bearer test-only-caller"},
			wantCalls: 1, wantNextCalls: 1,
			status: http.StatusInternalServerError, body: internal,
		},
		{
			name:    "cancellation overrides auth rejection",
			headers: []string{"Bearer test-only-caller"},
			authErr: errInvestigationUnauthorized, cancelDuring: true, wantCalls: 1,
			status: http.StatusInternalServerError, body: internal,
		},
		{
			name: "missing creator", headers: []string{"Bearer test-only-caller"},
			nilCreator: true, wantCalls: 1,
			status: http.StatusInternalServerError, body: internal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			payload := `{"fingerprint":"abc"}`
			if tt.invalidBody {
				payload = "{"
			}
			request := httptest.NewRequest(
				http.MethodPost, "/api/v1/investigations",
				strings.NewReader(payload),
			).WithContext(ctx)
			for _, header := range tt.headers {
				request.Header.Add("Authorization", header)
			}
			response := httptest.NewRecorder()
			calls := 0
			var authContext context.Context
			access := authorizeFunc(func(
				gotCtx context.Context, token string,
			) (AgenticRunCreator, error) {
				calls++
				authContext = gotCtx
				assert.Equal(t, "test-only-caller", token)
				deadline, ok := gotCtx.Deadline()
				if assert.True(t, ok, "authorization must have a deadline") {
					assert.Positive(t, time.Until(deadline))
					assert.LessOrEqual(t, time.Until(deadline), requestTimeout)
				}
				if tt.cancelDuring {
					cancel()
					assert.ErrorIs(t, gotCtx.Err(), context.Canceled)
				}
				if tt.nilCreator {
					return nil, nil
				}
				return &fakeRunClient{}, tt.authErr
			})

			nextCalls := 0
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalls++
				NewInvestigationHandler(investigateFunc(func(
					context.Context, string, AgenticRunCreator,
				) (*agenticv1alpha1.AgenticRun, error) {
					return nil, errInternal
				})).ServeHTTP(w, r)
			})
			server := NewInvestigationServer("", AuthMiddleware(access, next))
			server.Handler.ServeHTTP(response, request)

			assert.Equal(t, tt.wantCalls, calls)
			assert.Equal(t, tt.wantNextCalls, nextCalls)
			assert.Equal(t, tt.status, response.Code)
			assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
			assert.JSONEq(t, tt.body, response.Body.String())
			wantChallenge := ""
			if tt.status == http.StatusUnauthorized {
				wantChallenge = "Bearer"
			}
			assert.Equal(t, wantChallenge, response.Header().Get("WWW-Authenticate"))
			if authContext != nil {
				assert.ErrorIs(t, authContext.Err(), context.Canceled)
			}
		})
	}
}

func TestAuthMiddlewarePreservesRequestContext(t *testing.T) {
	parent, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	parentDeadline, ok := parent.Deadline()
	if !assert.True(t, ok) {
		return
	}
	creator := &fakeRunClient{}
	authCalls, nextCalls := 0, 0
	var authContext context.Context
	access := authorizeFunc(func(
		ctx context.Context, token string,
	) (AgenticRunCreator, error) {
		authCalls++
		authContext = ctx
		assert.Equal(t, "test-only-caller", token)
		deadline, ok := ctx.Deadline()
		assert.True(t, ok)
		assert.Equal(t, parentDeadline, deadline)
		return creator, nil
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalls++
		assert.Same(t, creator, r.Context().Value(creatorContextKey{}))
		deadline, ok := r.Context().Deadline()
		assert.True(t, ok)
		assert.Equal(t, parentDeadline, deadline)
		cancel()
		assert.ErrorIs(t, r.Context().Err(), context.Canceled)
		assert.ErrorIs(t, authContext.Err(), context.Canceled)
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/investigations", nil,
	).WithContext(parent)
	request.Header.Set("Authorization", "Bearer test-only-caller")
	response := httptest.NewRecorder()

	server := NewInvestigationServer("", AuthMiddleware(access, next))
	server.Handler.ServeHTTP(response, request)

	assert.Equal(t, 1, authCalls)
	assert.Equal(t, 1, nextCalls)
	assert.Equal(t, http.StatusNoContent, response.Code)
}

func TestInvestigationHandlerResponse(t *testing.T) {
	const internal = `{"error":{"code":"internal_error","message":"An internal error occurred"}}`
	tests := []struct {
		name         string
		err          error
		nilResult    bool
		cancelDuring bool
		invalidBody  bool
		status       int
		response     string
	}{
		{
			name: "created", status: http.StatusCreated,
			response: `{"namespace":"test-namespace","name":"test-run"}`,
		},
		{
			name: "suspended", err: errSuspended, status: http.StatusForbidden,
			response: `{"error":{"code":"investigations_suspended","message":"Investigations are suspended"}}`,
		},
		{
			name: "alert not found", err: errAlertNotFound, status: http.StatusNotFound,
			response: `{"error":{"code":"alert_not_found","message":"No eligible alert matches the fingerprint"}}`,
		},
		{
			name: "in progress", err: errRunInProgress, status: http.StatusConflict,
			response: `{"error":{"code":"investigation_in_progress","message":"An investigation already exists"}}`,
		},
		{
			name: "caller unauthorized", err: errInvestigationUnauthorized,
			status:   http.StatusUnauthorized,
			response: `{"error":{"code":"unauthorized","message":"A valid bearer token is required"}}`,
		},
		{
			name: "caller forbidden", err: errInvestigationForbidden,
			status:   http.StatusForbidden,
			response: `{"error":{"code":"forbidden","message":"You are not allowed to trigger investigations"}}`,
		},
		{
			name: "dependency failure", err: errors.New("test diagnostic must not escape"),
			status: http.StatusInternalServerError, response: internal,
		},
		{
			name: "missing result", nilResult: true,
			status: http.StatusInternalServerError, response: internal,
		},
		{
			name: "cancelled during investigation", cancelDuring: true,
			status: http.StatusInternalServerError, response: internal,
		},
		{
			name: "authorized invalid body", invalidBody: true,
			status:   http.StatusBadRequest,
			response: `{"error":{"code":"invalid_request","message":"Request must contain only a non-empty fingerprint string"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			creator := &fakeRunClient{}
			authCalls, investigationCalls := 0, 0
			access := authorizeFunc(func(
				context.Context, string,
			) (AgenticRunCreator, error) {
				authCalls++
				return creator, nil
			})
			investigator := investigateFunc(func(
				gotCtx context.Context, fingerprint string, gotCreator AgenticRunCreator,
			) (*agenticv1alpha1.AgenticRun, error) {
				investigationCalls++
				assert.Equal(t, "abc", fingerprint)
				assert.Same(t, creator, gotCreator)
				assert.Same(t, creator, gotCtx.Value(creatorContextKey{}))
				deadline, ok := gotCtx.Deadline()
				if assert.True(t, ok) {
					assert.Positive(t, time.Until(deadline))
					assert.LessOrEqual(t, time.Until(deadline), requestTimeout)
				}
				if tt.cancelDuring {
					cancel()
					assert.ErrorIs(t, gotCtx.Err(), context.Canceled)
				}
				if tt.nilResult {
					return nil, tt.err
				}
				return &agenticv1alpha1.AgenticRun{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-run", Namespace: "test-namespace",
					},
				}, tt.err
			})
			payload := `{"fingerprint":"abc"}`
			if tt.invalidBody {
				payload = "{"
			}
			request := httptest.NewRequest(
				http.MethodPost, "/api/v1/investigations", strings.NewReader(payload),
			).WithContext(ctx)
			request.Header.Set("Authorization", "Bearer test-only-caller")
			response := httptest.NewRecorder()
			server := NewInvestigationServer(
				"", AuthMiddleware(access, NewInvestigationHandler(investigator)),
			)

			server.Handler.ServeHTTP(response, request)

			assert.Equal(t, 1, authCalls)
			wantCalls := 1
			if tt.invalidBody {
				wantCalls = 0
			}
			assert.Equal(t, wantCalls, investigationCalls)
			assert.Equal(t, tt.status, response.Code)
			assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
			assert.JSONEq(t, tt.response, response.Body.String())
			wantChallenge := ""
			if tt.status == http.StatusUnauthorized {
				wantChallenge = "Bearer"
			}
			assert.Equal(t, wantChallenge, response.Header().Get("WWW-Authenticate"))
		})
	}
}
