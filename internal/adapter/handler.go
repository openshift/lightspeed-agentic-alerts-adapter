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

const invalidRequestMessage = "Request must contain only a non-empty fingerprint string"

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
			writeReadError(w, err)
			return
		}
		if len(body) > maxBody {
			writeErrorResponse(w, http.StatusRequestEntityTooLarge,
				"request_too_large", "Request body exceeds the size limit")
			return
		}

		var request requestBody
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.Fingerprint == "" {
			writeErrorResponse(w, http.StatusBadRequest,
				"invalid_request", invalidRequestMessage)
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			writeErrorResponse(w, http.StatusBadRequest,
				"invalid_request", invalidRequestMessage)
			return
		}

		ctx := r.Context()
		creator, ok := ctx.Value(creatorContextKey{}).(AgenticRunCreator)
		if !ok || creator == nil || ctx.Err() != nil {
			writeRunError(w, errInternal)
			return
		}
		run, err := investigator.Investigate(ctx, request.Fingerprint, creator)
		if err != nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			writeRunError(w, err)
			return
		}
		if run == nil {
			writeRunError(w, errInternal)
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

func writeRunError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errSuspended):
		writeErrorResponse(w, http.StatusForbidden,
			"investigations_suspended", "Investigations are suspended")
	case errors.Is(err, errAlertNotFound):
		writeErrorResponse(w, http.StatusNotFound,
			"alert_not_found", "No eligible alert matches the fingerprint")
	case errors.Is(err, errRunInProgress):
		writeErrorResponse(w, http.StatusConflict,
			"investigation_in_progress", "An investigation already exists")
	default:
		writeAuthError(w, err)
	}
}

func writeReadError(w http.ResponseWriter, err error) {
	var timeout net.Error
	switch {
	case errors.As(err, &timeout) && timeout.Timeout():
		writeErrorResponse(w, http.StatusRequestTimeout,
			"request_timeout", "Timed out while reading the request")
	default:
		writeErrorResponse(w, http.StatusInternalServerError,
			"internal_error", "An internal error occurred")
	}
}

func writeErrorResponse(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}
