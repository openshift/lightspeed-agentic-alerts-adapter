package multicluster

import (
	"context"
	"io"
	"log/slog"
	"testing"

	hubv1alpha1 "github.com/openshift/lightspeed-hub/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildTargetSkipsInvalidCredentials(t *testing.T) {
	tests := []struct {
		name string
		data map[string][]byte
	}{
		{
			name: "missing alertmanager URL",
			data: map[string][]byte{
				tokenKey:    []byte("token"),
				caBundleKey: []byte("ca"),
			},
		},
		{
			name: "empty alertmanager URL",
			data: map[string][]byte{
				alertmanagerURLKey: []byte{},
				tokenKey:           []byte("token"),
				caBundleKey:        []byte("ca"),
			},
		},
		{
			name: "missing token",
			data: map[string][]byte{
				alertmanagerURLKey: []byte("https://alertmanager.example.com"),
				caBundleKey:        []byte("ca"),
			},
		},
		{
			name: "empty token",
			data: map[string][]byte{
				alertmanagerURLKey: []byte("https://alertmanager.example.com"),
				tokenKey:           []byte{},
				caBundleKey:        []byte("ca"),
			},
		},
		{
			name: "missing CA bundle",
			data: map[string][]byte{
				alertmanagerURLKey: []byte("https://alertmanager.example.com"),
				tokenKey:           []byte("token"),
			},
		},
		{
			name: "empty CA bundle",
			data: map[string][]byte{
				alertmanagerURLKey: []byte("https://alertmanager.example.com"),
				tokenKey:           []byte("token"),
				caBundleKey:        []byte{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
						Name:      "credentials",
						Namespace: "test-namespace",
					},
					Data: tt.data,
				}).
				Build()
			spokeCluster := &hubv1alpha1.SpokeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "spoke",
					Labels: map[string]string{
						CredentialSecretLabel: "credentials",
					},
				},
			}

			_, ok := BuildTarget(
				context.Background(),
				k8sClient,
				k8sClient,
				"test-namespace",
				spokeCluster,
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			if ok {
				t.Fatal("BuildTarget() succeeded, want false")
			}
		})
	}
}
