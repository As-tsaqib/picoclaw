package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestHandlePatchConfigMemoryRoundTripPreservesOtherFields(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()

	cfg, loadErr := config.LoadConfig(configPath)
	if loadErr != nil {
		t.Fatalf("LoadConfig() error = %v", loadErr)
	}
	cfg.Gateway.Port = 19444
	cfg.Agents.Defaults.MaxTokens = 8192
	if saveErr := config.SaveConfig(configPath, cfg); saveErr != nil {
		t.Fatalf("SaveConfig() error = %v", saveErr)
	}

	patch := `{
		"memory": {
			"enabled": false,
			"workspace_char_limit": 24000,
			"per_user_char_limit": 16000,
			"write_approval": true,
			"approval_mode": "all_writes",
			"notifications": "verbose",
			"background_review": {
				"enabled": false,
				"interval": 17,
				"provider": "openai",
				"model": "review-model",
				"timeout_seconds": 45,
				"max_iterations": 4
			},
			"retrieval": {
				"enabled": true,
				"engine": "hybrid_lexical",
				"max_workspace_results": 11,
				"max_user_results": 12,
				"max_total_chars": 8765,
				"pinned_char_budget": 2100,
				"minimum_relevance_score": 0.42,
				"recency_weight": 0.33,
				"recency_half_life_days": 45,
				"fuzzy_weight": 0.91,
				"recent_fallback_count": 4
			},
			"lifecycle": {
				"archived_retention_days": 730,
				"stale_threshold_days": 75,
				"auto_archive_expired": true
			},
			"recall": {
				"mode": "group_recall",
				"max_results": 9,
				"max_chars": 9000
			},
			"checkpoints": {
				"enabled": true,
				"max_count": 321,
				"max_context_chars": 6789,
				"completed_retention_days": 120
			}
		},
		"evolution": {
			"enabled": true,
			"mode": "draft",
			"state_dir": "/tmp/picoclaw-evolution-state",
			"min_task_count": 4,
			"min_success_ratio": 0.85,
			"cold_path_trigger": "scheduled",
			"cold_path_times": ["03:00", "15:30"],
			"apply_policy": "approval_required",
			"private_data_scrubbing": true,
			"draft_timeout_seconds": 90,
			"max_evidence_records": 120,
			"max_draft_chars": 24000,
			"rollback_retention": 20
		}
	}`
	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPatch, "/api/config", bytes.NewBufferString(patch))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig(updated) error = %v", err)
	}
	if updated.Gateway.Port != 19444 || updated.Agents.Defaults.MaxTokens != 8192 {
		t.Fatalf(
			"unrelated fields changed: gateway.port=%d max_tokens=%d",
			updated.Gateway.Port,
			updated.Agents.Defaults.MaxTokens,
		)
	}
	memoryCfg := updated.Memory
	if memoryCfg.Enabled || memoryCfg.BackgroundReview.Enabled ||
		!memoryCfg.WriteApproval || !memoryCfg.Checkpoints.Enabled {
		t.Fatalf("memory booleans did not round trip: %#v", memoryCfg)
	}
	if memoryCfg.Notifications != config.MemoryNotificationVerbose ||
		memoryCfg.EffectiveApprovalMode() != config.MemoryApprovalAllWrites ||
		memoryCfg.BackgroundReview.Interval != 17 ||
		memoryCfg.BackgroundReview.Provider != "openai" ||
		memoryCfg.BackgroundReview.Model != "review-model" ||
		memoryCfg.BackgroundReview.TimeoutSeconds != 45 ||
		memoryCfg.BackgroundReview.MaxIterations != 4 ||
		memoryCfg.WorkspaceCharLimit != 24000 ||
		memoryCfg.PerUserCharLimit != 16000 ||
		memoryCfg.Recall.Mode != config.MemoryRecallGroupRecall ||
		memoryCfg.Recall.MaxResults != 9 || memoryCfg.Recall.MaxChars != 9000 ||
		memoryCfg.Checkpoints.MaxCount != 321 ||
		memoryCfg.Checkpoints.MaxContextChars != 6789 ||
		memoryCfg.Checkpoints.CompletedRetentionDays != 120 ||
		!memoryCfg.Retrieval.Enabled ||
		memoryCfg.Retrieval.Engine != config.MemoryRetrievalHybridLexical ||
		memoryCfg.Retrieval.MaxWorkspaceResults != 11 ||
		memoryCfg.Retrieval.MaxUserResults != 12 ||
		memoryCfg.Retrieval.MaxTotalChars != 8765 ||
		memoryCfg.Retrieval.PinnedCharBudget != 2100 ||
		memoryCfg.Retrieval.MinimumScore != 0.42 ||
		memoryCfg.Retrieval.RecencyWeight != 0.33 ||
		memoryCfg.Retrieval.RecencyHalfLifeDays != 45 ||
		memoryCfg.Retrieval.FuzzyWeight != 0.91 ||
		memoryCfg.Retrieval.RecentFallbackCount != 4 ||
		memoryCfg.Lifecycle.ArchivedRetentionDays != 730 ||
		memoryCfg.Lifecycle.StaleThresholdDays != 75 ||
		!memoryCfg.Lifecycle.AutoArchiveExpired {
		t.Fatalf("memory values did not round trip: %#v", memoryCfg)
	}
	evolutionCfg := updated.Evolution
	if !evolutionCfg.Enabled || evolutionCfg.Mode != "draft" ||
		evolutionCfg.StateDir != "/tmp/picoclaw-evolution-state" ||
		evolutionCfg.MinTaskCount != 4 || evolutionCfg.MinSuccessRatio != 0.85 ||
		evolutionCfg.ColdPathTrigger != "scheduled" ||
		len(evolutionCfg.ColdPathTimes) != 2 || evolutionCfg.ColdPathTimes[0] != "03:00" ||
		evolutionCfg.ApplyPolicy != config.EvolutionApplyApprovalRequired ||
		!evolutionCfg.PrivateDataScrubbing || evolutionCfg.DraftTimeoutSeconds != 90 ||
		evolutionCfg.MaxEvidenceRecords != 120 || evolutionCfg.MaxDraftChars != 24000 ||
		evolutionCfg.RollbackRetention != 20 {
		t.Fatalf("evolution values did not round trip: %#v", evolutionCfg)
	}
}

