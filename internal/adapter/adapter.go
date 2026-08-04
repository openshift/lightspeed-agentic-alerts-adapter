// Package adapter implements the poll loop that connects AlertManager alerts
// to AgenticRun creation with stateless deduplication.
package adapter

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"github.com/prometheus/alertmanager/api/v2/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/config"
)

// AlertSource retrieves firing alerts from an external alerting system.
type AlertSource interface {
	GetAlerts(ctx context.Context) (models.GettableAlerts, error)
}

// AgenticRunClient manages AgenticRun custom resources in the cluster.
type AgenticRunClient interface {
	ListAgenticRuns(ctx context.Context) ([]agenticv1alpha1.AgenticRun, error)
	CreateAgenticRun(ctx context.Context, p *agenticv1alpha1.AgenticRun) (bool, error)
}

// Adapter polls AlertManager for firing alerts and creates AgenticRun CRs,
// applying stateless deduplication (pre-run delay, active-run check,
// and post-run delay) on each cycle.
type Adapter struct {
	alerts   AlertSource
	arClient AgenticRunClient
	cfg      config.Config
	logger   *slog.Logger
}

// New creates an Adapter with the given alert source, run client,
// config, and logger.
func New(alerts AlertSource, arClient AgenticRunClient, cfg config.Config, logger *slog.Logger) *Adapter {
	return &Adapter{
		alerts:   alerts,
		arClient: arClient,
		cfg:      cfg,
		logger:   logger,
	}
}

// Run starts the poll loop, blocking until the context is cancelled.
func (a *Adapter) Run(ctx context.Context) error {
	a.logger.Info("adapter started",
		"pollInterval", a.cfg.PollInterval.String(),
		"preRunDelay", a.cfg.PreRunDelay.String(),
		"postRunDelay", a.cfg.PostRunDelay.String(),
		"allowedReceivers", a.cfg.AllowedReceivers,
	)

	a.reconcile(ctx)

	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.logger.Info("adapter stopping")
			return nil
		case <-ticker.C:
			a.reconcile(ctx)
		}
	}
}

func (a *Adapter) reconcile(ctx context.Context) {
	a.logger.Debug("poll cycle start")

	alerts, err := a.alerts.GetAlerts(ctx)
	if err != nil {
		a.logger.Error("failed to get alerts", "error", err)
		return
	}

	runs, err := a.arClient.ListAgenticRuns(ctx)
	if err != nil {
		a.logger.Error("failed to list runs", "error", err)
		return
	}

	now := time.Now()
	var created, skipped int

	for i := range alerts {
		if ctx.Err() != nil {
			return
		}

		alert := alerts[i]

		fingerprint := ""
		if alert.Fingerprint != nil {
			fingerprint = *alert.Fingerprint
		}
		alertName := alert.Labels["alertname"]

		if skipReceiver(alert, a.cfg.AllowedReceivers) {
			a.logger.Debug("alert skipped: no matching receiver",
				"alertname", alertName,
				"fingerprint", fingerprint,
				"receivers", receiverNames(alert),
			)
			skipped++
			continue
		}

		if skipSeverity(alert) {
			a.logger.Debug("alert skipped: low severity",
				"alertname", alertName,
				"fingerprint", fingerprint,
				"severity", alert.Labels["severity"],
			)
			skipped++
			continue
		}

		if a.cfg.PreRunDelay > 0 && tooEarly(alert, now, a.cfg.PreRunDelay) {
			a.logger.Debug("alert skipped: pre-run delay",
				"alertname", alertName,
				"fingerprint", fingerprint,
				"startsAt", alert.StartsAt,
				"preRunDelay", a.cfg.PreRunDelay,
			)
			skipped++
			continue
		}

		stableFP := agenticrun.StableFingerprint(alert.Labels, a.cfg.IgnoredLabels)

		if hasActiveRun(stableFP, runs) {
			a.logger.Debug("alert skipped: active run exists",
				"alertname", alertName,
				"fingerprint", fingerprint,
			)
			skipped++
			continue
		}

		if a.cfg.PostRunDelay > 0 && tooRecent(stableFP, runs, now, a.cfg.PostRunDelay) {
			a.logger.Debug("alert skipped: post-run delay",
				"alertname", alertName,
				"fingerprint", fingerprint,
				"postRunDelay", a.cfg.PostRunDelay,
			)
			skipped++
			continue
		}

		p, err := agenticrun.Build(alert, a.cfg.Tools, a.cfg.Agent, a.cfg.IgnoredLabels)
		if err != nil {
			a.logger.Error("failed to build run",
				"alertname", alertName,
				"fingerprint", fingerprint,
				"error", err,
			)
			continue
		}

		wasCreated, err := a.arClient.CreateAgenticRun(ctx, p)
		if err != nil {
			a.logger.Error("failed to create run",
				"alertname", alertName,
				"fingerprint", fingerprint,
				"run", p.Name,
				"error", err,
			)
			continue
		}

		if wasCreated {
			runs = append(runs, *p)
			a.logger.Info("run created",
				"alertname", alertName,
				"fingerprint", fingerprint,
				"run", p.Name,
			)
			created++
		}
	}

	a.logger.Info("poll cycle complete",
		"alertsTotal", len(alerts),
		"skipped", skipped,
		"created", created,
	)
}

