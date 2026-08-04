package agenticrun

import (
	"io"
	"log/slog"
	"testing"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestClient(t *testing.T) *Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := agenticv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("registering scheme: %v", err)
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Client{Client: fc, logger: logger}
}

func TestCreateAgenticRun(t *testing.T) {
	c := newTestClient(t)

	p := &agenticv1alpha1.AgenticRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-run-abcdef12",
			Namespace: runNamespace,
		},
		Spec: agenticv1alpha1.AgenticRunSpec{
			Request:  "test request",
			Analysis: agenticv1alpha1.AgenticRunStep{Agent: defaultAgent},
		},
	}

	created, err := c.CreateAgenticRun(t.Context(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatal("expected created=true for new run")
	}

	var got agenticv1alpha1.AgenticRun
	key := p.ObjectMeta
	if err := c.Get(t.Context(), toObjectKey(key), &got); err != nil {
		t.Fatalf("failed to get created run: %v", err)
	}

	if got.Spec.Request != "test request" {
		t.Errorf("request = %q, want %q", got.Spec.Request, "test request")
	}
}

func TestCreateAgenticRunAlreadyExists(t *testing.T) {
	c := newTestClient(t)

	p := &agenticv1alpha1.AgenticRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-run-abcdef12",
			Namespace: runNamespace,
		},
		Spec: agenticv1alpha1.AgenticRunSpec{
			Request:  "test request",
			Analysis: agenticv1alpha1.AgenticRunStep{Agent: defaultAgent},
		},
	}

	if _, err := c.CreateAgenticRun(t.Context(), p); err != nil {
		t.Fatalf("first create: %v", err)
	}

	duplicate := &agenticv1alpha1.AgenticRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-run-abcdef12",
			Namespace: runNamespace,
		},
		Spec: agenticv1alpha1.AgenticRunSpec{
			Request:  "test request",
			Analysis: agenticv1alpha1.AgenticRunStep{Agent: defaultAgent},
		},
	}
	created, err := c.CreateAgenticRun(t.Context(), duplicate)
	if err != nil {
		t.Fatalf("duplicate create should not error, got: %v", err)
	}
	if created {
		t.Fatal("expected created=false for duplicate run")
	}
}

func TestListAgenticRuns(t *testing.T) {
	c := newTestClient(t)

	matching := &agenticv1alpha1.AgenticRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matching-abcdef12",
			Namespace: runNamespace,
			Labels:    map[string]string{LabelSource: sourceValue},
		},
		Spec: agenticv1alpha1.AgenticRunSpec{
			Request:  "matching",
			Analysis: agenticv1alpha1.AgenticRunStep{Agent: defaultAgent},
		},
	}
	unrelated := &agenticv1alpha1.AgenticRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unrelated-12345678",
			Namespace: runNamespace,
			Labels:    map[string]string{LabelSource: "other"},
		},
		Spec: agenticv1alpha1.AgenticRunSpec{
			Request:  "unrelated",
			Analysis: agenticv1alpha1.AgenticRunStep{Agent: defaultAgent},
		},
	}

	if err := c.Create(t.Context(), matching); err != nil {
		t.Fatalf("creating matching run: %v", err)
	}
	if err := c.Create(t.Context(), unrelated); err != nil {
		t.Fatalf("creating unrelated run: %v", err)
	}

	runs, err := c.ListAgenticRuns(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}
	if runs[0].Name != "matching-abcdef12" {
		t.Errorf("name = %q, want %q", runs[0].Name, "matching-abcdef12")
	}
}

func TestListAgenticRunsEmpty(t *testing.T) {
	c := newTestClient(t)

	runs, err := c.ListAgenticRuns(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("got %d runs, want 0", len(runs))
	}
}

func toObjectKey(meta metav1.ObjectMeta) types.NamespacedName {
	return types.NamespacedName{Name: meta.Name, Namespace: meta.Namespace}
}
