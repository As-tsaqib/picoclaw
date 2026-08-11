package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvolutionConfigDefaultsRemainDisabledAndSafetyEnabled(t *testing.T) {
	cfg := DefaultConfig().Evolution
	if cfg.Enabled {
		t.Fatal("evolution enabled by default")
	}
	if !cfg.PrivateDataScrubbing {
		t.Fatal("private-data scrubbing disabled by default")
	}
	if cfg.EffectiveApplyPolicy() != EvolutionApplyApprovalRequired {
		t.Fatalf("apply policy=%q, want approval_required", cfg.EffectiveApplyPolicy())
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default evolution validation: %v", err)
	}
}

func TestLoadConfigWithoutEvolutionUsesSafeDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{
		"version": 3,
		"agents": {"defaults": {"workspace": "/tmp/legacy-evolution"}},
		"model_list": [{"model_name": "legacy", "model": "openai/gpt-4o"}]
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Evolution.Enabled || !cfg.Evolution.PrivateDataScrubbing ||
		cfg.Evolution.EffectiveApplyPolicy() != EvolutionApplyApprovalRequired {
		t.Fatalf("legacy evolution defaults=%#v", cfg.Evolution)
	}
}

func TestEvolutionConfigValidationEnumsSchedulesAndBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EvolutionConfig)
	}{
		{name: "mode", mutate: func(cfg *EvolutionConfig) { cfg.Mode = "autonomous" }},
		{name: "apply policy", mutate: func(cfg *EvolutionConfig) { cfg.ApplyPolicy = "always" }},
		{
			name: "scrubbing required",
			mutate: func(cfg *EvolutionConfig) {
				cfg.Enabled = true
				cfg.PrivateDataScrubbing = false
			},
		},
		{name: "trigger", mutate: func(cfg *EvolutionConfig) { cfg.ColdPathTrigger = "hourly" }},
		{
			name: "scheduled time required",
			mutate: func(cfg *EvolutionConfig) {
				cfg.ColdPathTrigger = "scheduled"
				cfg.ColdPathTimes = nil
			},
		},
		{
			name: "scheduled time format",
			mutate: func(cfg *EvolutionConfig) {
				cfg.ColdPathTrigger = "scheduled"
				cfg.ColdPathTimes = []string{"24:00"}
			},
		},
		{name: "minimum tasks", mutate: func(cfg *EvolutionConfig) { cfg.MinTaskCount = 1 }},
		{name: "success ratio", mutate: func(cfg *EvolutionConfig) { cfg.MinSuccessRatio = 1.1 }},
		{name: "draft timeout", mutate: func(cfg *EvolutionConfig) { cfg.DraftTimeoutSeconds = 301 }},
		{name: "evidence records", mutate: func(cfg *EvolutionConfig) { cfg.MaxEvidenceRecords = 1 }},
		{name: "draft chars", mutate: func(cfg *EvolutionConfig) { cfg.MaxDraftChars = 50_001 }},
		{name: "rollback retention", mutate: func(cfg *EvolutionConfig) { cfg.RollbackRetention = 101 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := DefaultConfig().Evolution
			test.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() error=nil, want failure")
			}
		})
	}
}

func TestEvolutionConfigAcceptsDashboardEnumsAndLegacyManualTriggers(t *testing.T) {
	for _, mode := range []string{"observe", "draft", "apply"} {
		cfg := DefaultConfig().Evolution
		cfg.Enabled = true
		cfg.Mode = mode
		if err := cfg.Validate(); err != nil {
			t.Fatalf("mode %q validation: %v", mode, err)
		}
	}
	for _, policy := range []string{EvolutionApplyApprovalRequired, EvolutionApplyAutomatic} {
		cfg := DefaultConfig().Evolution
		cfg.ApplyPolicy = policy
		if err := cfg.Validate(); err != nil {
			t.Fatalf("policy %q validation: %v", policy, err)
		}
	}
	for _, trigger := range []string{"after_turn", "manual", "none", "off"} {
		cfg := DefaultConfig().Evolution
		cfg.ColdPathTrigger = trigger
		if err := cfg.Validate(); err != nil {
			t.Fatalf("trigger %q validation: %v", trigger, err)
		}
	}
	scheduled := DefaultConfig().Evolution
	scheduled.ColdPathTrigger = "scheduled"
	scheduled.ColdPathTimes = []string{"03:00", "15:30"}
	if err := scheduled.Validate(); err != nil {
		t.Fatalf("scheduled validation: %v", err)
	}
}
