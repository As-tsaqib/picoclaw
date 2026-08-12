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
		{"approval mode", func(cfg *MemoryConfig) { cfg.ApprovalMode = "sometimes" }},
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
		{"profile chars", func(cfg *MemoryConfig) { cfg.Profile.MaxChars = MaxMemoryProfileChars + 1 }},
		{"profile confidence", func(cfg *MemoryConfig) { cfg.Profile.MinConfidence = 1.1 }},
		{"retrieval user share", func(cfg *MemoryConfig) { cfg.Retrieval.UserShare = 0.4 }},
		{"retrieval engine", func(cfg *MemoryConfig) { cfg.Retrieval.Engine = "vector" }},
		{"retrieval workspace results", func(cfg *MemoryConfig) { cfg.Retrieval.MaxWorkspaceResults = 51 }},
		{"retrieval user results", func(cfg *MemoryConfig) { cfg.Retrieval.MaxUserResults = 51 }},
		{"retrieval chars", func(cfg *MemoryConfig) { cfg.Retrieval.MaxTotalChars = 20_001 }},
		{"pinned chars", func(cfg *MemoryConfig) { cfg.Retrieval.PinnedCharBudget = 10_001 }},
		{"minimum relevance", func(cfg *MemoryConfig) { cfg.Retrieval.MinimumScore = 10.1 }},
		{"recency weight", func(cfg *MemoryConfig) { cfg.Retrieval.RecencyWeight = 5.1 }},
		{"recency half life", func(cfg *MemoryConfig) { cfg.Retrieval.RecencyHalfLifeDays = 3_651 }},
		{"fuzzy weight", func(cfg *MemoryConfig) { cfg.Retrieval.FuzzyWeight = 5.1 }},
		{"fallback count", func(cfg *MemoryConfig) { cfg.Retrieval.RecentFallbackCount = 51 }},
		{"archive retention", func(cfg *MemoryConfig) { cfg.Lifecycle.ArchivedRetentionDays = 3_651 }},
		{"stale threshold", func(cfg *MemoryConfig) { cfg.Lifecycle.StaleThresholdDays = 3_651 }},
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

func TestMemoryRetrievalEngineModes(t *testing.T) {
	for _, engine := range []string{MemoryRetrievalHybridLexical, MemoryRetrievalSemanticRerank} {
		cfg := DefaultConfig().Memory
		cfg.Retrieval.Engine = engine
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v", engine, err)
		}
		if got := cfg.Retrieval.EffectiveEngine(); got != engine {
			t.Fatalf("EffectiveEngine(%q) = %q", engine, got)
		}
	}
	if got := (MemoryRetrievalConfig{Engine: "unknown"}).EffectiveEngine(); got != MemoryRetrievalHybridLexical {
		t.Fatalf("unknown engine fallback = %q", got)
	}
}

func TestMemoryApprovalModeLegacyMappingAndExplicitPrecedence(t *testing.T) {
	tests := []struct {
		name string
		cfg  MemoryConfig
		want string
	}{
		{name: "legacy false", cfg: MemoryConfig{WriteApproval: false}, want: MemoryApprovalOff},
		{name: "legacy true", cfg: MemoryConfig{WriteApproval: true}, want: MemoryApprovalBackgroundOnly},
		{
			name: "explicit off wins",
			cfg:  MemoryConfig{WriteApproval: true, ApprovalMode: MemoryApprovalOff},
			want: MemoryApprovalOff,
		},
		{
			name: "explicit background",
			cfg:  MemoryConfig{ApprovalMode: MemoryApprovalBackgroundOnly},
			want: MemoryApprovalBackgroundOnly,
		},
		{
			name: "explicit all writes",
			cfg:  MemoryConfig{ApprovalMode: MemoryApprovalAllWrites},
			want: MemoryApprovalAllWrites,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.cfg.EffectiveApprovalMode(); got != test.want {
				t.Fatalf("EffectiveApprovalMode()=%q, want %q", got, test.want)
			}
		})
	}
	if (MemoryConfig{ApprovalMode: MemoryApprovalOff}).ShouldStageMemoryWrite(true) {
		t.Fatal("off staged background write")
	}
	background := MemoryConfig{ApprovalMode: MemoryApprovalBackgroundOnly}
	if !background.ShouldStageMemoryWrite(true) || background.ShouldStageMemoryWrite(false) {
		t.Fatal("background_only staging semantics are incorrect")
	}
	all := MemoryConfig{ApprovalMode: MemoryApprovalAllWrites}
	if !all.ShouldStageMemoryWrite(true) || !all.ShouldStageMemoryWrite(false) {
		t.Fatal("all_writes did not stage every model write")
	}
}

func TestDefaultConfigMemoryPersonalizationDefaults(t *testing.T) {
	cfg := DefaultConfig().Memory
	if !cfg.Profile.Enabled {
		t.Fatal("memory.profile.enabled = false, want true")
	}
	if got := cfg.Profile.EffectiveMaxChars(); got != DefaultMemoryProfileChars {
		t.Fatalf("profile max chars = %d, want %d", got, DefaultMemoryProfileChars)
	}
	if got := cfg.Profile.EffectiveMinConfidence(); got != DefaultMemoryProfileMinScore {
		t.Fatalf("profile min confidence = %v, want %v", got, DefaultMemoryProfileMinScore)
	}
	if got := cfg.Retrieval.EffectiveUserShare(); got != DefaultMemoryUserShare {
		t.Fatalf("retrieval user share = %v, want %v", got, DefaultMemoryUserShare)
	}
	if got := cfg.BackgroundReview.EffectiveMaxIterations(); got != DefaultMemoryReviewIterations {
		t.Fatalf("review iterations = %d, want %d", got, DefaultMemoryReviewIterations)
	}
}

func TestMemoryPersonalizationEffectiveDefaultsRemainBackwardCompatible(t *testing.T) {
	legacy := MemoryConfig{}
	if got := legacy.Profile.EffectiveMaxChars(); got != DefaultMemoryProfileChars {
		t.Fatalf("legacy profile max chars = %d, want %d", got, DefaultMemoryProfileChars)
	}
	if got := legacy.Profile.EffectiveMinConfidence(); got != DefaultMemoryProfileMinScore {
		t.Fatalf("legacy profile min confidence = %v, want %v", got, DefaultMemoryProfileMinScore)
	}
	if got := legacy.Retrieval.EffectiveUserShare(); got != DefaultMemoryUserShare {
		t.Fatalf("legacy retrieval user share = %v, want %v", got, DefaultMemoryUserShare)
	}
}
