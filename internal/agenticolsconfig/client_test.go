package agenticolsconfig

import (
	"context"
	"testing"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSuspended(t *testing.T) {
	tests := []struct {
		name string
		obj  *agenticv1alpha1.AgenticOLSConfig
		want bool
	}{
		{
			name: "suspended",
			obj: &agenticv1alpha1.AgenticOLSConfig{
				ObjectMeta: metav1.ObjectMeta{Name: ConfigName},
				Spec:       agenticv1alpha1.AgenticOLSConfigSpec{Suspended: true},
			},
			want: true,
		},
		{
			name: "not suspended",
			obj: &agenticv1alpha1.AgenticOLSConfig{
				ObjectMeta: metav1.ObjectMeta{Name: ConfigName},
				Spec:       agenticv1alpha1.AgenticOLSConfigSpec{Suspended: false},
			},
			want: false,
		},
		{
			name: "missing config means not suspended",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := agenticv1alpha1.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme() error = %v", err)
			}

			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.obj != nil {
				builder.WithObjects(tt.obj)
			}

			c := NewClient(builder.Build())
			got, err := c.Suspended(context.Background())
			if err != nil {
				t.Fatalf("Suspended() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Suspended() = %v, want %v", got, tt.want)
			}
		})
	}
}
