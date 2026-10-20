package adapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
)

const invalidRequestMessage = "Request must contain only a non-empty fingerprint string"

type requestBody struct {
	Fingerprint string `json:"fingerprint"`
}

// NewInvestigationHandler validates requests without creating runs yet.
func NewInvestigationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
		if err != nil {
			writeReadError(w, err)
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

		// TODO: Check suspension, fetch the canonical alert, and build/deduplicate the run.
		// Create it using the caller's creator from the request context and return 201.
		writeErrorResponse(w, http.StatusInternalServerError,
			"internal_error", "An internal error occurred")
	}
}

func writeReadError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	var timeout net.Error
	switch {
	case errors.As(err, &tooLarge):
		writeErrorResponse(w, http.StatusRequestEntityTooLarge,
			"request_too_large", "Request body exceeds the size limit")
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
