package adapter

import (
	"net/http"
	"time"
)

// NewInvestigationServer configures the listener without starting it.
func NewInvestigationServer(address string, handler http.Handler) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/investigations", withTimeout(handler))

	return &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}
