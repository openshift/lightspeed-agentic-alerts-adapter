package alertmanager

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTokenFile(t *testing.T, token string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func setupCAFile(t *testing.T, server *httptest.Server) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "service-ca.crt")

	cert := server.TLS.Certificates[0]
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}

	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: parsed.Raw,
	})
	if err := os.WriteFile(path, pemData, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGetAlerts(t *testing.T) {
	tests := []struct {
		name          string
		handler       http.HandlerFunc
		expectedCount int
		expectErr     bool
		errContains   string
		token         string
	}{
		{
			name:  "successful retrieval with alerts",
			token: "test-token",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[
					{
						"annotations": {"summary": "high cpu usage"},
						"endsAt": "2025-01-01T01:00:00.000Z",
						"startsAt": "2025-01-01T00:00:00.000Z",
						"updatedAt": "2025-01-01T00:00:00.000Z",
						"fingerprint": "abc123",
						"receivers": [{"name": "default"}],
						"status": {"state": "active", "silencedBy": [], "inhibitedBy": [], "mutedBy": []},
						"labels": {"alertname": "HighCPU", "severity": "warning"},
						"generatorURL": "http://prometheus:9090/graph"
					}
				]`))
			}),
			expectedCount: 1,
		},
		{
			name:  "query filters for active non-silenced non-inhibited alerts",
			token: "test-token",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if q.Get("active") != "true" {
					t.Errorf("active = %q, want %q", q.Get("active"), "true")
				}
				if q.Get("silenced") != "false" {
					t.Errorf("silenced = %q, want %q", q.Get("silenced"), "false")
				}
				if q.Get("inhibited") != "false" {
					t.Errorf("inhibited = %q, want %q", q.Get("inhibited"), "false")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			}),
			expectedCount: 0,
		},
		{
			name:  "empty alerts list",
			token: "test-token",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			}),
			expectedCount: 0,
		},
		{
			name:  "bearer token sent in request",
			token: "my-sa-token",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth := r.Header.Get("Authorization")
				if auth != "Bearer my-sa-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			}),
			expectedCount: 0,
		},
		{
			name:  "bearer token with trailing newline is trimmed",
			token: "my-sa-token\n",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth := r.Header.Get("Authorization")
				if auth != "Bearer my-sa-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			}),
			expectedCount: 0,
		},
		{
			name:  "authentication failure",
			token: "bad-token",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`"forbidden"`))
			}),
			expectErr:   true,
			errContains: "querying alerts",
		},
		{
			name:  "server error",
			token: "test-token",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`"internal server error"`))
			}),
			expectErr:   true,
			errContains: "querying alerts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(tt.handler)
			defer server.Close()

			tokenPath := setupTokenFile(t, tt.token)
			caPath := setupCAFile(t, server)

			client, err := New(Config{
				URL:       server.URL,
				TokenPath: tokenPath,
				CAPath:    caPath,
			})
			if err != nil {
				t.Fatalf("unexpected error creating client: %v", err)
			}

			alerts, err := client.GetAlerts(context.Background())
			if tt.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(alerts) != tt.expectedCount {
				t.Errorf("got %d alerts, want %d", len(alerts), tt.expectedCount)
			}
		})
	}
}

func TestGetAlertsMissingTokenFile(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request should not have been made")
	}))
	defer server.Close()

	caPath := setupCAFile(t, server)

	client, err := New(Config{
		URL:       server.URL,
		TokenPath: "/nonexistent/path/token",
		CAPath:    caPath,
	})
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	_, err = client.GetAlerts(context.Background())
	if err == nil {
		t.Fatal("expected error for missing token file, got nil")
	}
	if !strings.Contains(err.Error(), "reading service account token") {
		t.Errorf("error %q does not mention token reading", err.Error())
	}
}

func TestGetAlertsWithConfiguredToken(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		wantBearer  string
		trustServer bool
		errText     string
	}{
		{
			name:        "sends configured bearer token",
			token:       "configured-token",
			wantBearer:  "Bearer configured-token",
			trustServer: true,
		},
		{
			name:        "trims configured bearer token",
			token:       "configured-token\n",
			wantBearer:  "Bearer configured-token",
			trustServer: true,
		},
		{
			name:    "rejects untrusted tls certificate",
			token:   "configured-token",
			errText: "tls: failed to verify certificate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != tt.wantBearer {
					t.Errorf("Authorization = %q, want %q", got, tt.wantBearer)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[]`))
			}))
			defer server.Close()

			var caBundle []byte
			if tt.trustServer {
				var err error
				caBundle, err = os.ReadFile(setupCAFile(t, server))
				if err != nil {
					t.Fatal(err)
				}
			}
			client, err := New(Config{
				URL:      server.URL,
				CABundle: caBundle,
				CASource: "test CA bundle",
				Token:    tt.token,
			})
			if err != nil {
				t.Fatalf("unexpected error creating client: %v", err)
			}

			_, err = client.GetAlerts(context.Background())
			if tt.errText != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errText) {
					t.Errorf("error = %q, want it to contain %q", err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error retrieving alerts: %v", err)
			}
		})
	}
}

