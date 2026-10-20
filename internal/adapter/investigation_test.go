package adapter

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"github.com/prometheus/alertmanager/api/v2/models"
	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
)

func TestInvestigationService(t *testing.T) {
	failure := errors.New("test dependency failure")
	tests := []struct {
		name          string
		suspended     bool
		suspensionErr error
		alertsErr     error
		missingAlert  bool
		invalidAlert  bool
		listErr       error
		createErr     error
		alreadyExists bool
		wantErr       error
		wantAlerts    int
		wantList      int
		wantCreate    int
	}{
		{name: "creates with caller identity", wantAlerts: 1, wantList: 1, wantCreate: 1},
		{name: "suspension enabled", suspended: true, wantErr: errSuspended},
		{name: "suspension read failure", suspensionErr: failure, wantErr: errInternal},
		{name: "Alertmanager failure", alertsErr: failure, wantErr: errInternal, wantAlerts: 1},
		{name: "fingerprint must match exactly", missingAlert: true, wantErr: errAlertNotFound, wantAlerts: 1},
		{name: "build failure", invalidAlert: true, wantErr: errInternal, wantAlerts: 1},
		{name: "internal listing failure", listErr: failure, wantErr: errInternal, wantAlerts: 1, wantList: 1},
		{name: "internal credentials rejected", listErr: apierrors.NewUnauthorized("test rejection"), wantErr: errInternal, wantAlerts: 1, wantList: 1},
		{name: "caller unauthorized", createErr: apierrors.NewUnauthorized("test rejection"), wantErr: errInvestigationUnauthorized, wantAlerts: 1, wantList: 1, wantCreate: 1},
		{
			name:      "caller forbidden",
			createErr: apierrors.NewForbidden(schema.GroupResource{Group: "agentic.openshift.io", Resource: "agenticruns"}, "test-run", failure),
			wantErr:   errInvestigationForbidden, wantAlerts: 1, wantList: 1, wantCreate: 1,
		},
		{name: "creation failure", createErr: failure, wantErr: errInternal, wantAlerts: 1, wantList: 1, wantCreate: 1},
		{name: "concurrent name collision", alreadyExists: true, wantErr: errRunInProgress, wantAlerts: 1, wantList: 1, wantCreate: 1},
		{
			name:      "raw AlreadyExists",
			createErr: apierrors.NewAlreadyExists(schema.GroupResource{Group: "agentic.openshift.io", Resource: "agenticruns"}, "test-run"),
			wantErr:   errRunInProgress, wantAlerts: 1, wantList: 1, wantCreate: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const fingerprint = "abcdef1234567890"
			alert := makeAlert("HighCPU", fingerprint, time.Now())
			if tt.missingAlert {
				alert.Fingerprint = ptr("ABCDEF1234567890")
			}
			if tt.invalidAlert {
				alert.StartsAt = nil
			}
			alerts := &fakeAlertSource{alerts: models.GettableAlerts{nil, alert}, err: tt.alertsErr}
			internalRuns := &fakeRunClient{listErr: tt.listErr}
			caller := &fakeRunClient{createErr: tt.createErr, wasCreated: ptr(!tt.alreadyExists)}
			suspension := &fakeSuspensionSource{suspended: tt.suspended, err: tt.suspensionErr}
			cfg := defaultTestConfig()
			cfg.AllowedReceivers = []string{"different-receiver"}
			service := NewInvestigationService(Target{
				Name: "local", Namespace: agenticrun.RunNamespace, Alerts: alerts, ARClient: internalRuns,
			}, suspension, cfg)
			run, err := service.Investigate(t.Context(), fingerprint, caller)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, 1, suspension.calls)
			assert.Equal(t, tt.wantAlerts, alerts.getCalls)
			assert.Equal(t, tt.wantList, internalRuns.listCalls)
			assert.Zero(t, internalRuns.createCalls)
			assert.Zero(t, caller.listCalls)
			assert.Equal(t, tt.wantCreate, caller.createCalls)
			if tt.wantErr != nil {
				assert.Nil(t, run)
				assert.Empty(t, caller.created)
				return
			}
			if !assert.NotNil(t, run) || !assert.Len(t, caller.created, 1) {
				return
			}
			assert.Same(t, run, caller.created[0])
			assert.Equal(t, agenticrun.RunNamespace, run.Namespace)
			assert.Equal(t, "api", run.Labels[triggerLabel])
			assert.Equal(t, "alertmanager", run.Labels[agenticrun.LabelSource])
			assert.Equal(t, time.Time(*alert.StartsAt).UTC().Format(time.RFC3339), run.Annotations[agenticrun.AnnotStartsAt])
			assert.Empty(t, run.Spec.TargetCluster)
		})
	}
}

