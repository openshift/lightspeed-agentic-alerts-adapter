package agenticrun

import (
	"strings"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"github.com/prometheus/alertmanager/api/v2/models"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/config"
)

func strPtr(s string) *string { return &s }

func makeAlert(alertName, namespace, fingerprint, severity string) *models.GettableAlert {
	startsAt := strfmt.DateTime(time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC))
	return &models.GettableAlert{
		Alert: models.Alert{
			Labels: models.LabelSet{
				"alertname": alertName,
				"namespace": namespace,
				"severity":  severity,
			},
			GeneratorURL: "http://prometheus:9090/graph",
		},
		Annotations: models.LabelSet{
			"summary":     "Pod is crash looping",
			"description": "Pod my-pod has restarted 5 times in the last hour",
		},
		Fingerprint: strPtr(fingerprint),
		StartsAt:    &startsAt,
		Status: &models.AlertStatus{
			State: strPtr("active"),
		},
	}
}

func TestBuild(t *testing.T) {
	noIgnored := []string{}

	tests := []struct {
		name              string
		alert             *models.GettableAlert
		ignoredLabels     []string
		expectedName      string
		expectedNamespace string
		expectedTargetNS  []string
		expectedLabels    map[string]string
	}{
		{
			name:              "namespaced alert",
			alert:             makeAlert("KubePodCrashLooping", "production", "abcdef1234567890", "critical"),
			ignoredLabels:     noIgnored,
			expectedName:      "kubepodcrashlooping-production-895c8977",
			expectedNamespace: runNamespace,
			expectedTargetNS:  []string{"production"},
			expectedLabels: map[string]string{
				LabelSource:      sourceValue,
				LabelFingerprint: "abcdef12",
				LabelDedupFingerprint: StableFingerprint(map[string]string{
					"alertname": "KubePodCrashLooping",
					"namespace": "production",
					"severity":  "critical",
				}, noIgnored),
				LabelAlertName: "kubepodcrashlooping",
				LabelSeverity:  "critical",
			},
		},
		{
			name: "cluster-scoped alert (no namespace)",
			alert: func() *models.GettableAlert {
				a := makeAlert("ClusterVersionAvailable", "", "ff00ff00ff00ff00", "info")
				delete(a.Labels, "namespace")
				return a
			}(),
			ignoredLabels:     noIgnored,
			expectedName:      "clusterversionavailable-895c8977",
			expectedNamespace: runNamespace,
			expectedTargetNS:  nil,
			expectedLabels: map[string]string{
				LabelSource:      sourceValue,
				LabelFingerprint: "ff00ff00",
				LabelDedupFingerprint: StableFingerprint(map[string]string{
					"alertname": "ClusterVersionAvailable",
					"severity":  "info",
				}, noIgnored),
				LabelAlertName: "clusterversionavailable",
				LabelSeverity:  "info",
			},
		},
		{
			name:              "ignored labels stripped from stable fingerprint",
			alert:             makeAlert("TestAlert", "ns", "abc", "warning"),
			ignoredLabels:     []string{"pod", "instance"},
			expectedName:      "testalert-ns-895c8977",
			expectedNamespace: runNamespace,
			expectedTargetNS:  []string{"ns"},
			expectedLabels: map[string]string{
				LabelSource:      sourceValue,
				LabelFingerprint: "abc",
				LabelDedupFingerprint: StableFingerprint(map[string]string{
					"alertname": "TestAlert",
					"namespace": "ns",
					"severity":  "warning",
				}, []string{"pod", "instance"}),
				LabelAlertName: "testalert",
				LabelSeverity:  "warning",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Build(tt.alert, config.ToolsConfig{}, config.AgentConfig{}, tt.ignoredLabels)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if p.Name != tt.expectedName {
				t.Errorf("name = %q, want %q", p.Name, tt.expectedName)
			}
			if p.Namespace != tt.expectedNamespace {
				t.Errorf("namespace = %q, want %q", p.Namespace, tt.expectedNamespace)
			}

			if len(tt.expectedTargetNS) == 0 && len(p.Spec.TargetNamespaces) != 0 {
				t.Errorf("targetNamespaces = %v, want empty", p.Spec.TargetNamespaces)
			}
			if len(tt.expectedTargetNS) > 0 {
				if len(p.Spec.TargetNamespaces) != len(tt.expectedTargetNS) {
					t.Fatalf("targetNamespaces length = %d, want %d", len(p.Spec.TargetNamespaces), len(tt.expectedTargetNS))
				}
				for i, ns := range tt.expectedTargetNS {
					if p.Spec.TargetNamespaces[i] != ns {
						t.Errorf("targetNamespaces[%d] = %q, want %q", i, p.Spec.TargetNamespaces[i], ns)
					}
				}
			}

			for k, v := range tt.expectedLabels {
				if got := p.Labels[k]; got != v {
					t.Errorf("label %q = %q, want %q", k, got, v)
				}
			}
		})
	}
}

