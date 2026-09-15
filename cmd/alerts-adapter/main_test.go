package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"os"
	"strings"
	"testing"

	hubv1alpha1 "github.com/openshift/lightspeed-hub/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAlertCredentialsFromSecret(t *testing.T) {
	tests := []struct {
		name      string
		data      map[string][]byte
		wantURL   string
		wantToken string
		wantCA    string
		errText   string
	}{
		{
			name: "valid credentials",
			data: map[string][]byte{
				alertmanagerURLKey: []byte("https://alertmanager.example.com"),
				tokenKey:           []byte("spoke-token"),
				caBundleKey:        []byte("test-ca-bundle"),
			},
			wantURL:   "https://alertmanager.example.com",
			wantToken: "spoke-token",
			wantCA:    "test-ca-bundle",
		},
		{
			name: "missing alertmanager url",
			data: map[string][]byte{
				tokenKey:    []byte("spoke-token"),
				caBundleKey: []byte("test-ca-bundle"),
			},
			errText: alertmanagerURLKey,
		},
		{
			name: "empty alertmanager url",
			data: map[string][]byte{
				alertmanagerURLKey: []byte{},
				tokenKey:           []byte("spoke-token"),
				caBundleKey:        []byte("test-ca-bundle"),
			},
			errText: alertmanagerURLKey,
		},
		{
			name: "missing token",
			data: map[string][]byte{
				alertmanagerURLKey: []byte("https://alertmanager.example.com"),
				caBundleKey:        []byte("test-ca-bundle"),
			},
			errText: tokenKey,
		},
		{
			name: "empty token",
			data: map[string][]byte{
				alertmanagerURLKey: []byte("https://alertmanager.example.com"),
				tokenKey:           []byte{},
				caBundleKey:        []byte("test-ca-bundle"),
			},
			errText: tokenKey,
		},
		{
			name: "missing ca bundle",
			data: map[string][]byte{
				alertmanagerURLKey: []byte("https://alertmanager.example.com"),
				tokenKey:           []byte("spoke-token"),
			},
			errText: caBundleKey,
		},
		{
			name: "empty ca bundle",
			data: map[string][]byte{
				alertmanagerURLKey: []byte("https://alertmanager.example.com"),
				tokenKey:           []byte("spoke-token"),
				caBundleKey:        []byte{},
			},
			errText: caBundleKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, token, caBundle, err := alertCredentialsFromSecret(&corev1.Secret{
				Data: tt.data,
			})

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
				t.Fatalf("unexpected error: %v", err)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
			if token != tt.wantToken {
				t.Errorf("token = %q, want %q", token, tt.wantToken)
			}
			if got := string(caBundle); got != tt.wantCA {
				t.Errorf("caBundle = %q, want %q", got, tt.wantCA)
			}
		})
	}
}

func testCABundle(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating test CA key: %v", err)
	}
	cert, err := x509.CreateCertificate(
		rand.Reader,
		&x509.Certificate{
			SerialNumber:          big.NewInt(1),
			IsCA:                  true,
			BasicConstraintsValid: true,
		},
		&x509.Certificate{
			SerialNumber:          big.NewInt(1),
			IsCA:                  true,
			BasicConstraintsValid: true,
		},
		&key.PublicKey,
		key,
	)
	if err != nil {
		t.Fatalf("creating test CA certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})
}

func TestNewTargetsMulticluster(t *testing.T) {
	tests := []struct {
		name         string
		multicluster bool
		wantNames    []string
		errText      string
	}{
		{
			name:    "disabled does not discover spoke targets",
			errText: "no targets configured",
		},
		{
			name:         "enabled returns spoke target",
			multicluster: true,
			wantNames:    []string{"spoke"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ALERTMANAGER_URL", "")

			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatalf("adding core scheme: %v", err)
			}
			if err := hubv1alpha1.AddToScheme(scheme); err != nil {
				t.Fatalf("adding hub scheme: %v", err)
			}

			spokeCluster := &hubv1alpha1.SpokeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "spoke",
					Labels: map[string]string{
						alertCredentialSecretLabel: "spoke-alert-credentials",
					},
				},
			}
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(
					spokeCluster,
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "spoke-alert-credentials",
							Namespace: "test-namespace",
						},
						Data: map[string][]byte{
							alertmanagerURLKey: []byte("https://alertmanager.spoke"),
							tokenKey:           []byte("spoke-token"),
							caBundleKey:        testCABundle(t),
						},
					},
				).
				Build()

			targets, err := newTargets(
				context.Background(),
				k8sClient,
				"test-namespace",
				tt.multicluster,
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			if tt.errText != "" {
				if err == nil {
					t.Fatal("newTargets() error = nil, want an error")
				}
				if !strings.Contains(err.Error(), tt.errText) {
					t.Errorf("newTargets() error = %q, want it to contain %q", err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatalf("newTargets() error = %v", err)
			}

			if len(targets) != len(tt.wantNames) {
				t.Fatalf("len(targets) = %d, want %d", len(targets), len(tt.wantNames))
			}
			for i, target := range targets {
				if target.Name != tt.wantNames[i] {
					t.Errorf("targets[%d].Name = %q, want %q", i, target.Name, tt.wantNames[i])
				}
			}
		})
	}
}