func TestNextRunName(t *testing.T) {
	const base = "highcpu-abcdef12"
	const group = "01234567"
	const otherGroup = "76543210"
	const qualified = "g-" + group + "-" + base
	createdAt := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	built := makeRunWithName(base, group, nil)
	attempt := func(name, group string, seconds int, terminal bool) agenticv1alpha1.AgenticRun {
		run := makeRunWithName(name, group, nil)
		run.CreationTimestamp = metav1.NewTime(createdAt.Add(time.Duration(seconds) * time.Second))
		if terminal {
			run.Status.Conditions = []metav1.Condition{{Type: "Verified", Status: metav1.ConditionTrue}}
		}
		return run
	}
	tests := []struct {
		name    string
		runs    []agenticv1alpha1.AgenticRun
		want    string
		wantErr error
	}{
		{name: "unused base", want: base},
		{name: "active original", runs: []agenticv1alpha1.AgenticRun{attempt(base, group, 0, false)}, wantErr: errRunInProgress},
		{name: "terminal original", runs: []agenticv1alpha1.AgenticRun{attempt(base, group, 0, true)}, want: base + "-retry-1"},
		{name: "active occupant from another group", runs: []agenticv1alpha1.AgenticRun{attempt(base, otherGroup, 0, false)}, want: qualified},
		{name: "terminal occupant from another group", runs: []agenticv1alpha1.AgenticRun{attempt(base, otherGroup, 0, true)}, want: qualified},
		{name: "active qualified attempt without original occupant", runs: []agenticv1alpha1.AgenticRun{attempt(qualified, group, 0, false)}, wantErr: errRunInProgress},
		{name: "terminal qualified attempt retains family", runs: []agenticv1alpha1.AgenticRun{attempt(qualified, group, 0, true)}, want: qualified + "-retry-1"},
		{name: "both families prefer original", runs: []agenticv1alpha1.AgenticRun{attempt(base, group, 0, true), attempt(qualified, group, 1, true)}, want: base + "-retry-1"},
		{name: "older active attempt does not block latest terminal", runs: []agenticv1alpha1.AgenticRun{attempt(base, group, 0, false), attempt(base+"-retry-1", group, 1, true)}, want: base + "-retry-2"},
		{name: "latest active retry blocks", runs: []agenticv1alpha1.AgenticRun{attempt(base, group, 0, true), attempt(base+"-retry-1", group, 1, false)}, wantErr: errRunInProgress},
		{name: "three latest attempts with one active", runs: []agenticv1alpha1.AgenticRun{attempt(base, group, 0, true), attempt(base+"-retry-1", group, 0, false), attempt(base+"-retry-2", group, 0, true)}, wantErr: errRunInProgress},
		{name: "three terminal latest attempts allow retry", runs: []agenticv1alpha1.AgenticRun{attempt(base, group, 0, true), attempt(base+"-retry-1", group, 0, true), attempt(base+"-retry-2", group, 0, true)}, want: base + "-retry-3"},
		{name: "mixed latest tie across families", runs: []agenticv1alpha1.AgenticRun{attempt(base, group, 0, true), attempt(qualified, group, 0, false)}, wantErr: errRunInProgress},
		{name: "another episode in same group does not block", runs: []agenticv1alpha1.AgenticRun{attempt("highcpu-12345678", group, 0, false)}, want: base},
		{name: "occupied qualified candidate is skipped", runs: []agenticv1alpha1.AgenticRun{attempt(base, otherGroup, 0, false), attempt(qualified, otherGroup, 0, false)}, want: qualified + "-retry-1"},
		{name: "retry occupied by another group is skipped", runs: []agenticv1alpha1.AgenticRun{attempt(base, group, 0, true), attempt(base+"-retry-1", otherGroup, 1, false)}, want: base + "-retry-2"},
		{name: "suffix gaps use first available name", runs: []agenticv1alpha1.AgenticRun{attempt(base, group, 0, true), attempt(base+"-retry-2", group, 1, true)}, want: base + "-retry-1"},
	}
	for _, tt := range tests {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%t", tt.name, reverse), func(t *testing.T) {
				runs := slices.Clone(tt.runs)
				if reverse {
					slices.Reverse(runs)
				}
				name, err := nextRunName(&built, runs)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, tt.want, name)
			})
		}
	}
}