func TestBuildNilFingerprint(t *testing.T) {
	a := makeAlert("TestAlert", "ns", "abcdef12", "warning")
	a.Fingerprint = nil

	_, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
	if err == nil {
		t.Fatal("expected error for nil fingerprint, got nil")
	}

	want := "fingerprint is nil"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err, want)
	}
}

func TestBuildNilStartsAt(t *testing.T) {
	a := makeAlert("TestAlert", "ns", "abcdef12", "warning")
	a.StartsAt = nil

	_, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
	if err == nil {
		t.Fatal("expected error for nil startsAt, got nil")
	}

	want := "startsAt is nil"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err, want)
	}
}

func TestBuildWorkflowSteps(t *testing.T) {
	a := makeAlert("TestAlert", "ns", "abcdef12", "warning")
	p, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Spec.Analysis.Agent != defaultAgent {
		t.Errorf("analysis.agent = %q, want %q", p.Spec.Analysis.Agent, defaultAgent)
	}
	if p.Spec.Execution.Agent != defaultAgent {
		t.Errorf("execution.agent = %q, want %q", p.Spec.Execution.Agent, defaultAgent)
	}
	if p.Spec.Verification.Agent != defaultAgent {
		t.Errorf("verification.agent = %q, want %q", p.Spec.Verification.Agent, defaultAgent)
	}
}

func TestBuildDeterministicNaming(t *testing.T) {
	a := makeAlert("KubePodCrashLooping", "production", "abcdef1234567890", "critical")

	p1, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	p2, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
	if err != nil {
		t.Fatalf("second build: %v", err)
	}

	if p1.Name != p2.Name {
		t.Errorf("names differ: %q vs %q", p1.Name, p2.Name)
	}
}

func TestBuildAnnotations(t *testing.T) {
	t.Run("starts-at is RFC3339 UTC", func(t *testing.T) {
		a := makeAlert("TestAlert", "ns", "abcdef12", "warning")
		p, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := p.Annotations[AnnotStartsAt]
		expected := "2026-01-15T10:30:00Z"
		if got != expected {
			t.Errorf("starts-at = %q, want %q", got, expected)
		}
	})

	t.Run("summary is included", func(t *testing.T) {
		a := makeAlert("TestAlert", "ns", "abcdef12", "warning")
		p, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := p.Annotations[AnnotSummary]; got != "Pod is crash looping" {
			t.Errorf("summary = %q, want %q", got, "Pod is crash looping")
		}
	})

	t.Run("missing annotations produce empty map entries", func(t *testing.T) {
		a := makeAlert("TestAlert", "ns", "abcdef12", "warning")
		a.Annotations = models.LabelSet{}
		startsAt := strfmt.DateTime(time.Time{})
		a.StartsAt = &startsAt

		p, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := p.Annotations[AnnotStartsAt]; ok {
			t.Error("expected no starts-at annotation for zero time")
		}
		if _, ok := p.Annotations[AnnotSummary]; ok {
			t.Error("expected no summary annotation when empty")
		}
	})

	t.Run("long summary is truncated", func(t *testing.T) {
		a := makeAlert("TestAlert", "ns", "abcdef12", "warning")
		a.Annotations["summary"] = strings.Repeat("x", 300)

		p, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := len(p.Annotations[AnnotSummary]); got != maxSummaryLen {
			t.Errorf("summary length = %d, want %d", got, maxSummaryLen)
		}
	})
}

