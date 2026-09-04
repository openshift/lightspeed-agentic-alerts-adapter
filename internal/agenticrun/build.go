// Package agenticrun translates Alertmanager alerts into AgenticRun custom resources.
package agenticrun

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"github.com/prometheus/alertmanager/api/v2/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/config"
)

const (
	RunNamespace = "openshift-lightspeed"
	defaultAgent = "default"

	LabelSource           = "agentic.openshift.io/source"
	LabelFingerprint      = "agentic.openshift.io/alert-fingerprint"
	LabelDedupFingerprint = "agentic.openshift.io/alert-group-id"
	LabelAlertName        = "agentic.openshift.io/alert-name"
	LabelSeverity         = "agentic.openshift.io/alert-severity"
	AnnotStartsAt         = "agentic.openshift.io/alert-starts-at"
	AnnotSummary          = "agentic.openshift.io/alert-summary"

	sourceValue = "alertmanager"

	maxLabelValueLen = 63
	fingerprintLen   = 8
	maxSummaryLen    = 256
)

var (
	invalidLabelChars = regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	invalidDNSChars   = regexp.MustCompile(`[^a-z0-9-]`)
	backtickRun       = regexp.MustCompile("`{3,}")

	//go:embed request.tmpl
	requestTemplateStr string
	requestTemplate    = template.Must(template.New("request").Parse(requestTemplateStr))
)

type requestData struct {
	AlertName   string
	Severity    string
	RunbookURL  string
	Namespace   string
	Summary     string
	Description string
	SkillPaths  []string
}

// Build constructs an AgenticRun from a single Alertmanager GettableAlert.
// The AgenticRun name is deterministic based on the alert's identity (alertname,
// namespace, startsAt), making repeated calls for the same alert occurrence
// safe against duplicate creation via Kubernetes 409 AlreadyExists.
// Different occurrences of the same alert (different startsAt) produce
// distinct AgenticRun names, allowing re-creation after cooldown.
func Build(a *models.GettableAlert, tools config.ToolsConfig, agent config.AgentConfig, ignoredLabels []string, targetNamespace string) (*agenticv1alpha1.AgenticRun, error) {
	if a.Fingerprint == nil {
		return nil, fmt.Errorf("agenticrun: alert fingerprint is nil")
	}

	alertName := a.Labels["alertname"]
	namespace := a.Labels["namespace"]
	originalFP := *a.Fingerprint
	stableFP := StableFingerprint(a.Labels, ignoredLabels)
	severity := a.Labels["severity"]

	if a.StartsAt == nil {
		return nil, fmt.Errorf("agenticrun: alert startsAt is nil")
	}
	startsAt := time.Time(*a.StartsAt)

	request, err := buildRequest(a, tools.Shared, tools.Analysis)
	if err != nil {
		return nil, err
	}

	analysis := agenticv1alpha1.AgenticRunStep{Agent: resolveAgent(agent.Analysis, agent.Default)}
	execution := agenticv1alpha1.AgenticRunStep{Agent: resolveAgent(agent.Execution, agent.Default)}
	verification := agenticv1alpha1.AgenticRunStep{Agent: resolveAgent(agent.Verification, agent.Default)}

	if len(tools.Analysis) > 0 {
		analysis.Tools = agenticv1alpha1.ToolsSpec{Skills: tools.Analysis}
	}
	if len(tools.Execution) > 0 {
		execution.Tools = agenticv1alpha1.ToolsSpec{Skills: tools.Execution}
	}
	if len(tools.Verification) > 0 {
		verification.Tools = agenticv1alpha1.ToolsSpec{Skills: tools.Verification}
	}

	p := &agenticv1alpha1.AgenticRun{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "agentic.openshift.io/v1alpha1",
			Kind:       "AgenticRun",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        buildName(alertName, namespace, startsAt),
			Namespace:   targetNamespace,
			Labels:      buildLabels(alertName, severity, originalFP, stableFP),
			Annotations: buildAnnotations(a),
		},
		Spec: agenticv1alpha1.AgenticRunSpec{
			Request:      request,
			Analysis:     analysis,
			Execution:    execution,
			Verification: verification,
		},
	}

	if namespace != "" {
		p.Spec.TargetNamespaces = []string{namespace}
	}

	if len(tools.Shared) > 0 {
		p.Spec.Tools = agenticv1alpha1.ToolsSpec{Skills: tools.Shared}
	}

	return p, nil
}

// buildName produces a deterministic DNS-compatible name: {alertname}-{namespace}-{startsAtHash}
// or {alertname}-{startsAtHash} for cluster-scoped alerts.
// The startsAt hash is an 8-character hex digest of the alert's start time,
// ensuring each alert occurrence gets a unique AgenticRun name.
// The name is capped at 63 characters because the agentic operator uses it as a
// Kubernetes label value, which has a 63-byte limit.
func buildName(alertName, namespace string, startsAt time.Time) string {
	hash := startsAtHash(startsAt)

	name := strings.ToLower(alertName)
	name = invalidDNSChars.ReplaceAllString(name, "-")

	if namespace != "" {
		ns := strings.ToLower(namespace)
		ns = invalidDNSChars.ReplaceAllString(ns, "-")
		combined := name + "-" + ns + "-" + hash
		if len(combined) <= maxLabelValueLen {
			return combined
		}
		available := max(maxLabelValueLen-len(ns)-len(hash)-2, 1)
		return truncateDNS(name, available) + "-" + ns + "-" + hash
	}

	combined := name + "-" + hash
	if len(combined) <= maxLabelValueLen {
		return combined
	}
	available := maxLabelValueLen - len(hash) - 1
	return truncateDNS(name, available) + "-" + hash
}