func TestMatchesAttempt(t *testing.T) {
	const startsAt = "2026-01-15T10:00:00Z"
	for _, qualifier := range []string{"", "g-01234567-"} {
		base := qualifier + strings.Repeat("a", 54-len(qualifier)) + "-abcdef12"
		retry := qualifier + strings.Repeat("a", 46-len(qualifier)) + "-abcdef12-retry-1"
		legacy := qualifier + strings.Repeat("a", 54-len(qualifier)) + "-retry-1"
		tests := []struct {
			name       string
			runName    string
			annotation string
			want       bool
		}{
			{name: "base", runName: base, want: true},
			{name: "hash preserving retry", runName: retry, want: true},
			{name: "legacy with matching start", runName: legacy, annotation: startsAt, want: true},
			{name: "legacy without start", runName: legacy},
			{name: "legacy with different start", runName: legacy, annotation: "2026-01-16T10:00:00Z"},
			{name: "different hash", runName: strings.Replace(retry, "abcdef12", "12345678", 1)},
			{name: "zero retry", runName: base + "-retry-0"},
			{name: "noncanonical suffix", runName: base + "-retry-01"},
			{name: "nested suffix", runName: retry + "-retry-1"},
		}
		for _, tt := range tests {
			t.Run(qualifier+tt.name, func(t *testing.T) {
				run := makeRunWithName(tt.runName, "01234567", nil)
				run.Annotations = map[string]string{agenticrun.AnnotStartsAt: tt.annotation}
				assert.Equal(t, tt.want, matchesAttempt(base, startsAt, &run))
			})
		}
	}
}

func TestLegacyRetryEpisodeAttribution(t *testing.T) {
	const group = "01234567"
	const startsAt = "2026-01-15T10:00:00Z"
	base := strings.Repeat("a", 54) + "-abcdef12"
	built := makeRunWithName(base, group, nil)
	built.Annotations = map[string]string{agenticrun.AnnotStartsAt: startsAt}
	tests := []struct {
		name       string
		group      string
		annotation string
		wantErr    error
	}{
		{name: "same episode", group: group, annotation: startsAt, wantErr: errRunInProgress},
		{name: "missing annotation", group: group},
		{name: "different episode", group: group, annotation: "2026-01-16T10:00:00Z"},
		{name: "different group", group: "76543210", annotation: startsAt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			legacy := makeRunWithName(strings.Repeat("a", 54)+"-retry-1", tt.group, nil)
			legacy.Annotations = map[string]string{agenticrun.AnnotStartsAt: tt.annotation}
			name, err := nextRunName(&built, []agenticv1alpha1.AgenticRun{legacy})
			assert.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr == nil {
				assert.Equal(t, base, name)
			} else {
				assert.Empty(t, name)
			}
		})
	}
}

func TestQualifiedNamePreservesIdentity(t *testing.T) {
	base := strings.Repeat("a", 54) + "-abcdef12"
	names := make(map[string]bool)
	for _, group := range []string{"01234567", "76543210"} {
		qualifier := "g-" + group + "-"
		selected := shortenBase(qualifier+base, 63)
		assert.Equal(t, qualifier+strings.Repeat("a", 43)+"-abcdef12", selected)
		assert.False(t, names[selected], "different groups must have distinct names")
		names[selected] = true
		for _, suffix := range []string{"-retry-1", "-retry-10", "-retry-100"} {
			t.Run(group+suffix, func(t *testing.T) {
				want := qualifier + strings.Repeat("a", 63-len(qualifier)-9-len(suffix)) + "-abcdef12" + suffix
				got := shortenBase(selected, 63-len(suffix)) + suffix
				assert.Equal(t, want, got)
				assert.Len(t, got, 63)
				run := makeRunWithName(got, group, nil)
				assert.True(t, matchesAttempt(selected, "", &run))
			})
		}
	}
}

func TestTerminalAttemptsPermitRetry(t *testing.T) {
	tests := []struct {
		name      string
		condition metav1.Condition
		phase     agenticv1alpha1.AgenticRunPhase
	}{
		{name: "completed", phase: agenticv1alpha1.AgenticRunPhaseCompleted, condition: metav1.Condition{Type: "Verified", Status: metav1.ConditionTrue}},
		{name: "failed", phase: agenticv1alpha1.AgenticRunPhaseFailed, condition: metav1.Condition{Type: "Verified", Status: metav1.ConditionFalse}},
		{name: "denied", phase: agenticv1alpha1.AgenticRunPhaseDenied, condition: metav1.Condition{Type: "Denied", Status: metav1.ConditionTrue}},
		{name: "escalated", phase: agenticv1alpha1.AgenticRunPhaseEscalated, condition: metav1.Condition{Type: "Escalated", Status: metav1.ConditionTrue}},
		{name: "emergency stopped", phase: agenticv1alpha1.AgenticRunPhaseEmergencyStopped, condition: metav1.Condition{Type: "EmergencyStopped", Status: metav1.ConditionTrue}},
		{name: "no action required", phase: agenticv1alpha1.AgenticRunPhaseCompleted, condition: metav1.Condition{Type: "Analyzed", Status: metav1.ConditionTrue, Reason: "NoActionRequired"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			built := makeRunWithName("highcpu-abcdef12", "01234567", nil)
			existing := built.DeepCopy()
			existing.Status.Conditions = []metav1.Condition{tt.condition}
			assert.Equal(t, tt.phase, agenticv1alpha1.DerivePhase(existing.Status.Conditions))
			name, err := nextRunName(&built, []agenticv1alpha1.AgenticRun{*existing})
			assert.NoError(t, err)
			assert.Equal(t, built.Name+"-retry-1", name)
		})
	}
}

