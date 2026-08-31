package adapter

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"github.com/prometheus/alertmanager/api/v2/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/config"
)

type fakeAlertSource struct {
	alerts models.GettableAlerts
	err    error
}

func (f *fakeAlertSource) GetAlerts(_ context.Context) (models.GettableAlerts, error) {
	return f.alerts, f.err
}

type fakeRunClient struct {
	runs        []agenticv1alpha1.AgenticRun
	listErr     error
	createErr   error
	created     []*agenticv1alpha1.AgenticRun
	createCalls int
	wasCreated  *bool
}

func (f *fakeRunClient) ListAgenticRuns(_ context.Context) ([]agenticv1alpha1.AgenticRun, error) {
	return f.runs, f.listErr
}

func (f *fakeRunClient) CreateAgenticRun(_ context.Context, p *agenticv1alpha1.AgenticRun) (bool, error) {
	f.createCalls++
	if f.createErr != nil {
		return false, f.createErr
	}
	if f.wasCreated != nil && !*f.wasCreated {
		return false, nil
	}
	f.created = append(f.created, p)
	return true, nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func ptr[T any](v T) *T { return &v }

func defaultTestConfig() config.Config {
	return config.Config{
		PollInterval:     config.DefaultPollInterval,
		PreRunDelay:      5 * time.Minute,
		PostRunDelay:     1 * time.Hour,
		AllowedReceivers: []string{"critical"},
		IgnoredLabels:    config.DefaultIgnoredLabels,
	}
}

func stableFP(labels models.LabelSet) string {
	return agenticrun.StableFingerprint(labels, config.DefaultIgnoredLabels)
}

func makeAlert(name, fingerprint string, startsAt time.Time) *models.GettableAlert {
	return makeAlertWithSeverity(name, fingerprint, startsAt, "warning")
}

func makeAlertWithSeverity(name, fingerprint string, startsAt time.Time, severity string) *models.GettableAlert {
	sa := strfmt.DateTime(startsAt)
	labels := models.LabelSet{"alertname": name}
	if severity != "" {
		labels["severity"] = severity
	}
	return &models.GettableAlert{
		Fingerprint: &fingerprint,
		StartsAt:    &sa,
		Receivers:   []*models.ReceiverReference{{Name: ptr("Critical")}},
		Alert: models.Alert{
			Labels: labels,
		},
		Annotations: models.LabelSet{"summary": "test alert"},
	}
}

func makeRun(fingerprint string, conditions []metav1.Condition) agenticv1alpha1.AgenticRun {
	return agenticv1alpha1.AgenticRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-" + fingerprint,
			Namespace: "openshift-lightspeed",
			Labels: map[string]string{
				agenticrun.LabelDedupFingerprint: fingerprint,
				agenticrun.LabelSource:           "alertmanager",
			},
		},
		Status: agenticv1alpha1.AgenticRunStatus{
			Conditions: conditions,
		},
	}
}

