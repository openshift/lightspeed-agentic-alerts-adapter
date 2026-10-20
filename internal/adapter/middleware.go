package adapter

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"
)

const requestTimeout = 20 * time.Second

var tokenHeader = regexp.MustCompile(`(?i)^Bearer +([A-Za-z0-9._~+/-]+=*)$`)

type creatorContextKey struct{}

func withTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AuthMiddleware authorizes the caller and passes its creator through the request context.
func AuthMiddleware(access AgenticRunRequest, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := ctx.Err(); err != nil {
			writeAuthError(w, err)
			return
		}
		token := readToken(r)
		if token == "" {
			writeAuthError(w, errInvestigationUnauthorized)
			return
		}
		creator, err := access.Authorize(ctx, token)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			writeAuthError(w, err)
			return
		}
		if creator == nil {
			writeAuthError(w, errInternal)
			return
		}
		ctx = context.WithValue(ctx, creatorContextKey{}, creator)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func readToken(r *http.Request) string {
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 {
		return ""
	}
	match := tokenHeader.FindStringSubmatch(headers[0])
	if match == nil {
		return ""
	}
	return match[1]
}

func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvestigationUnauthorized):
		writeErrorResponse(w, http.StatusUnauthorized,
			"unauthorized", "A valid bearer token is required")
	case errors.Is(err, errInvestigationForbidden):
		writeErrorResponse(w, http.StatusForbidden,
			"forbidden", "You are not allowed to trigger investigations")
	default:
		writeErrorResponse(w, http.StatusInternalServerError,
			"internal_error", "An internal error occurred")
	}
}
