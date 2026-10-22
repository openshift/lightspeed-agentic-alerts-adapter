package adapter

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"github.com/prometheus/alertmanager/api/v2/models"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/config"
)

const triggerLabel = "agentic.openshift.io/trigger"

var (
	errSuspended     = errors.New("suspension is enabled")
	errAlertNotFound = errors.New("alert not found")
	errRunInProgress = errors.New("investigation in progress")
)

// InvestigationService creates on-demand investigations independently of the poller.
type InvestigationService struct {
	target     Target
	suspension SuspensionSource
	cfg        config.Config
	logger     *slog.Logger
}

func NewInvestigationService(
	target Target, suspension SuspensionSource, cfg config.Config, logger *slog.Logger,
) *InvestigationService {
	return &InvestigationService{
		target: target, suspension: suspension, cfg: cfg, logger: logger,
	}
}

// Investigate creates a local alert investigation without polling delays or backoff.
func (service *InvestigationService) Investigate(
	ctx context.Context,
	fingerprint string,
	creator AgenticRunCreator,
) (*agenticv1alpha1.AgenticRun, error) {
	if ctx.Err() != nil || creator == nil {
		logError(ctx, service.logger, "validate_investigation", ctx.Err())
		return nil, errInternal
	}
	suspended, err := service.suspension.Suspended(ctx)
	if err != nil || ctx.Err() != nil {
		logError(ctx, service.logger, "read_suspension", err)
		return nil, errInternal
	}
	if suspended {
		return nil, errSuspended
	}

	target := service.target
	if target.Name != "local" || target.ID != "" ||
		target.Alerts == nil || target.ARClient == nil || ctx.Err() != nil {
		logError(ctx, service.logger, "select_local_target", errInternal)
		return nil, errInternal
	}
	alerts, err := target.Alerts.GetAlerts(ctx)
	if err != nil || ctx.Err() != nil {
		logError(ctx, service.logger, "get_alerts", err)
		return nil, errInternal
	}
	var matched *models.GettableAlert
	for _, alert := range alerts {
		if alert != nil && alert.Fingerprint != nil &&
			*alert.Fingerprint == fingerprint {
			matched = alert
			break
		}
	}
	if matched == nil {
		return nil, errAlertNotFound
	}

	run, err := agenticrun.BuildForTarget(
		matched, service.cfg.Tools, service.cfg.Agent, service.cfg.IgnoredLabels,
		target.Namespace, target.ID, "",
	)
	if err != nil || ctx.Err() != nil {
		logError(ctx, service.logger, "build_run", err)
		return nil, errInternal
	}
	run.Labels[triggerLabel] = "api"

	runs, err := target.ARClient.ListAgenticRuns(ctx)
	if err != nil || ctx.Err() != nil {
		logError(ctx, service.logger, "list_runs", err)
		return nil, errInternal
	}
	run.Name, err = nextRunName(run, runs)
	if err != nil {
		if !errors.Is(err, errRunInProgress) {
			logError(ctx, service.logger, "select_run_name", err)
		}
		return nil, err
	}
	if ctx.Err() != nil {
		logError(ctx, service.logger, "prepare_run_creation", ctx.Err())
		return nil, errInternal
	}

	// Only the request-scoped caller client may create API-triggered runs.
	created, err := creator.CreateAgenticRun(ctx, run)
	if ctx.Err() != nil {
		logError(ctx, service.logger, "create_run", ctx.Err())
		return nil, errInternal
	}
	if apierrors.IsAlreadyExists(err) {
		return nil, errRunInProgress
	}
	if err != nil {
		logError(ctx, service.logger, "create_run", err)
		return nil, investigationCallerError(err)
	}
	if !created {
		return nil, errRunInProgress
	}
	return run, nil
}

const (
	maxRunNameLen  = 63
	episodeHashLen = 8
	groupIDLen     = 8
)

func nextRunName(
	built *agenticv1alpha1.AgenticRun,
	runs []agenticv1alpha1.AgenticRun,
) (string, error) {
	base := built.Name
	group := built.Labels[agenticrun.LabelDedupFingerprint]
	hashSeparator := strings.LastIndexByte(base, '-')
	if len(group) != groupIDLen || hashSeparator < 0 ||
		len(base)-hashSeparator-1 != episodeHashLen {
		return "", errInternal
	}
	qualified := shortenBase("g-"+group+"-"+base, maxRunNameLen)
	startsAt := built.Annotations[agenticrun.AnnotStartsAt]
	var latest *agenticv1alpha1.AgenticRun
	var latestActive, originalFound, qualifiedFound, baseOccupied bool

	for i := range runs {
		run := &runs[i]
		if run.Name == base {
			baseOccupied = true
		}
		if run.Labels[agenticrun.LabelDedupFingerprint] != group {
			continue
		}
		original := matchesAttempt(base, startsAt, run)
		scoped := matchesAttempt(qualified, startsAt, run)
		if !original && !scoped {
			continue
		}
		originalFound = originalFound || original
		qualifiedFound = qualifiedFound || scoped
		active := !isTerminal(agenticv1alpha1.DerivePhase(run.Status.Conditions))
		switch {
		case latest == nil || latest.CreationTimestamp.Before(&run.CreationTimestamp):
			latest = run
			latestActive = active
		case latest.CreationTimestamp.Equal(&run.CreationTimestamp):
			latestActive = latestActive || active
		}
	}
	if latestActive {
		return "", errRunInProgress
	}
	if !originalFound && (qualifiedFound || baseOccupied) {
		base = qualified
	}
	return agenticrun.NextAvailableName(base, runNames(runs)), nil
}

func matchesAttempt(
	base, startsAt string, run *agenticv1alpha1.AgenticRun,
) bool {
	if run.Name == base {
		return true
	}
	index := strings.LastIndex(run.Name, "-retry-")
	if index < 0 {
		return false
	}
	number := run.Name[index+len("-retry-"):]
	n, err := strconv.Atoi(number)
	if err != nil || n < 1 || strconv.Itoa(n) != number {
		return false
	}
	suffix := run.Name[index:]
	available := maxRunNameLen - len(suffix)
	if run.Name == shortenBase(base, available)+suffix {
		return true
	}

	// Legacy truncation may remove the hash; require the canonical start time.
	if len(base) <= available || startsAt == "" ||
		run.Annotations[agenticrun.AnnotStartsAt] != startsAt {
		return false
	}
	return run.Name == strings.TrimRight(base[:available], "-_.")+suffix
}

// shortenBase preserves the episode hash and optional full group qualifier.
func shortenBase(base string, limit int) string {
	if len(base) <= limit {
		return base
	}
	hashStart := len(base) - episodeHashLen - 1
	prefix, hash := base[:hashStart], base[hashStart:]
	qualifier := ""
	if strings.HasPrefix(prefix, "g-") {
		group, readable, ok := strings.Cut(prefix[2:], "-")
		if ok && len(group) == groupIDLen {
			qualifier, prefix = "g-"+group+"-", readable
		}
	}
	available := limit - len(qualifier) - len(hash)
	prefix = strings.TrimRight(prefix[:available], "-_.")
	return qualifier + prefix + hash
}