func TestBuildRequest(t *testing.T) {
	a := makeAlert("KubePodCrashLooping", "production", "abcdef12", "critical")
	a.Annotations["runbook_url"] = "https://runbooks.example.com/KubePodCrashLooping"

	p, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{
		"Alert: KubePodCrashLooping (critical)",
		"Namespace: production",
		"Runbook URL: https://runbooks.example.com/KubePodCrashLooping",
		"Description: Pod my-pod has restarted 5 times in the last hour",
		"alertname: KubePodCrashLooping",
	} {
		if !strings.Contains(p.Spec.Request, want) {
			t.Errorf("request does not contain %q\nfull request:\n%s", want, p.Spec.Request)
		}
	}
}

func TestBuildRequestWithoutRunbook(t *testing.T) {
	a := makeAlert("TestAlert", "ns", "abcdef12", "warning")
	p, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(p.Spec.Request, "Runbook URL:") {
		t.Error("request should not contain Runbook URL when not set")
	}
}

func TestBuildName(t *testing.T) {
	testTime := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	hash := startsAtHash(testTime)

	tests := []struct {
		name      string
		alertName string
		namespace string
		startsAt  time.Time
		expected  string
	}{
		{
			name:      "standard namespaced",
			alertName: "KubePodCrashLooping",
			namespace: "production",
			startsAt:  testTime,
			expected:  "kubepodcrashlooping-production-" + hash,
		},
		{
			name:      "cluster-scoped",
			alertName: "ClusterReady",
			namespace: "",
			startsAt:  testTime,
			expected:  "clusterready-" + hash,
		},
		{
			name:      "invalid characters replaced",
			alertName: "Alert_With:Special/Chars",
			namespace: "my ns!",
			startsAt:  testTime,
			expected:  "alert-with-special-chars-my-ns--" + hash,
		},
		{
			name:      "long name truncated to 63 chars with namespace",
			alertName: "AlertmanagerReceiversNotConfigured",
			namespace: "openshift-monitoring",
			startsAt:  testTime,
			expected:  "alertmanagerreceiversnotconfigure-openshift-monitoring-" + hash,
		},
		{
			name:      "long alert name truncated with namespace",
			alertName: strings.Repeat("a", 250),
			namespace: "ns",
			startsAt:  testTime,
			expected:  strings.Repeat("a", maxLabelValueLen-len("-ns-"+hash)) + "-ns-" + hash,
		},
		{
			name:      "long alert name truncated without namespace",
			alertName: strings.Repeat("a", 250),
			namespace: "",
			startsAt:  testTime,
			expected:  strings.Repeat("a", maxLabelValueLen-len("-"+hash)) + "-" + hash,
		},
		{
			name:      "different startsAt produces different name",
			alertName: "KubePodCrashLooping",
			namespace: "production",
			startsAt:  testTime.Add(time.Hour),
			expected:  "kubepodcrashlooping-production-" + startsAtHash(testTime.Add(time.Hour)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildName(tt.alertName, tt.namespace, tt.startsAt)
			if got != tt.expected {
				t.Errorf("buildName() = %q, want %q", got, tt.expected)
			}
			if len(got) > maxLabelValueLen {
				t.Errorf("name length %d exceeds max %d", len(got), maxLabelValueLen)
			}
		})
	}
}

func TestSanitizeLabelValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid value unchanged",
			input:    "my-valid-label",
			expected: "my-valid-label",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "special characters replaced",
			input:    "alert/name:with@chars",
			expected: "alert-name-with-chars",
		},
		{
			name:     "truncated to 63 chars",
			input:    strings.Repeat("x", 70),
			expected: strings.Repeat("x", 63),
		},
		{
			name:     "leading non-alphanumeric trimmed",
			input:    "---value",
			expected: "value",
		},
		{
			name:     "trailing non-alphanumeric trimmed",
			input:    "value---",
			expected: "value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeLabelValue(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeLabelValue(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestBuildTypeMeta(t *testing.T) {
	a := makeAlert("TestAlert", "ns", "abcdef12", "warning")
	p, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.APIVersion != "agentic.openshift.io/v1alpha1" {
		t.Errorf("apiVersion = %q, want %q", p.APIVersion, "agentic.openshift.io/v1alpha1")
	}
	if p.Kind != "AgenticRun" {
		t.Errorf("kind = %q, want %q", p.Kind, "AgenticRun")
	}
}