func TestReconcile(t *testing.T) {
	now := time.Now()
	oldEnough := now.Add(-10 * time.Minute)
	tooRecent := now.Add(-2 * time.Minute)
	withinPostDelay := now.Add(-30 * time.Minute)
	pastPostDelay := now.Add(-2 * time.Hour)

	highCPUFP := stableFP(models.LabelSet{"alertname": "HighCPU", "severity": "warning"})

	tests := []struct {
		name            string
		alerts          models.GettableAlerts
		runs            []agenticv1alpha1.AgenticRun
		wantCreated     int
		wantCreateCalls int
		alertsErr       error
		runsErr         error
		createErr       error
		wasCreated      *bool
	}{
		{
			name:            "new alert creates run",
			alerts:          models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", oldEnough)},
			wantCreated:     1,
			wantCreateCalls: 1,
		},
		{
			name:        "transient alert skipped (pre-run delay)",
			alerts:      models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", tooRecent)},
			wantCreated: 0,
		},
		{
			name:   "active run skipped",
			alerts: models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", oldEnough)},
			runs: []agenticv1alpha1.AgenticRun{
				makeRun(highCPUFP, []metav1.Condition{
					{Type: "Analyzed", Status: metav1.ConditionUnknown},
				}),
			},
			wantCreated: 0,
		},
		{
			name:   "terminal run within post-run delay skipped",
			alerts: models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", oldEnough)},
			runs: []agenticv1alpha1.AgenticRun{
				makeRun(highCPUFP, []metav1.Condition{
					{Type: "Analyzed", Status: metav1.ConditionTrue},
					{Type: "Executed", Status: metav1.ConditionTrue},
					{
						Type:               "Verified",
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(withinPostDelay),
					},
				}),
			},
			wantCreated: 0,
		},
		{
			name:   "terminal run past post-run delay creates new run",
			alerts: models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", oldEnough)},
			runs: []agenticv1alpha1.AgenticRun{
				makeRun(highCPUFP, []metav1.Condition{
					{Type: "Analyzed", Status: metav1.ConditionTrue},
					{Type: "Executed", Status: metav1.ConditionTrue},
					{
						Type:               "Verified",
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(pastPostDelay),
					},
				}),
			},
			wantCreated:     1,
			wantCreateCalls: 1,
		},
		{
			name:   "failed run within post-run delay skipped",
			alerts: models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", oldEnough)},
			runs: []agenticv1alpha1.AgenticRun{
				makeRun(highCPUFP, []metav1.Condition{
					{
						Type:               "Analyzed",
						Status:             metav1.ConditionFalse,
						LastTransitionTime: metav1.NewTime(withinPostDelay),
					},
				}),
			},
			wantCreated: 0,
		},
		{
			name:   "denied run within post-run delay skipped",
			alerts: models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", oldEnough)},
			runs: []agenticv1alpha1.AgenticRun{
				makeRun(highCPUFP, []metav1.Condition{
					{
						Type:               "Denied",
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(withinPostDelay),
					},
				}),
			},
			wantCreated: 0,
		},
		{
			name:   "escalated run within post-run delay skipped",
			alerts: models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", oldEnough)},
			runs: []agenticv1alpha1.AgenticRun{
				makeRun(highCPUFP, []metav1.Condition{
					{
						Type:               "Escalated",
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(withinPostDelay),
					},
				}),
			},
			wantCreated: 0,
		},
		{
			name:        "alertmanager error skips cycle",
			alertsErr:   errors.New("connection refused"),
			wantCreated: 0,
		},
		{
			name:        "kubernetes list error skips cycle",
			alerts:      models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", oldEnough)},
			runsErr:     errors.New("api server unavailable"),
			wantCreated: 0,
		},
		{
			name: "kubernetes create error does not block other alerts",
			alerts: models.GettableAlerts{
				makeAlert("HighCPU", "aaaa111122223333", oldEnough),
				makeAlert("HighMem", "bbbb444455556666", oldEnough),
			},
			createErr:       errors.New("api server error"),
			wantCreated:     0,
			wantCreateCalls: 2,
		},
		{
			name: "nil startsAt skips alert",
			alerts: models.GettableAlerts{
				{
					Fingerprint: ptr("abcdef1234567890"),
					Receivers:   []*models.ReceiverReference{{Name: ptr("Critical")}},
					Alert: models.Alert{
						Labels: models.LabelSet{"alertname": "HighCPU"},
					},
				},
			},
			wantCreated: 0,
		},
		{
			name:            "already exists run not counted as created",
			alerts:          models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", oldEnough)},
			wasCreated:      ptr(false),
			wantCreated:     0,
			wantCreateCalls: 1,
		},
		{
			name: "nil fingerprint alert skipped with build error",
			alerts: models.GettableAlerts{
				{
					StartsAt:  func() *strfmt.DateTime { dt := strfmt.DateTime(oldEnough); return &dt }(),
					Receivers: []*models.ReceiverReference{{Name: ptr("Critical")}},
					Alert: models.Alert{
						Labels: models.LabelSet{"alertname": "HighCPU"},
					},
				},
			},
			wantCreated:     0,
			wantCreateCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := &fakeAlertSource{alerts: tt.alerts, err: tt.alertsErr}
			rc := &fakeRunClient{runs: tt.runs, listErr: tt.runsErr, createErr: tt.createErr, wasCreated: tt.wasCreated}

			a := &Adapter{
				alerts:    as,
				arClient:  rc,
				cfg:       defaultTestConfig(),
				namespace: agenticrun.RunNamespace,
				logger:    quietLogger(),
			}

			a.reconcile(context.Background())

			if len(rc.created) != tt.wantCreated {
				t.Errorf("created %d runs, want %d", len(rc.created), tt.wantCreated)
			}
			if rc.createCalls != tt.wantCreateCalls {
				t.Errorf("CreateAgenticRun called %d times, want %d", rc.createCalls, tt.wantCreateCalls)
			}
		})
	}
}