func skipReceiver(alert *models.GettableAlert, allowed []string) bool {
	for _, r := range alert.Receivers {
		if r == nil || r.Name == nil {
			continue
		}
		if slices.Contains(allowed, strings.ToLower(*r.Name)) {
			return false
		}
	}
	return true
}

func receiverNames(alert *models.GettableAlert) []string {
	names := make([]string, 0, len(alert.Receivers))
	for _, r := range alert.Receivers {
		if r != nil && r.Name != nil {
			names = append(names, *r.Name)
		}
	}
	return names
}

func skipSeverity(alert *models.GettableAlert) bool {
	sev := strings.ToLower(string(alert.Labels["severity"]))
	return sev == "none" || sev == "info"
}

func tooEarly(alert *models.GettableAlert, now time.Time, threshold time.Duration) bool {
	if alert.StartsAt == nil {
		return true
	}
	return now.Sub(time.Time(*alert.StartsAt)) < threshold
}

func hasActiveRun(stableFingerprint string, runs []agenticv1alpha1.AgenticRun) bool {
	for i := range runs {
		if runs[i].Labels[agenticrun.LabelDedupFingerprint] != stableFingerprint {
			continue
		}
		phase := agenticv1alpha1.DerivePhase(runs[i].Status.Conditions)
		if !isTerminal(phase) {
			return true
		}
	}
	return false
}

func tooRecent(stableFingerprint string, runs []agenticv1alpha1.AgenticRun, now time.Time, window time.Duration) bool {
	for i := range runs {
		if runs[i].Labels[agenticrun.LabelDedupFingerprint] != stableFingerprint {
			continue
		}
		tt := terminalTime(&runs[i])
		if tt != nil && now.Sub(*tt) < window {
			return true
		}
	}
	return false
}

// terminalTime returns the LastTransitionTime of the condition that caused
// the run to reach a terminal phase, or nil if the run is not terminal.
func terminalTime(p *agenticv1alpha1.AgenticRun) *time.Time {
	phase := agenticv1alpha1.DerivePhase(p.Status.Conditions)

	var condType string
	switch phase {
	case agenticv1alpha1.AgenticRunPhaseCompleted:
		condType = agenticv1alpha1.AgenticRunConditionVerified
	case agenticv1alpha1.AgenticRunPhaseFailed:
		condType = findFailedConditionType(p.Status.Conditions)
	case agenticv1alpha1.AgenticRunPhaseDenied:
		condType = agenticv1alpha1.AgenticRunConditionDenied
	case agenticv1alpha1.AgenticRunPhaseEscalated:
		condType = agenticv1alpha1.AgenticRunConditionEscalated
	default:
		return nil
	}

	if condType == "" {
		return nil
	}

	for i := range p.Status.Conditions {
		if p.Status.Conditions[i].Type == condType {
			t := p.Status.Conditions[i].LastTransitionTime.Time
			return &t
		}
	}
	return nil
}

func findFailedConditionType(conditions []metav1.Condition) string {
	for i := range conditions {
		if conditions[i].Status == metav1.ConditionFalse {
			return conditions[i].Type
		}
	}
	return ""
}

func isTerminal(phase agenticv1alpha1.AgenticRunPhase) bool {
	switch phase {
	case agenticv1alpha1.AgenticRunPhaseCompleted,
		agenticv1alpha1.AgenticRunPhaseFailed,
		agenticv1alpha1.AgenticRunPhaseDenied,
		agenticv1alpha1.AgenticRunPhaseEscalated:
		return true
	}
	return false
}