const startsAtHashLen = 8

func startsAtHash(t time.Time) string {
	h := sha256.Sum256([]byte(t.UTC().Format(time.RFC3339)))
	return hex.EncodeToString(h[:])[:startsAtHashLen]
}

// buildLabels sets Kubernetes labels for alert traceability and filtering.
func buildLabels(alertName, severity, originalFingerprint, stableFingerprint string) map[string]string {
	return map[string]string{
		LabelSource:           sourceValue,
		LabelFingerprint:      sanitizeLabelValue(originalFingerprint[:min(len(originalFingerprint), fingerprintLen)]),
		LabelDedupFingerprint: stableFingerprint,
		LabelAlertName:        sanitizeLabelValue(strings.ToLower(alertName)),
		LabelSeverity:         sanitizeLabelValue(severity),
	}
}

// buildAnnotations sets Kubernetes annotations with non-indexed alert metadata.
func buildAnnotations(a *models.GettableAlert) map[string]string {
	annots := map[string]string{}

	if a.StartsAt != nil && !time.Time(*a.StartsAt).IsZero() {
		annots[AnnotStartsAt] = time.Time(*a.StartsAt).UTC().Format(time.RFC3339)
	}

	if summary, ok := a.Annotations["summary"]; ok && summary != "" {
		if len(summary) > maxSummaryLen {
			summary = summary[:maxSummaryLen]
		}
		annots[AnnotSummary] = summary
	}

	return annots
}

// buildRequest renders the embedded template with alert data for the analysis agent.
func buildRequest(a *models.GettableAlert, sharedSkills, analysisSkills []agenticv1alpha1.SkillsSource) (string, error) {
	var skillPaths []string
	for _, s := range sharedSkills {
		for _, p := range s.Paths {
			skillPaths = append(skillPaths, "/app"+p)
		}
	}
	for _, s := range analysisSkills {
		for _, p := range s.Paths {
			skillPaths = append(skillPaths, "/app"+p)
		}
	}

	data := requestData{
		AlertName:   sanitizePromptValue(a.Labels["alertname"]),
		Severity:    sanitizePromptValue(a.Labels["severity"]),
		RunbookURL:  sanitizePromptValue(a.Annotations["runbook_url"]),
		Namespace:   sanitizePromptValue(a.Labels["namespace"]),
		Summary:     sanitizePromptValue(a.Annotations["summary"]),
		Description: sanitizePromptValue(a.Annotations["description"]),
		SkillPaths:  skillPaths,
	}

	var buf bytes.Buffer
	if err := requestTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("agenticrun: rendering request template: %w", err)
	}
	return buf.String(), nil
}

// sanitizeLabelValue ensures a string conforms to Kubernetes label value rules:
// max 63 chars, alphanumeric/hyphens/underscores/dots, must start and end with alphanumeric.
func sanitizeLabelValue(s string) string {
	if s == "" {
		return s
	}

	s = invalidLabelChars.ReplaceAllString(s, "-")

	if len(s) > maxLabelValueLen {
		s = s[:maxLabelValueLen]
	}

	s = strings.TrimLeft(s, "-_.")
	s = strings.TrimRight(s, "-_.")

	return s
}

// sanitizePromptValue strips control characters (except newline), Unicode format
// characters (zero-width spaces, bidi overrides), and backtick runs of 3+ from s.
func sanitizePromptValue(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, s)
	return backtickRun.ReplaceAllString(s, "")
}

func resolveAgent(perStep, global string) string {
	if perStep != "" {
		return perStep
	}
	if global != "" {
		return global
	}
	return defaultAgent
}

// NextAvailableName returns baseName when unused, otherwise returns the first
// available deterministic retry name using -retry-N suffixes.
func NextAvailableName(baseName string, existingNames []string) string {
	existing := make(map[string]struct{}, len(existingNames))
	for _, name := range existingNames {
		existing[name] = struct{}{}
	}

	if _, ok := existing[baseName]; !ok {
		return baseName
	}

	for i := 1; ; i++ {
		suffix := fmt.Sprintf("-retry-%d", i)
		candidate := truncateDNS(baseName, maxLabelValueLen-len(suffix)) + suffix
		if _, ok := existing[candidate]; !ok {
			return candidate
		}
	}
}

func truncateDNS(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	s = s[:maxLen]
	s = strings.TrimRight(s, "-_.")
	return s
}

// StableFingerprint computes a stable hash from alert labels after removing
// ignored labels. The remaining key=value pairs are sorted lexicographically,
// joined with a null byte separator, and hashed with FNV-64a truncated to 8
// hex characters.
func StableFingerprint(labels map[string]string, ignoredLabels []string) string {
	pairs := make([]string, 0, len(labels))
	for k, v := range labels {
		if slices.Contains(ignoredLabels, k) {
			continue
		}
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)

	h := fnv.New64a()
	for i, p := range pairs {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}

	return fmt.Sprintf("%016x", h.Sum64())[:fingerprintLen]
}