func TestSkipReceiver(t *testing.T) {
	tests := []struct {
		name      string
		receivers []*models.ReceiverReference
		allowed   []string
		want      bool
	}{
		{
			name:      "matching receiver",
			receivers: []*models.ReceiverReference{{Name: ptr("Critical")}},
			allowed:   []string{"critical"},
			want:      false,
		},
		{
			name:      "no matching receiver",
			receivers: []*models.ReceiverReference{{Name: ptr("Default")}},
			allowed:   []string{"critical"},
			want:      true,
		},
		{
			name:      "one of multiple receivers matches",
			receivers: []*models.ReceiverReference{{Name: ptr("Default")}, {Name: ptr("Critical")}},
			allowed:   []string{"critical"},
			want:      false,
		},
		{
			name:      "case-insensitive match",
			receivers: []*models.ReceiverReference{{Name: ptr("CRITICAL")}},
			allowed:   []string{"critical"},
			want:      false,
		},
		{
			name:      "empty receivers list",
			receivers: []*models.ReceiverReference{},
			allowed:   []string{"critical"},
			want:      true,
		},
		{
			name:      "nil receivers",
			receivers: nil,
			allowed:   []string{"critical"},
			want:      true,
		},
		{
			name:      "empty allowlist skips all",
			receivers: []*models.ReceiverReference{{Name: ptr("Critical")}},
			allowed:   []string{},
			want:      true,
		},
		{
			name:      "multiple allowed receivers",
			receivers: []*models.ReceiverReference{{Name: ptr("PagerDuty")}},
			allowed:   []string{"critical", "pagerduty"},
			want:      false,
		},
		{
			name:      "nil receiver entry skipped",
			receivers: []*models.ReceiverReference{nil, {Name: ptr("Critical")}},
			allowed:   []string{"critical"},
			want:      false,
		},
		{
			name:      "nil receiver name skipped",
			receivers: []*models.ReceiverReference{{Name: nil}, {Name: ptr("Critical")}},
			allowed:   []string{"critical"},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alert := &models.GettableAlert{
				Receivers: tt.receivers,
				Alert: models.Alert{
					Labels: models.LabelSet{"alertname": "TestAlert"},
				},
			}
			got := skipReceiver(alert, tt.allowed)
			if got != tt.want {
				t.Errorf("skipReceiver() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReconcileSkipsSeverity(t *testing.T) {
	now := time.Now()
	oldEnough := now.Add(-10 * time.Minute)

	tests := []struct {
		name            string
		severity        string
		wantCreateCalls int
	}{
		{name: "none severity processed", severity: "none", wantCreateCalls: 1},
		{name: "info severity processed", severity: "info", wantCreateCalls: 1},
		{name: "warning severity processed", severity: "warning", wantCreateCalls: 1},
		{name: "critical severity processed", severity: "critical", wantCreateCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alert := makeAlertWithSeverity("HighCPU", "abcdef1234567890", oldEnough, tt.severity)
			as := &fakeAlertSource{alerts: models.GettableAlerts{alert}}
			rc := &fakeRunClient{}

			a := &Adapter{
				alerts:    as,
				arClient:  rc,
				cfg:       defaultTestConfig(),
				namespace: agenticrun.RunNamespace,
				logger:    quietLogger(),
			}

			a.reconcile(context.Background())

			if rc.createCalls != tt.wantCreateCalls {
				t.Errorf("CreateAgenticRun called %d times, want %d", rc.createCalls, tt.wantCreateCalls)
			}
		})
	}
}

func TestReconcileWithTools(t *testing.T) {
	now := time.Now()
	oldEnough := now.Add(-10 * time.Minute)

	t.Run("shared tools set on run", func(t *testing.T) {
		as := &fakeAlertSource{alerts: models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", oldEnough)}}
		rc := &fakeRunClient{}

		cfg := config.Default()
		cfg.AllowedReceivers = []string{"critical"}
		cfg.Tools.Shared = []agenticv1alpha1.SkillsSource{
			{Image: "registry.example.com/skills:latest", Paths: []string{"/skills/prometheus"}},
		}

		a := &Adapter{
			alerts:    as,
			arClient:  rc,
			cfg:       cfg,
			namespace: agenticrun.RunNamespace,
			logger:    quietLogger(),
		}

		a.reconcile(context.Background())

		if len(rc.created) != 1 {
			t.Fatalf("created %d runs, want 1", len(rc.created))
		}
		p := rc.created[0]
		if len(p.Spec.Tools.Skills) != 1 {
			t.Fatalf("spec.tools.skills length = %d, want 1", len(p.Spec.Tools.Skills))
		}
		if p.Spec.Tools.Skills[0].Image != "registry.example.com/skills:latest" {
			t.Errorf("spec.tools.skills[0].image = %q, want %q", p.Spec.Tools.Skills[0].Image, "registry.example.com/skills:latest")
		}
	})

	t.Run("per-step tools set on run", func(t *testing.T) {
		as := &fakeAlertSource{alerts: models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", oldEnough)}}
		rc := &fakeRunClient{}

		cfg := config.Default()
		cfg.AllowedReceivers = []string{"critical"}
		cfg.Tools.Analysis = []agenticv1alpha1.SkillsSource{
			{Image: "registry.example.com/analysis:latest", Paths: []string{"/skills/diagnostic"}},
		}
		cfg.Tools.Execution = []agenticv1alpha1.SkillsSource{
			{Image: "registry.example.com/exec:latest", Paths: []string{"/skills/remediation"}},
		}

		a := &Adapter{
			alerts:    as,
			arClient:  rc,
			cfg:       cfg,
			namespace: agenticrun.RunNamespace,
			logger:    quietLogger(),
		}

		a.reconcile(context.Background())

		if len(rc.created) != 1 {
			t.Fatalf("created %d runs, want 1", len(rc.created))
		}
		p := rc.created[0]
		if p.Spec.Tools.IsZero() != true {
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
		if !p.Spec.Verification.Tools.IsZero() {
			t.Errorf("expected zero verification.tools, got %+v", p.Spec.Verification.Tools)
		}
	})
}

func TestReconcileZeroDelays(t *testing.T) {
	now := time.Now()
	justStarted := now.Add(-1 * time.Second)
	recentTerminal := now.Add(-1 * time.Minute)

	highCPUFP := stableFP(models.LabelSet{"alertname": "HighCPU", "severity": "warning"})

	tests := []struct {
		name         string
		preRunDelay  time.Duration
		postRunDelay time.Duration
		alerts       models.GettableAlerts
		runs         []agenticv1alpha1.AgenticRun
	}{
		{
			name:         "zero preRunDelay skips the delay check",
			preRunDelay:  0,
			postRunDelay: 1 * time.Hour,
			alerts:       models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", justStarted)},
		},
		{
			name:         "zero postRunDelay skips the delay check",
			preRunDelay:  5 * time.Minute,
			postRunDelay: 0,
			alerts:       models.GettableAlerts{makeAlert("HighCPU", "abcdef1234567890", now.Add(-10*time.Minute))},
			runs: []agenticv1alpha1.AgenticRun{
				makeRun(highCPUFP, []metav1.Condition{
					{Type: "Analyzed", Status: metav1.ConditionTrue},
					{Type: "Executed", Status: metav1.ConditionTrue},
					{Type: "Verified", Status: metav1.ConditionTrue, LastTransitionTime: metav1.NewTime(recentTerminal)},
				}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := &fakeAlertSource{alerts: tt.alerts}
			rc := &fakeRunClient{runs: tt.runs}

			cfg := defaultTestConfig()
			cfg.PreRunDelay = tt.preRunDelay
			cfg.PostRunDelay = tt.postRunDelay

			a := &Adapter{alerts: as, arClient: rc, cfg: cfg, namespace: agenticrun.RunNamespace, logger: quietLogger()}
			a.reconcile(context.Background())

			if rc.createCalls != 1 {
				t.Errorf("CreateAgenticRun called %d times, want 1 (zero delay should not skip)", rc.createCalls)
			}
		})
	}
}

func TestReconcileDedupsWithinSameCycle(t *testing.T) {
	now := time.Now()
	oldEnough1 := now.Add(-10 * time.Minute)
	oldEnough2 := now.Add(-20 * time.Minute)
	oldEnough3 := now.Add(-30 * time.Minute)

	as := &fakeAlertSource{
		alerts: models.GettableAlerts{
			makeAlert("KubeJobFailed", "aaa111", oldEnough1),
			makeAlert("KubeJobFailed", "bbb222", oldEnough2),
			makeAlert("KubeJobFailed", "ccc333", oldEnough3),
		},
	}
	rc := &fakeRunClient{}

	a := &Adapter{
		alerts:    as,
		arClient:  rc,
		cfg:       defaultTestConfig(),
		namespace: agenticrun.RunNamespace,
		logger:    quietLogger(),
	}

	a.reconcile(context.Background())

	if rc.createCalls != 1 {
		t.Errorf("CreateAgenticRun called %d times, want 1 (same fingerprint should dedup within cycle)", rc.createCalls)
	}
	if len(rc.created) != 1 {
		t.Errorf("created %d runs, want 1", len(rc.created))
	}
}

func TestRunExitsOnContextCancel(t *testing.T) {
	as := &fakeAlertSource{}
	rc := &fakeRunClient{}

	cfg := defaultTestConfig()
	cfg.PollInterval = time.Hour

	a := &Adapter{
		alerts:   as,
		arClient: rc,
		cfg:      cfg,
		logger:   quietLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}
