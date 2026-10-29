package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

// apiError contains only fixed, public response text, never dependency diagnostics.
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.code }

var (
	errInvalidRequest            = &apiError{http.StatusBadRequest, "invalid_request", "Request must contain only a non-empty fingerprint string"}
	errInvestigationUnauthorized = &apiError{http.StatusUnauthorized, "unauthorized", "A valid bearer token is required"}
	errInvestigationForbidden    = &apiError{http.StatusForbidden, "forbidden", "You are not allowed to trigger investigations"}
	errSuspended                 = &apiError{http.StatusForbidden, "investigations_suspended", "Investigations are suspended"}
	errAlertNotFound             = &apiError{http.StatusNotFound, "alert_not_found", "No eligible alert matches the fingerprint"}
	errRequestTimeout            = &apiError{http.StatusRequestTimeout, "request_timeout", "Timed out while reading the request"}
	errRunInProgress             = &apiError{http.StatusConflict, "investigation_in_progress", "An investigation already exists"}
	errRequestTooLarge           = &apiError{http.StatusRequestEntityTooLarge, "request_too_large", "Request body exceeds the size limit"}
	errInternal                  = &apiError{http.StatusInternalServerError, "internal_error", "An internal error occurred"}
)

type requestBody struct {
	Fingerprint string `json:"fingerprint"`
}

type responseBody struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Investigator creates an investigation using the authorized caller's creator.
type Investigator interface {
	Investigate(context.Context, string, AgenticRunCreator) (*agenticv1alpha1.AgenticRun, error)
}

// NewInvestigationHandler validates requests and creates an investigation.
func NewInvestigationHandler(investigator Investigator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const maxBody = 4 << 10
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if err != nil {
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				err = errRequestTimeout
			}
			writeErrorResponse(w, err)
			return
		}
		if len(body) > maxBody {
			writeErrorResponse(w, errRequestTooLarge)
			return
		}

		var request requestBody
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.Fingerprint == "" {
			writeErrorResponse(w, errInvalidRequest)
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			writeErrorResponse(w, errInvalidRequest)
			return
		}

		ctx := r.Context()
		creator, _ := ctx.Value(creatorContextKey{}).(AgenticRunCreator)
		if creator == nil || ctx.Err() != nil {
			writeErrorResponse(w, errInternal)
			return
		}
		run, err := investigator.Investigate(ctx, request.Fingerprint, creator)
		if err != nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil || run == nil {
			writeErrorResponse(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(responseBody{
			Namespace: run.Namespace,
			Name:      run.Name,
		})
	}
}

func writeErrorResponse(w http.ResponseWriter, err error) {
	var response *apiError
	if !errors.As(err, &response) {
		response = errInternal
	}
	w.Header().Set("Content-Type", "application/json")
	if response.status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	w.WriteHeader(response.status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": response.code, "message": response.message},
	})
}
