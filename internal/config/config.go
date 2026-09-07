package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"go.yaml.in/yaml/v3"
)

const (
	DefaultPollInterval = 30 * time.Second
	DefaultPreRunDelay  = 0
	DefaultPostRunDelay = 1 * time.Hour

	DefaultConfigPath = "/etc/alerts-adapter/config.yaml"
)

var DefaultIgnoredLabels = []string{"pod", "instance", "endpoint", "uid"}

// AgentConfig holds the agent name overrides for AgenticRun workflow steps.
type AgentConfig struct {
	Default      string
	Analysis     string
	Execution    string
	Verification string
}

// ToolsConfig holds shared and per-step skills configuration.
type ToolsConfig struct {
	Shared       []agenticv1alpha1.SkillsSource
	Analysis     []agenticv1alpha1.SkillsSource
	Execution    []agenticv1alpha1.SkillsSource
	Verification []agenticv1alpha1.SkillsSource
}

// Config holds the adapter's runtime-tunable parameters.
type Config struct {
	PollInterval     time.Duration
	PreRunDelay      time.Duration
	PostRunDelay     time.Duration
	AllowedReceivers []string
	IgnoredLabels    []string
	Tools            ToolsConfig
	Agent            AgentConfig
}

// Default returns a Config with the default values.
func Default() Config {
	return Config{
		PollInterval:     DefaultPollInterval,
		PreRunDelay:      DefaultPreRunDelay,
		PostRunDelay:     DefaultPostRunDelay,
		AllowedReceivers: nil,
		IgnoredLabels:    append([]string{}, DefaultIgnoredLabels...),
	}
}

type configFile struct {
	PollInterval     Duration           `yaml:"pollInterval"`
	PreRunDelay      Duration           `yaml:"preRunDelay"`
	PostRunDelay     Duration           `yaml:"postRunDelay"`
	AllowedReceivers *[]string          `yaml:"allowedReceivers"`
	Filtering        filteringEntry     `yaml:"filtering"`
	Deduplication    deduplicationEntry `yaml:"deduplication"`
	Tools            toolsEntry         `yaml:"tools"`
	Analysis         stepEntry          `yaml:"analysis"`
	Execution        stepEntry          `yaml:"execution"`
	Verification     stepEntry          `yaml:"verification"`
	Agent            agentEntry         `yaml:"agent"`
}

type filteringEntry struct {
	AllowedReceivers *[]string `yaml:"allowedReceivers"`
}

type deduplicationEntry struct {
	IgnoredLabels *[]string `yaml:"ignoredLabels"`
}

type agentEntry struct {
	Default      string `yaml:"default"`
	Analysis     string `yaml:"analysis"`
	Execution    string `yaml:"execution"`
	Verification string `yaml:"verification"`
}

type toolsEntry struct {
	Skills []skillsEntry `yaml:"skills"`
}

type stepEntry struct {
	Tools toolsEntry `yaml:"tools"`
}

type skillsEntry struct {
	Image string   `yaml:"image"`
	Paths []string `yaml:"paths"`
}

// Duration wraps time.Duration for YAML unmarshalling.
type Duration struct {
	time.Duration
	isSet bool
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("config: invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	d.isSet = true
	return nil
}

// LoadFromFile reads configuration from a YAML file at the given path.
// It returns an error on invalid YAML or unparseable duration values.
// Missing file is not an error — defaults are used.
func LoadFromFile(path string, logger *slog.Logger) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("config file not found, using defaults", "path", path)
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading config file: %w", err)
	}

	var cf configFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return cfg, fmt.Errorf("parsing config file: %w", err)
	}

	if cf.PollInterval.isSet {
		if cf.PollInterval.Duration > 0 {
			cfg.PollInterval = cf.PollInterval.Duration
		} else {
			logger.Error("pollInterval must be positive, using default", "value", cf.PollInterval.Duration)
		}
	}
	if cf.PreRunDelay.isSet {
		if cf.PreRunDelay.Duration > 0 {
			cfg.PreRunDelay = cf.PreRunDelay.Duration
		} else {
			cfg.PreRunDelay = 0
		}
	}
	if cf.PostRunDelay.isSet {
		if cf.PostRunDelay.Duration > 0 {
			cfg.PostRunDelay = cf.PostRunDelay.Duration
		} else {
			cfg.PostRunDelay = 0
		}
	}

	rawReceivers := cf.AllowedReceivers
	if cf.Filtering.AllowedReceivers != nil {
		rawReceivers = cf.Filtering.AllowedReceivers
	}
	if rawReceivers != nil {
		normalized := make([]string, 0, len(*rawReceivers))
		for _, r := range *rawReceivers {
			normalized = append(normalized, strings.ToLower(r))
		}
		cfg.AllowedReceivers = normalized
	}

	if cf.Deduplication.IgnoredLabels != nil {
		cfg.IgnoredLabels = *cf.Deduplication.IgnoredLabels
	}

	cfg.Tools = ToolsConfig{
		Shared:       parseSkills(cf.Tools.Skills, "shared", logger),
		Analysis:     parseSkills(cf.Analysis.Tools.Skills, "analysis", logger),
		Execution:    parseSkills(cf.Execution.Tools.Skills, "execution", logger),
		Verification: parseSkills(cf.Verification.Tools.Skills, "verification", logger),
	}

	cfg.Agent = AgentConfig{
		Default:      cf.Agent.Default,
		Analysis:     cf.Agent.Analysis,
		Execution:    cf.Agent.Execution,
		Verification: cf.Agent.Verification,
	}

	return cfg, nil
}

func parseSkills(entries []skillsEntry, step string, logger *slog.Logger) []agenticv1alpha1.SkillsSource {
	var skills []agenticv1alpha1.SkillsSource
	for _, e := range entries {
		if e.Image == "" {
			logger.Warn("skills entry missing image, skipping", "step", step)
			continue
		}
		if len(e.Paths) == 0 {
			logger.Warn("skills entry missing paths, skipping", "step", step)
			continue
		}
		skills = append(skills, agenticv1alpha1.SkillsSource{
			Image: e.Image,
			Paths: e.Paths,
		})
	}
	return skills
}
