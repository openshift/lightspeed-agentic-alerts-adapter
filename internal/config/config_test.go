package config

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	return path
}

func TestLoadFromFile(t *testing.T) {
	tests := []struct {
		name             string
		yaml             string
		wantPollInterval time.Duration
		wantPreRunDelay  time.Duration
		wantPostRunDelay time.Duration
	}{
		{
			name:             "valid full config",
			yaml:             "pollInterval: 45s\npreRunDelay: 10m\npostRunDelay: 30m\n",
			wantPollInterval: 45 * time.Second,
			wantPreRunDelay:  10 * time.Minute,
			wantPostRunDelay: 30 * time.Minute,
		},
		{
			name:             "partial config - only poll interval",
			yaml:             "pollInterval: 1m\n",
			wantPollInterval: time.Minute,
			wantPreRunDelay:  DefaultPreRunDelay,
			wantPostRunDelay: DefaultPostRunDelay,
		},
		{
			name:             "partial config - only postRunDelay",
			yaml:             "postRunDelay: 2h\n",
			wantPollInterval: DefaultPollInterval,
			wantPreRunDelay:  DefaultPreRunDelay,
			wantPostRunDelay: 2 * time.Hour,
		},
		{
			name:             "empty yaml",
			yaml:             "",
			wantPollInterval: DefaultPollInterval,
			wantPreRunDelay:  DefaultPreRunDelay,
			wantPostRunDelay: DefaultPostRunDelay,
		},
		{
			name:             "empty document",
			yaml:             "---\n",
			wantPollInterval: DefaultPollInterval,
			wantPreRunDelay:  DefaultPreRunDelay,
			wantPostRunDelay: DefaultPostRunDelay,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, tt.yaml)

			cfg, err := LoadFromFile(path, quietLogger())
			if err != nil {
				t.Fatalf("LoadFromFile() error = %v", err)
			}

			if cfg.PollInterval != tt.wantPollInterval {
				t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, tt.wantPollInterval)
			}
			if cfg.PreRunDelay != tt.wantPreRunDelay {
				t.Errorf("PreRunDelay = %v, want %v", cfg.PreRunDelay, tt.wantPreRunDelay)
			}
			if cfg.PostRunDelay != tt.wantPostRunDelay {
				t.Errorf("PostRunDelay = %v, want %v", cfg.PostRunDelay, tt.wantPostRunDelay)
			}
		})
	}
}

func TestLoadFromFileInvalid(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "invalid yaml",
			yaml: "key: [unclosed bracket",
		},
		{
			name: "invalid duration value",
			yaml: "pollInterval: not-a-duration\n",
		},
		{
			name: "invalid preRunDelay",
			yaml: "preRunDelay: abc\n",
		},
		{
			name: "invalid postRunDelay",
			yaml: "postRunDelay: xyz\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, tt.yaml)

			_, err := LoadFromFile(path, quietLogger())
			if err == nil {
				t.Fatal("LoadFromFile() expected error, got nil")
			}
		})
	}
}

func TestNonPositiveDurationsClampToZero(t *testing.T) {
	tests := []struct {
		name             string
		yaml             string
		wantPollInterval time.Duration
		wantPreRunDelay  time.Duration
		wantPostRunDelay time.Duration
	}{
		{
			name:             "zero poll interval falls back to default",
			yaml:             "pollInterval: 0s",
			wantPollInterval: DefaultPollInterval,
			wantPreRunDelay:  DefaultPreRunDelay,
			wantPostRunDelay: DefaultPostRunDelay,
		},
		{
			name:             "negative preRunDelay clamped to 0",
			yaml:             "preRunDelay: -5m",
			wantPollInterval: DefaultPollInterval,
			wantPreRunDelay:  0,
			wantPostRunDelay: DefaultPostRunDelay,
		},
		{
			name:             "negative postRunDelay clamped to 0",
			yaml:             "postRunDelay: -1h",
			wantPollInterval: DefaultPollInterval,
			wantPreRunDelay:  DefaultPreRunDelay,
			wantPostRunDelay: 0,
		},
		{
			name:             "explicit zero preRunDelay and postRunDelay override defaults",
			yaml:             "preRunDelay: 0s\npostRunDelay: 0s",
			wantPollInterval: DefaultPollInterval,
			wantPreRunDelay:  0,
			wantPostRunDelay: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, tt.yaml)

			cfg, err := LoadFromFile(path, quietLogger())
			if err != nil {
				t.Fatalf("LoadFromFile() error = %v", err)
			}

			if cfg.PollInterval != tt.wantPollInterval {
				t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, tt.wantPollInterval)
			}
			if cfg.PreRunDelay != tt.wantPreRunDelay {
				t.Errorf("PreRunDelay = %v, want %v", cfg.PreRunDelay, tt.wantPreRunDelay)
			}
			if cfg.PostRunDelay != tt.wantPostRunDelay {
				t.Errorf("PostRunDelay = %v, want %v", cfg.PostRunDelay, tt.wantPostRunDelay)
			}
		})
	}
}

