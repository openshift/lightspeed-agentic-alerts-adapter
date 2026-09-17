package multicluster

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"testing"

	hubv1alpha1 "github.com/openshift/lightspeed-hub/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSpokeClusterReconciler(t *testing.T) {
	tests := []struct {
		name       string
		change     func(t *testing.T, k8sClient client.Client, spokeCluster *hubv1alpha1.SpokeCluster)
		wantTarget bool
	}{
		{
			name:       "adds target for valid credentials",
			wantTarget: true,
		},
		{
			name: "removes target for unavailable credentials",
			change: func(t *testing.T, k8sClient client.Client, spokeCluster *hubv1alpha1.SpokeCluster) {
				t.Helper()
				spokeCluster.Labels[CredentialSecretLabel] = "missing"
				if err := k8sClient.Update(t.Context(), spokeCluster); err != nil {
					t.Fatalf("updating SpokeCluster credential reference: %v", err)
				}
			},
		},
		{
			name: "removes target when credential label is removed",
			change: func(t *testing.T, k8sClient client.Client, spokeCluster *hubv1alpha1.SpokeCluster) {
				t.Helper()
				delete(spokeCluster.Labels, CredentialSecretLabel)
				if err := k8sClient.Update(t.Context(), spokeCluster); err != nil {
					t.Fatalf("removing SpokeCluster credential reference: %v", err)
				}
			},
		},
		{
			name: "removes target for invalid credentials",
			change: func(t *testing.T, k8sClient client.Client, spokeCluster *hubv1alpha1.SpokeCluster) {
				t.Helper()
				if err := k8sClient.Create(t.Context(), &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "test-namespace"},
					Data: map[string][]byte{
						alertmanagerURLKey: []byte("https://alertmanager.invalid"),
						caBundleKey:        testCABundle(t),
					},
				}); err != nil {
					t.Fatalf("creating invalid Secret: %v", err)
				}
				spokeCluster.Labels[CredentialSecretLabel] = "invalid"
				if err := k8sClient.Update(t.Context(), spokeCluster); err != nil {
					t.Fatalf("updating SpokeCluster credential reference: %v", err)
				}
			},
		},
		{
			name: "removes target when SpokeCluster is deleted",
			change: func(t *testing.T, k8sClient client.Client, spokeCluster *hubv1alpha1.SpokeCluster) {
				t.Helper()
				if err := k8sClient.Delete(t.Context(), spokeCluster); err != nil {
					t.Fatalf("deleting SpokeCluster: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8sClient, registry, reconciler := newSpokeClusterReconciler(t)
			spokeCluster := &hubv1alpha1.SpokeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "spoke",
					Labels: map[string]string{
						CredentialSecretLabel: "valid",
					},
				},
			}
			if err := k8sClient.Create(t.Context(), spokeCluster); err != nil {
				t.Fatalf("creating SpokeCluster: %v", err)
			}
			if tt.change != nil {
				tt.change(t, k8sClient, spokeCluster)
			}

			reconcileSpokeCluster(t, reconciler, spokeCluster.Name)
			if got := hasSpokeTarget(registry, spokeCluster.Name); got != tt.wantTarget {
				t.Errorf("has target = %t, want %t", got, tt.wantTarget)
			}
		})
	}
}

func newSpokeClusterReconciler(t *testing.T) (client.Client, *Registry, *Reconciler) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
	if err := hubv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding hub scheme: %v", err)
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid",
				Namespace: "test-namespace",
			},
			Data: map[string][]byte{
				alertmanagerURLKey: []byte("https://alertmanager.valid"),
				tokenKey:           []byte("valid-token"),
				caBundleKey:        testCABundle(t),
			},
		}).
		Build()
	registry := NewRegistry(nil, nil)
	return k8sClient, registry, &Reconciler{
		Client:    k8sClient,
		APIReader: k8sClient,
		RunClient: k8sClient,
		Namespace: "test-namespace",
		Targets:   registry,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func reconcileSpokeCluster(t *testing.T, reconciler *Reconciler, name string) {
	t.Helper()
	if _, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name},
	}); err != nil {
		t.Fatalf("reconciling SpokeCluster %q: %v", name, err)
	}
}

func hasSpokeTarget(registry *Registry, name string) bool {
	for _, target := range registry.Targets() {
		if target.Name == name {
			return true
		}
	}
	return false
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