type contextAwareDependencies struct {
	t        *testing.T
	ctx      context.Context
	cancel   context.CancelFunc
	cancelAt string
	calls    []string
	alert    *models.GettableAlert
}

func (dependencies *contextAwareDependencies) record(ctx context.Context, step string) {
	dependencies.t.Helper()
	assert.True(dependencies.t, ctx == dependencies.ctx, "every operation must receive the request context")
	dependencies.calls = append(dependencies.calls, step)
	if step == dependencies.cancelAt {
		dependencies.cancel()
	}
}

func (dependencies *contextAwareDependencies) Suspended(ctx context.Context) (bool, error) {
	dependencies.record(ctx, "suspension")
	return false, nil
}

func (dependencies *contextAwareDependencies) GetAlerts(ctx context.Context) (models.GettableAlerts, error) {
	dependencies.record(ctx, "alerts")
	return models.GettableAlerts{dependencies.alert}, nil
}

func (dependencies *contextAwareDependencies) ListAgenticRuns(ctx context.Context) ([]agenticv1alpha1.AgenticRun, error) {
	dependencies.record(ctx, "list")
	return nil, nil
}

func (dependencies *contextAwareDependencies) CreateAgenticRun(ctx context.Context, _ *agenticv1alpha1.AgenticRun) (bool, error) {
	dependencies.record(ctx, "create")
	return true, nil
}

func TestInvestigationCancellation(t *testing.T) {
	tests := []struct {
		name           string
		cancelBefore   bool
		cancelAt       string
		missingCreator bool
		wantCalls      []string
	}{
		{name: "already cancelled", cancelBefore: true},
		{name: "missing creator", missingCreator: true},
		{name: "after suspension", cancelAt: "suspension", wantCalls: []string{"suspension"}},
		{name: "after alerts", cancelAt: "alerts", wantCalls: []string{"suspension", "alerts"}},
		{name: "after listing", cancelAt: "list", wantCalls: []string{"suspension", "alerts", "list"}},
		{name: "after creation", cancelAt: "create", wantCalls: []string{"suspension", "alerts", "list", "create"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			dependencies := &contextAwareDependencies{
				t: t, ctx: ctx, cancel: cancel, cancelAt: tt.cancelAt,
				alert: makeAlert("HighCPU", "abcdef1234567890", time.Now()),
			}
			service := NewInvestigationService(Target{
				Name: "local", Namespace: agenticrun.RunNamespace, Alerts: dependencies, ARClient: dependencies,
			}, dependencies, defaultTestConfig())
			var creator AgenticRunCreator = dependencies
			if tt.missingCreator {
				creator = nil
			}
			if tt.cancelBefore {
				cancel()
			}
			run, err := service.Investigate(ctx, "abcdef1234567890", creator)
			assert.ErrorIs(t, err, errInternal)
			assert.Nil(t, run)
			assert.Equal(t, tt.wantCalls, dependencies.calls)
		})
	}
}

func TestInvestigationRejectsNonLocalTarget(t *testing.T) {
	for _, target := range []Target{{}, {Name: "spoke", ID: "spoke-example"}, {Name: "local", ID: "spoke-local"}} {
		t.Run(target.Name+"/"+target.ID, func(t *testing.T) {
			alerts := &fakeAlertSource{}
			internalRuns := &fakeRunClient{}
			caller := &fakeRunClient{}
			suspension := &fakeSuspensionSource{}
			target.Alerts, target.ARClient = alerts, internalRuns
			service := NewInvestigationService(target, suspension, defaultTestConfig())
			run, err := service.Investigate(t.Context(), "abc", caller)
			assert.ErrorIs(t, err, errInternal)
			assert.Nil(t, run)
			assert.Equal(t, 1, suspension.calls)
			assert.Zero(t, alerts.getCalls)
			assert.Zero(t, internalRuns.listCalls)
			assert.Zero(t, caller.createCalls)
		})
	}
}