func TestHandleGetConfigWithoutMemoryReturnsActiveDefaults(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()

	raw, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	var document map[string]any
	if unmarshalErr := json.Unmarshal(raw, &document); unmarshalErr != nil {
		t.Fatalf("Unmarshal() error = %v", unmarshalErr)
	}
	delete(document, "memory")
	delete(document, "evolution")
	raw, marshalErr := json.Marshal(document)
	if marshalErr != nil {
		t.Fatalf("Marshal() error = %v", marshalErr)
	}
	if writeErr := os.WriteFile(configPath, raw, 0o600); writeErr != nil {
		t.Fatalf("WriteFile() error = %v", writeErr)
	}

	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response config.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal(response) error = %v", err)
	}
	if !response.Memory.Enabled || !response.Memory.BackgroundReview.Enabled ||
		response.Memory.BackgroundReview.Interval != 10 {
		t.Fatalf("GET memory defaults = %#v, want active defaults", response.Memory)
	}
	if response.Evolution.Enabled || !response.Evolution.PrivateDataScrubbing ||
		response.Evolution.EffectiveApplyPolicy() != config.EvolutionApplyApprovalRequired {
		t.Fatalf("GET evolution defaults = %#v, want disabled safe defaults", response.Evolution)
	}
}

func TestHandlePatchConfigRejectsInvalidMemoryValues(t *testing.T) {
	tests := []string{
		`{"memory":{"notifications":"sometimes"}}`,
		`{"memory":{"approval_mode":"sometimes"}}`,
		`{"memory":{"background_review":{"max_iterations":5}}}`,
		`{"memory":{"retrieval":{"engine":"vector"}}}`,
		`{"memory":{"retrieval":{"max_workspace_results":51}}}`,
		`{"memory":{"retrieval":{"max_total_chars":20001}}}`,
		`{"memory":{"retrieval":{"pinned_char_budget":10001}}}`,
		`{"memory":{"retrieval":{"minimum_relevance_score":10.1}}}`,
		`{"memory":{"retrieval":{"recency_weight":5.1}}}`,
		`{"memory":{"retrieval":{"fuzzy_weight":5.1}}}`,
		`{"memory":{"lifecycle":{"stale_threshold_days":3651}}}`,
		`{"memory":{"recall":{"mode":"everywhere"}}}`,
		`{"memory":{"recall":{"max_results":21}}}`,
		`{"memory":{"recall":{"max_chars":20001}}}`,
		`{"memory":{"checkpoints":{"max_count":1001}}}`,
		`{"memory":{"checkpoints":{"max_context_chars":20001}}}`,
		`{"evolution":{"mode":"autonomous"}}`,
		`{"evolution":{"apply_policy":"always"}}`,
		`{"evolution":{"enabled":true,"private_data_scrubbing":false}}`,
		`{"evolution":{"cold_path_trigger":"hourly"}}`,
		`{"evolution":{"cold_path_trigger":"scheduled","cold_path_times":[]}}`,
		`{"evolution":{"cold_path_trigger":"scheduled","cold_path_times":["25:99"]}}`,
		`{"evolution":{"min_task_count":1}}`,
		`{"evolution":{"min_success_ratio":1.1}}`,
		`{"evolution":{"draft_timeout_seconds":301}}`,
		`{"evolution":{"max_evidence_records":1}}`,
		`{"evolution":{"max_draft_chars":50001}}`,
		`{"evolution":{"rollback_retention":101}}`,
	}

	for _, patch := range tests {
		t.Run(patch, func(t *testing.T) {
			configPath, cleanup := setupOAuthTestEnv(t)
			defer cleanup()
			h := NewHandler(configPath)
			mux := http.NewServeMux()
			h.RegisterRoutes(mux)
			req := httptest.NewRequest(
				http.MethodPatch,
				"/api/config",
				bytes.NewBufferString(patch),
			)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf(
					"PATCH status = %d, want %d, body=%s",
					rec.Code,
					http.StatusBadRequest,
					rec.Body.String(),
				)
			}
		})
	}
}

func TestHandleUpdateConfigWithoutMemoryAppliesActiveDefaults(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()
	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	body := `{
		"version": 3,
		"agents": {"defaults": {"workspace": "/tmp/dashboard"}},
		"model_list": [{"model_name": "custom", "model": "openai/gpt-4o"}]
	}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig(updated) error = %v", err)
	}
	if !updated.Memory.Enabled || !updated.Memory.BackgroundReview.Enabled ||
		updated.Memory.BackgroundReview.Interval != 10 {
		t.Fatalf("PUT memory defaults = %#v, want active defaults", updated.Memory)
	}
	if updated.Evolution.Enabled || !updated.Evolution.PrivateDataScrubbing ||
		updated.Evolution.EffectiveApplyPolicy() != config.EvolutionApplyApprovalRequired {
		t.Fatalf("PUT evolution defaults = %#v, want disabled safe defaults", updated.Evolution)
	}
}