func TestLoadFromFileMissing(t *testing.T) {
	cfg, err := LoadFromFile("/nonexistent/path/config.yaml", quietLogger())
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	assertDefaults(t, cfg)
}

func assertDefaults(t *testing.T, cfg Config) {
	t.Helper()
	defaults := Default()
	if cfg.PollInterval != defaults.PollInterval {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, defaults.PollInterval)
	}
	if cfg.PreRunDelay != defaults.PreRunDelay {
		t.Errorf("PreRunDelay = %v, want %v", cfg.PreRunDelay, defaults.PreRunDelay)
	}
	if cfg.PostRunDelay != defaults.PostRunDelay {
		t.Errorf("PostRunDelay = %v, want %v", cfg.PostRunDelay, defaults.PostRunDelay)
	}
	assertReceiversEqual(t, cfg.AllowedReceivers, defaults.AllowedReceivers)
	assertStringSliceEqual(t, "IgnoredLabels", cfg.IgnoredLabels, DefaultIgnoredLabels)
	assertEmptyTools(t, cfg.Tools)
	if cfg.Agent != (AgentConfig{}) {
		t.Errorf("Agent = %+v, want zero value", cfg.Agent)
	}
}

func assertEmptyTools(t *testing.T, tc ToolsConfig) {
	t.Helper()
	if len(tc.Shared) != 0 {
		t.Errorf("Tools.Shared = %v, want empty", tc.Shared)
	}
}

func TestParseToolsConfig(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantTools ToolsConfig
	}{
		{
			name: "shared skills only",
			yaml: `
tools:
  skills:
    - image: registry.example.com/skills:latest
      paths:
        - /skills/prometheus
`,
			wantTools: ToolsConfig{
				Shared: []agenticv1alpha1.SkillsSource{
					{Image: "registry.example.com/skills:latest", Paths: []string{"/skills/prometheus"}},
				},
			},
		},
		{
			name: "multiple skills in shared",
			yaml: `
tools:
  skills:
    - image: registry.example.com/skills:latest
      paths:
        - /skills/prometheus
        - /skills/cluster-diagnostics
    - image: registry.example.com/acs:latest
      paths:
        - /skills/acs
`,
			wantTools: ToolsConfig{
				Shared: []agenticv1alpha1.SkillsSource{
					{Image: "registry.example.com/skills:latest", Paths: []string{"/skills/prometheus", "/skills/cluster-diagnostics"}},
					{Image: "registry.example.com/acs:latest", Paths: []string{"/skills/acs"}},
				},
			},
		},
		{
			name:      "no tools key",
			yaml:      "pollInterval: 30s",
			wantTools: ToolsConfig{},
		},
		{
			name: "empty skills list",
			yaml: `
tools:
  skills: []
`,
			wantTools: ToolsConfig{},
		},
		{
			name: "shared skills entry with empty image skipped",
			yaml: `
tools:
  skills:
    - image: ""
      paths:
        - /skills/prometheus
`,
			wantTools: ToolsConfig{},
		},
		{
			name: "shared skills entry with empty paths skipped",
			yaml: `
tools:
  skills:
    - image: registry.example.com/skills:latest
      paths: []
`,
			wantTools: ToolsConfig{},
		},
		{
			name: "mix of valid and invalid shared skills",
			yaml: `
tools:
  skills:
    - image: ""
      paths:
        - /skills/bad
    - image: registry.example.com/no-paths:latest
      paths: []
    - image: registry.example.com/good:latest
      paths:
        - /skills/good
`,
			wantTools: ToolsConfig{
				Shared: []agenticv1alpha1.SkillsSource{
					{Image: "registry.example.com/good:latest", Paths: []string{"/skills/good"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, tt.yaml)

			cfg, err := LoadFromFile(path, quietLogger())
			if err != nil {
				t.Fatalf("LoadFromFile() error = %v", err)
			}

			assertSkillsEqual(t, "Tools.Shared", cfg.Tools.Shared, tt.wantTools.Shared)
		})
	}
}

func TestParseAllowedReceivers(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []string
	}{
		{
			name: "single receiver",
			yaml: "allowedReceivers:\n  - Critical\n",
			want: []string{"critical"},
		},
		{
			name: "multiple receivers",
			yaml: "allowedReceivers:\n  - Critical\n  - PagerDuty\n",
			want: []string{"critical", "pagerduty"},
		},
		{
			name: "mixed case normalized to lowercase",
			yaml: "allowedReceivers:\n  - CRITICAL\n  - Slack-OnCall\n",
			want: []string{"critical", "slack-oncall"},
		},
		{
			name: "field absent defaults to empty",
			yaml: "pollInterval: 30s\n",
			want: nil,
		},
		{
			name: "explicit empty list",
			yaml: "allowedReceivers: []\n",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, tt.yaml)

			cfg, err := LoadFromFile(path, quietLogger())
			if err != nil {
				t.Fatalf("LoadFromFile() error = %v", err)
			}

			assertReceiversEqual(t, cfg.AllowedReceivers, tt.want)
		})
	}
}