func TestNewMissingCAFile(t *testing.T) {
	_, err := New(Config{
		URL:       "https://localhost:9094",
		TokenPath: "/some/path",
		CAPath:    "/nonexistent/ca.crt",
	})
	if err == nil {
		t.Fatal("expected error for missing CA file, got nil")
	}
	if !strings.Contains(err.Error(), "reading ca certificate") {
		t.Errorf("error %q does not mention ca certificate", err.Error())
	}
}

func TestNewInvalidURL(t *testing.T) {
	tokenPath := setupTokenFile(t, "token")

	_, err := New(Config{
		URL:       "://invalid",
		TokenPath: tokenPath,
		CAPath:    "/some/path",
	})
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
	if !strings.Contains(err.Error(), "parsing url") {
		t.Errorf("error %q does not mention url parsing", err.Error())
	}
}

func TestNewRejectsNonHTTPSURL(t *testing.T) {
	_, err := New(Config{
		URL:       "http://alertmanager.example.com",
		TokenPath: "/some/path",
		CAPath:    "/some/path",
	})
	if err == nil {
		t.Fatal("expected error for non-HTTPS URL, got nil")
	}
	if !strings.Contains(err.Error(), "scheme must be https") {
		t.Errorf("error %q does not require HTTPS", err.Error())
	}
}

func TestNewMalformedURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "no scheme", url: "alertmanager:9094"},
		{name: "empty string after default override", url: "not-a-url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(Config{
				URL:       tt.url,
				TokenPath: "/some/path",
				CAPath:    "/some/path",
			})
			if err == nil {
				t.Fatal("expected error for malformed URL, got nil")
			}
			if !strings.Contains(err.Error(), "invalid url") {
				t.Errorf("error %q does not mention invalid url", err.Error())
			}
		})
	}
}

func TestNewDefaultConfig(t *testing.T) {
	var cfg Config
	cfg.setDefaults()

	if cfg.URL != defaultURL {
		t.Errorf("default URL = %q, want %q", cfg.URL, defaultURL)
	}
	if cfg.TokenPath != defaultTokenPath {
		t.Errorf("default token path = %q, want %q", cfg.TokenPath, defaultTokenPath)
	}
	if cfg.CAPath != defaultCAPath {
		t.Errorf("default CA path = %q, want %q", cfg.CAPath, defaultCAPath)
	}
}

func TestNewConfigCustomURL(t *testing.T) {
	customURL := "https://custom-alertmanager:9094"
	cfg := Config{URL: customURL}
	cfg.setDefaults()

	if cfg.URL != customURL {
		t.Errorf("URL = %q, want %q", cfg.URL, customURL)
	}
	if cfg.TokenPath != defaultTokenPath {
		t.Errorf("token path = %q, want %q", cfg.TokenPath, defaultTokenPath)
	}
}
