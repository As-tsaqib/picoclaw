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

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Gateway.Port = 19444
	cfg.Agents.Defaults.MaxTokens = 8192
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	patch := `{
		"memory": {
			"enabled": false,
			"workspace_char_limit": 24000,
			"per_user_char_limit": 16000,
			"write_approval": true,
			"notifications": "verbose",
			"background_review": {
				"enabled": false,
				"interval": 17,
				"provider": "openai",
				"model": "review-model",
				"timeout_seconds": 45,
				"max_iterations": 4
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
		t.Fatalf("unrelated fields changed: gateway.port=%d max_tokens=%d", updated.Gateway.Port, updated.Agents.Defaults.MaxTokens)
	}
	memoryCfg := updated.Memory
	if memoryCfg.Enabled || memoryCfg.BackgroundReview.Enabled || !memoryCfg.WriteApproval || !memoryCfg.Checkpoints.Enabled {
		t.Fatalf("memory booleans did not round trip: %#v", memoryCfg)
	}
	if memoryCfg.Notifications != config.MemoryNotificationVerbose ||
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
		memoryCfg.Checkpoints.CompletedRetentionDays != 120 {
		t.Fatalf("memory values did not round trip: %#v", memoryCfg)
	}
}

func TestHandleGetConfigWithoutMemoryReturnsActiveDefaults(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	delete(document, "memory")
	raw, err = json.Marshal(document)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
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
}

func TestHandlePatchConfigRejectsInvalidMemoryValues(t *testing.T) {
	tests := []string{
		`{"memory":{"notifications":"sometimes"}}`,
		`{"memory":{"background_review":{"max_iterations":5}}}`,
		`{"memory":{"recall":{"mode":"everywhere"}}}`,
		`{"memory":{"recall":{"max_results":21}}}`,
		`{"memory":{"recall":{"max_chars":20001}}}`,
		`{"memory":{"checkpoints":{"max_count":1001}}}`,
		`{"memory":{"checkpoints":{"max_context_chars":20001}}}`,
	}

	for _, patch := range tests {
		t.Run(patch, func(t *testing.T) {
			configPath, cleanup := setupOAuthTestEnv(t)
			defer cleanup()
			h := NewHandler(configPath)
			mux := http.NewServeMux()
			h.RegisterRoutes(mux)
			req := httptest.NewRequest(http.MethodPatch, "/api/config", bytes.NewBufferString(patch))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("PATCH status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
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
}