func TestBuildWithTools(t *testing.T) {
	a := makeAlert("TestAlert", "ns", "abcdef12", "warning")

	t.Run("empty tools config omits all tools", func(t *testing.T) {
		p, err := Build(a, config.ToolsConfig{}, config.AgentConfig{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !p.Spec.Tools.IsZero() {
			t.Errorf("expected zero spec.tools, got %+v", p.Spec.Tools)
		}
		if !p.Spec.Analysis.Tools.IsZero() {
			t.Errorf("expected zero analysis.tools, got %+v", p.Spec.Analysis.Tools)
		}
		if !p.Spec.Execution.Tools.IsZero() {
			t.Errorf("expected zero execution.tools, got %+v", p.Spec.Execution.Tools)
		}
		if !p.Spec.Verification.Tools.IsZero() {
			t.Errorf("expected zero verification.tools, got %+v", p.Spec.Verification.Tools)
		}
	})

	t.Run("shared skills only sets spec.tools", func(t *testing.T) {
		tc := config.ToolsConfig{
			Shared: []agenticv1alpha1.SkillsSource{
				{Image: "registry.example.com/skills:latest", Paths: []string{"/skills/prometheus"}},
			},
		}
		p, err := Build(a, tc, config.AgentConfig{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(p.Spec.Tools.Skills) != 1 {
			t.Fatalf("spec.tools.skills length = %d, want 1", len(p.Spec.Tools.Skills))
		}
		if p.Spec.Tools.Skills[0].Image != "registry.example.com/skills:latest" {
			t.Errorf("spec.tools.skills[0].image = %q, want %q", p.Spec.Tools.Skills[0].Image, "registry.example.com/skills:latest")
		}
		if !p.Spec.Analysis.Tools.IsZero() {
			t.Errorf("expected zero analysis.tools, got %+v", p.Spec.Analysis.Tools)
		}
		if !p.Spec.Execution.Tools.IsZero() {
			t.Errorf("expected zero execution.tools, got %+v", p.Spec.Execution.Tools)
		}
		if !p.Spec.Verification.Tools.IsZero() {
			t.Errorf("expected zero verification.tools, got %+v", p.Spec.Verification.Tools)
		}
	})

	t.Run("per-step skills only sets step tools", func(t *testing.T) {
		tc := config.ToolsConfig{
			Analysis: []agenticv1alpha1.SkillsSource{
				{Image: "registry.example.com/analysis:latest", Paths: []string{"/skills/diagnostic"}},
			},
			Execution: []agenticv1alpha1.SkillsSource{
				{Image: "registry.example.com/exec:latest", Paths: []string{"/skills/remediation"}},
			},
			Verification: []agenticv1alpha1.SkillsSource{
				{Image: "registry.example.com/verify:latest", Paths: []string{"/skills/validation"}},
			},
		}
		p, err := Build(a, tc, config.AgentConfig{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !p.Spec.Tools.IsZero() {
			t.Errorf("expected zero spec.tools, got %+v", p.Spec.Tools)
		}
		if len(p.Spec.Analysis.Tools.Skills) != 1 {
			t.Fatalf("analysis.tools.skills length = %d, want 1", len(p.Spec.Analysis.Tools.Skills))
		}
		if p.Spec.Analysis.Tools.Skills[0].Image != "registry.example.com/analysis:latest" {
			t.Errorf("analysis.tools.skills[0].image = %q, want %q", p.Spec.Analysis.Tools.Skills[0].Image, "registry.example.com/analysis:latest")
		}
		if len(p.Spec.Execution.Tools.Skills) != 1 {
			t.Fatalf("execution.tools.skills length = %d, want 1", len(p.Spec.Execution.Tools.Skills))
		}
		if p.Spec.Execution.Tools.Skills[0].Image != "registry.example.com/exec:latest" {
			t.Errorf("execution.tools.skills[0].image = %q, want %q", p.Spec.Execution.Tools.Skills[0].Image, "registry.example.com/exec:latest")
		}
		if len(p.Spec.Verification.Tools.Skills) != 1 {
			t.Fatalf("verification.tools.skills length = %d, want 1", len(p.Spec.Verification.Tools.Skills))
		}
		if p.Spec.Verification.Tools.Skills[0].Image != "registry.example.com/verify:latest" {
			t.Errorf("verification.tools.skills[0].image = %q, want %q", p.Spec.Verification.Tools.Skills[0].Image, "registry.example.com/verify:latest")
		}
	})

	t.Run("shared and per-step skills combined", func(t *testing.T) {
		tc := config.ToolsConfig{
			Shared: []agenticv1alpha1.SkillsSource{
				{Image: "registry.example.com/shared:latest", Paths: []string{"/skills/common"}},
			},
			Analysis: []agenticv1alpha1.SkillsSource{
				{Image: "registry.example.com/analysis:latest", Paths: []string{"/skills/diagnostic"}},
			},
		}
		p, err := Build(a, tc, config.AgentConfig{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(p.Spec.Tools.Skills) != 1 {
			t.Fatalf("spec.tools.skills length = %d, want 1", len(p.Spec.Tools.Skills))
		}
		if p.Spec.Tools.Skills[0].Image != "registry.example.com/shared:latest" {
			t.Errorf("spec.tools.skills[0].image = %q, want %q", p.Spec.Tools.Skills[0].Image, "registry.example.com/shared:latest")
		}
		if len(p.Spec.Analysis.Tools.Skills) != 1 {
			t.Fatalf("analysis.tools.skills length = %d, want 1", len(p.Spec.Analysis.Tools.Skills))
		}
		if p.Spec.Analysis.Tools.Skills[0].Image != "registry.example.com/analysis:latest" {
			t.Errorf("analysis.tools.skills[0].image = %q, want %q", p.Spec.Analysis.Tools.Skills[0].Image, "registry.example.com/analysis:latest")
		}
		if !p.Spec.Execution.Tools.IsZero() {
			t.Errorf("expected zero execution.tools, got %+v", p.Spec.Execution.Tools)
		}
		if !p.Spec.Verification.Tools.IsZero() {
			t.Errorf("expected zero verification.tools, got %+v", p.Spec.Verification.Tools)
		}
	})

	t.Run("per-step skills preserves agent", func(t *testing.T) {
		tc := config.ToolsConfig{
			Analysis: []agenticv1alpha1.SkillsSource{
				{Image: "registry.example.com/analysis:latest", Paths: []string{"/skills/diagnostic"}},
			},
		}
		p, err := Build(a, tc, config.AgentConfig{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Spec.Analysis.Agent != defaultAgent {
			t.Errorf("analysis.agent = %q, want %q", p.Spec.Analysis.Agent, defaultAgent)
		}
	})
}

func TestStableFingerprint(t *testing.T) {
	t.Run("no ignored labels present", func(t *testing.T) {
		labels := map[string]string{
			"alertname": "HighCPU",
			"namespace": "myns",
			"container": "app",
		}
		fp := StableFingerprint(labels, []string{"pod", "instance", "endpoint", "uid"})
		if len(fp) != 8 {
			t.Errorf("fingerprint length = %d, want %d", len(fp), 8)
		}
		if fp == "" {
			t.Error("fingerprint is empty")
		}
	})

	t.Run("ignored labels stripped", func(t *testing.T) {
		labels := map[string]string{
			"alertname": "KubePodCrashLooping",
			"namespace": "myns",
			"pod":       "app-abc123",
			"container": "app",
		}
		fp := StableFingerprint(labels, []string{"pod", "instance", "endpoint", "uid"})

		labelsWithout := map[string]string{
			"alertname": "KubePodCrashLooping",
			"namespace": "myns",
			"container": "app",
		}
		fpWithout := StableFingerprint(labelsWithout, []string{"pod", "instance", "endpoint", "uid"})

		if fp != fpWithout {
			t.Errorf("fingerprint with pod (%s) != fingerprint without pod (%s)", fp, fpWithout)
		}
	})

	t.Run("two alerts differing only in ignored labels produce same hash", func(t *testing.T) {
		labelsA := map[string]string{
			"alertname": "X",
			"namespace": "ns",
			"pod":       "pod-aaa",
		}
		labelsB := map[string]string{
			"alertname": "X",
			"namespace": "ns",
			"pod":       "pod-bbb",
		}
		fpA := StableFingerprint(labelsA, []string{"pod"})
		fpB := StableFingerprint(labelsB, []string{"pod"})
		if fpA != fpB {
			t.Errorf("fingerprints differ: %s vs %s", fpA, fpB)
		}
	})

	t.Run("differing in non-ignored labels produce different hash", func(t *testing.T) {
		labelsA := map[string]string{
			"alertname": "X",
			"namespace": "ns",
			"container": "foo",
		}
		labelsB := map[string]string{
			"alertname": "X",
			"namespace": "ns",
			"container": "bar",
		}
		fpA := StableFingerprint(labelsA, []string{"pod"})
		fpB := StableFingerprint(labelsB, []string{"pod"})
		if fpA == fpB {
			t.Errorf("fingerprints should differ but both are %s", fpA)
		}
	})

	t.Run("empty ignored list includes all labels", func(t *testing.T) {
		labels := map[string]string{
			"alertname": "X",
			"namespace": "ns",
			"pod":       "pod-aaa",
		}
		fpAll := StableFingerprint(labels, []string{})

		labelsNoPod := map[string]string{
			"alertname": "X",
			"namespace": "ns",
		}
		fpNoPod := StableFingerprint(labelsNoPod, []string{})

		if fpAll == fpNoPod {
			t.Errorf("fingerprints should differ when all labels included, but both are %s", fpAll)
		}
	})

	t.Run("deterministic across calls", func(t *testing.T) {
		labels := map[string]string{
			"alertname": "HighCPU",
			"namespace": "myns",
		}
		fp1 := StableFingerprint(labels, []string{"pod"})
		fp2 := StableFingerprint(labels, []string{"pod"})
		if fp1 != fp2 {
			t.Errorf("fingerprints differ across calls: %s vs %s", fp1, fp2)
		}
	})
}

func TestBuildWithAgentOverrides(t *testing.T) {
	tests := []struct {
		name             string
		agent            config.AgentConfig
		wantAnalysis     string
		wantExecution    string
		wantVerification string
	}{
		{
			name:             "no agent config uses default for all steps",
			agent:            config.AgentConfig{},
			wantAnalysis:     defaultAgent,
			wantExecution:    defaultAgent,
			wantVerification: defaultAgent,
		},
		{
			name:             "global agent override applies to all steps",
			agent:            config.AgentConfig{Default: "my-agent"},
			wantAnalysis:     "my-agent",
			wantExecution:    "my-agent",
			wantVerification: "my-agent",
		},
		{
			name: "per-step overrides take precedence over global",
			agent: config.AgentConfig{
				Default:      "global-agent",
				Analysis:     "analyzer",
				Execution:    "executor",
				Verification: "verifier",
			},
			wantAnalysis:     "analyzer",
			wantExecution:    "executor",
			wantVerification: "verifier",
		},
		{
			name: "mixed config uses per-step where set and global elsewhere",
			agent: config.AgentConfig{
				Default:  "global-agent",
				Analysis: "analyzer",
			},
			wantAnalysis:     "analyzer",
			wantExecution:    "global-agent",
			wantVerification: "global-agent",
		},
		{
			name:             "per-step only without global falls back to hardcoded default",
			agent:            config.AgentConfig{Analysis: "analyzer"},
			wantAnalysis:     "analyzer",
			wantExecution:    defaultAgent,
			wantVerification: defaultAgent,
		},
	}

	a := makeAlert("TestAlert", "ns", "abcdef12", "warning")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Build(a, config.ToolsConfig{}, tt.agent, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Spec.Analysis.Agent != tt.wantAnalysis {
				t.Errorf("analysis.agent = %q, want %q", p.Spec.Analysis.Agent, tt.wantAnalysis)
			}
			if p.Spec.Execution.Agent != tt.wantExecution {
				t.Errorf("execution.agent = %q, want %q", p.Spec.Execution.Agent, tt.wantExecution)
			}
			if p.Spec.Verification.Agent != tt.wantVerification {
				t.Errorf("verification.agent = %q, want %q", p.Spec.Verification.Agent, tt.wantVerification)
			}
		})
	}
}