func TestNewTargetsSkipsUnlabeledAndInvalidSpokes(t *testing.T) {
	t.Setenv("ALERTMANAGER_URL", "")

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
	if err := hubv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding hub scheme: %v", err)
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(
			&hubv1alpha1.SpokeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "valid",
					Labels: map[string]string{
						alertCredentialSecretLabel: "valid-credentials",
					},
				},
			},
			&hubv1alpha1.SpokeCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "unlabeled"},
			},
			&hubv1alpha1.SpokeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "malformed",
					Labels: map[string]string{
						alertCredentialSecretLabel: "malformed-credentials",
					},
				},
			},
			&hubv1alpha1.SpokeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unavailable",
					Labels: map[string]string{
						alertCredentialSecretLabel: "unavailable-credentials",
					},
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-credentials",
					Namespace: "test-namespace",
				},
				Data: map[string][]byte{
					alertmanagerURLKey: []byte("https://alertmanager.valid"),
					tokenKey:           []byte("valid-token"),
					caBundleKey:        testCABundle(t),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "malformed-credentials",
					Namespace: "test-namespace",
				},
				Data: map[string][]byte{
					alertmanagerURLKey: []byte("https://alertmanager.malformed"),
					caBundleKey:        testCABundle(t),
				},
			},
		).
		Build()

	targets, err := newTargets(
		context.Background(),
		k8sClient,
		"test-namespace",
		true,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("newTargets() error = %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1", len(targets))
	}
	if targets[0].Name != "valid" {
		t.Errorf("targets[0].Name = %q, want %q", targets[0].Name, "valid")
	}
}

func TestMulticlusterMaxConcurrentTargets(t *testing.T) {
	tests := []struct {
		name         string
		multicluster bool
		set          bool
		value        string
		want         int
		wantErr      bool
	}{
		{
			name:         "defaults to four in multicluster mode",
			multicluster: true,
			want:         defaultMulticlusterMaxConcurrentTargets,
		},
		{
			name:         "uses configured positive value",
			multicluster: true,
			set:          true,
			value:        "2",
			want:         2,
		},
		{
			name:         "rejects non integer value",
			multicluster: true,
			set:          true,
			value:        "invalid",
			wantErr:      true,
		},
		{
			name:         "rejects zero",
			multicluster: true,
			set:          true,
			value:        "0",
			wantErr:      true,
		},
		{
			name:  "ignores invalid value in local-only mode",
			set:   true,
			value: "invalid",
			want:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(maxConcurrentTargetsEnv, "")
			if !tt.set {
				if err := os.Unsetenv(maxConcurrentTargetsEnv); err != nil {
					t.Fatalf("unsetting %s: %v", maxConcurrentTargetsEnv, err)
				}
			} else {
				t.Setenv(maxConcurrentTargetsEnv, tt.value)
			}

			got, err := multiclusterMaxConcurrentTargets(tt.multicluster)
			if tt.wantErr {
				if err == nil {
					t.Fatal("multiclusterMaxConcurrentTargets() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("multiclusterMaxConcurrentTargets() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("multiclusterMaxConcurrentTargets() = %d, want %d", got, tt.want)
			}
		})
	}
}
