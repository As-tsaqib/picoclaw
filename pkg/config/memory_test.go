package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigMemoryFeaturesAreActive(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Memory.Enabled {
		t.Fatal("memory.enabled = false, want true")
	}
	if !cfg.Memory.BackgroundReview.Enabled {
		t.Fatal("memory.background_review.enabled = false, want true")
	}
	if got := cfg.Memory.BackgroundReview.Interval; got != 10 {
		t.Fatalf("memory.background_review.interval = %d, want 10", got)
	}
	if err := cfg.Memory.Validate(); err != nil {
		t.Fatalf("default memory config failed validation: %v", err)
	}
}

func TestLoadConfigWithoutMemoryUsesActiveDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{
		"version": 3,
		"agents": {"defaults": {"workspace": "/tmp/legacy-workspace"}},
		"model_list": [{"model_name": "legacy", "model": "openai/gpt-4o"}]
	}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !cfg.Memory.Enabled || !cfg.Memory.BackgroundReview.Enabled {
		t.Fatalf("legacy memory defaults = %#v, want memory and reviewer enabled", cfg.Memory)
	}
	if got := cfg.Memory.BackgroundReview.Interval; got != DefaultMemoryReviewInterval {
		t.Fatalf("review interval = %d, want %d", got, DefaultMemoryReviewInterval)
	}
}

func TestMemoryConfigValidateEnumsAndBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*MemoryConfig)
	}{
		{"notification", func(cfg *MemoryConfig) { cfg.Notifications = "sometimes" }},
		{"review interval", func(cfg *MemoryConfig) { cfg.BackgroundReview.Interval = 0 }},
		{"review timeout", func(cfg *MemoryConfig) { cfg.BackgroundReview.TimeoutSeconds = 0 }},
		{"review iterations", func(cfg *MemoryConfig) { cfg.BackgroundReview.MaxIterations = 5 }},
		{"recall mode", func(cfg *MemoryConfig) { cfg.Recall.Mode = "everywhere" }},
		{"recall results", func(cfg *MemoryConfig) { cfg.Recall.MaxResults = 21 }},
		{"recall chars", func(cfg *MemoryConfig) { cfg.Recall.MaxChars = 20_001 }},
		{"recall records", func(cfg *MemoryConfig) { cfg.Recall.MaxRecords = 20_001 }},
		{"workspace capacity", func(cfg *MemoryConfig) { cfg.WorkspaceCharLimit = 0 }},
		{"user capacity", func(cfg *MemoryConfig) { cfg.PerUserCharLimit = -1 }},
		{"checkpoint count", func(cfg *MemoryConfig) { cfg.Checkpoints.MaxCount = 1_001 }},
		{"checkpoint context", func(cfg *MemoryConfig) { cfg.Checkpoints.MaxContextChars = 20_001 }},
		{"checkpoint retention", func(cfg *MemoryConfig) { cfg.Checkpoints.CompletedRetentionDays = 0 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			memoryCfg := DefaultConfig().Memory
			test.mutate(&memoryCfg)
			if err := memoryCfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want validation failure")
			}
		})
	}
}