func TestParseIgnoredLabels(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []string
	}{
		{
			name: "absent defaults to default list",
			yaml: "pollInterval: 30s\n",
			want: DefaultIgnoredLabels,
		},
		{
			name: "explicit list replaces defaults",
			yaml: "deduplication:\n  ignoredLabels:\n    - pod\n    - instance\n    - job\n",
			want: []string{"pod", "instance", "job"},
		},
		{
			name: "empty list means no stripping",
			yaml: "deduplication:\n  ignoredLabels: []\n",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, tt.yaml)

			cfg, err := LoadFromFile(path, quietLogger())
			if err != nil {
				t.Fatalf("LoadFromFile() error = %v", err)
			}

			assertStringSliceEqual(t, "IgnoredLabels", cfg.IgnoredLabels, tt.want)
		})
	}
}

func TestParseFilteringAllowedReceivers(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []string
	}{
		{
			name: "filtering.allowedReceivers",
			yaml: "filtering:\n  allowedReceivers:\n    - Critical\n",
			want: []string{"critical"},
		},
		{
			name: "top-level allowedReceivers still works",
			yaml: "allowedReceivers:\n  - PagerDuty\n",
			want: []string{"pagerduty"},
		},
		{
			name: "filtering section takes precedence over top-level",
			yaml: "allowedReceivers:\n  - PagerDuty\nfiltering:\n  allowedReceivers:\n    - Critical\n",
			want: []string{"critical"},
		},
		{
			name: "filtering section with empty list",
			yaml: "filtering:\n  allowedReceivers: []\n",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, tt.yaml)

			cfg, err := LoadFromFile(path, quietLogger())
			if err != nil {
				t.Fatalf("LoadFromFile() error = %v", err)
			}

			assertReceiversEqual(t, cfg.AllowedReceivers, tt.want)
		})
	}
}

func TestAllowedReceiversDefaultsOnMissingFile(t *testing.T) {
	cfg, err := LoadFromFile("/nonexistent/path/config.yaml", quietLogger())
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	assertReceiversEqual(t, cfg.AllowedReceivers, nil)
}

func assertReceiversEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("AllowedReceivers length = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("AllowedReceivers[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestParseAgentConfig(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantAgent AgentConfig
	}{
		{
			name: "global default agent only",
			yaml: `
agent:
  default: "my-agent"
`,
			wantAgent: AgentConfig{Default: "my-agent"},
		},
		{
			name: "per-step agents only",
			yaml: `
agent:
  analysis: "analyzer"
  execution: "executor"
  verification: "verifier"
`,
			wantAgent: AgentConfig{
				Analysis:     "analyzer",
				Execution:    "executor",
				Verification: "verifier",
			},
		},
		{
			name: "global and per-step agents mixed",
			yaml: `
agent:
  default: "global-agent"
  analysis: "analyzer"
`,
			wantAgent: AgentConfig{
				Default:  "global-agent",
				Analysis: "analyzer",
			},
		},
		{
			name:      "no agent section",
			yaml:      "pollInterval: 30s",
			wantAgent: AgentConfig{},
		},
		{
			name: "agent section with empty strings",
			yaml: `
agent:
  default: ""
  analysis: ""
`,
			wantAgent: AgentConfig{},
		},
		{
			name: "full agent config",
			yaml: `
agent:
  default: "my-agent"
  analysis: "analyzer"
  execution: "executor"
  verification: "verifier"
`,
			wantAgent: AgentConfig{
				Default:      "my-agent",
				Analysis:     "analyzer",
				Execution:    "executor",
				Verification: "verifier",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, tt.yaml)

			cfg, err := LoadFromFile(path, quietLogger())
			if err != nil {
				t.Fatalf("LoadFromFile() error = %v", err)
			}

			if cfg.Agent != tt.wantAgent {
				t.Errorf("Agent = %+v, want %+v", cfg.Agent, tt.wantAgent)
			}
		})
	}
}

func assertStringSliceEqual(t *testing.T, field string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s length = %d, want %d (got %v)", field, len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("%s[%d] = %q, want %q", field, i, got[i], w)
		}
	}
}

func assertSkillsEqual(t *testing.T, field string, got, want []agenticv1alpha1.SkillsSource) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s length = %d, want %d", field, len(got), len(want))
	}
	for i, w := range want {
		if got[i].Image != w.Image {
			t.Errorf("%s[%d].image = %q, want %q", field, i, got[i].Image, w.Image)
		}
		if len(got[i].Paths) != len(w.Paths) {
			t.Fatalf("%s[%d].paths length = %d, want %d", field, i, len(got[i].Paths), len(w.Paths))
		}
		for j, p := range w.Paths {
			if got[i].Paths[j] != p {
				t.Errorf("%s[%d].paths[%d] = %q, want %q", field, i, j, got[i].Paths[j], p)
			}
		}
	}
}
