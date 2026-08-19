package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
)

func TestMemoryManageCaptureModeSemantics(t *testing.T) {
	store, err := memory.NewCuratedStore(
		filepath.Join(t.TempDir(), "curated"),
		memory.CuratedStoreOptions{},
	)
	if err != nil {
		t.Fatalf("NewCuratedStore: %v", err)
	}
	tool := NewMemoryManageToolWithApprovalMode(store, config.MemoryApprovalOff, nil)
	baseCaller := memory.CallerScope{
		AgentID: "main", UserKey: "person:owner", Channel: "telegram", Account: "default",
		ChatID: "123", SessionKey: "session-a", SessionRef: "session-ref-a", MessageRef: "11",
	}
	preferenceArgs := map[string]any{
		"action": "add", "target": "current_user", "content": "Prefers concise answers",
		"type": memory.CuratedTypeCommunicationPreference, "evidence_kind": memory.CuratedEvidenceExplicit,
		"preference_key": "communication.verbosity", "preference_value": "concise",
	}

	automatic := baseCaller
	automatic.CaptureMode = config.MemoryCaptureAutomatic
	automaticCtx := WithToolCallerScope(context.Background(), automatic)
	result := tool.Execute(automaticCtx, preferenceArgs)
	if result.IsError {
		t.Fatalf("automatic capture rejected natural preference: %v", result.Err)
	}

	explicitOnlyNatural := baseCaller
	explicitOnlyNatural.MessageRef = "12"
	explicitOnlyNatural.CaptureMode = config.MemoryCaptureExplicitOnly
	explicitOnlyNatural.ExplicitMemoryIntent = false
	naturalCtx := WithToolCallerScope(context.Background(), explicitOnlyNatural)
	result = tool.Execute(naturalCtx, map[string]any{
		"action": "add", "target": "current_user", "content": "Prefers short bullet lists",
		"type": memory.CuratedTypeCommunicationPreference, "evidence_kind": memory.CuratedEvidenceExplicit,
		"preference_key": "communication.response_format", "preference_value": "short_bullets",
	})
	if !result.IsError || !strings.Contains(result.ForLLM, "explicit_memory_intent_required") {
		t.Fatalf("explicit_only natural statement was not rejected: %#v", result)
	}

	explicitSave := explicitOnlyNatural
	explicitSave.MessageRef = "13"
	explicitSave.ExplicitMemoryIntent = true
	saveCtx := WithToolCallerScope(context.Background(), explicitSave)
	result = tool.Execute(saveCtx, map[string]any{
		"action": "add", "target": "current_user", "content": "Use native quiz when available",
		"type": memory.CuratedTypeWorkflowPreference, "evidence_kind": memory.CuratedEvidenceExplicit,
		"preference_key": "presentation.quiz.mode", "preference_value": "native",
	})
	if result.IsError {
		t.Fatalf("explicit save request rejected: %v", result.Err)
	}

	entries, err := store.List(memory.CuratedTargetCurrentUser, baseCaller)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries after explicit save = %d, err=%v", len(entries), err)
	}
	var removeID string
	for _, entry := range entries {
		if entry.PreferenceKey == "presentation.quiz.mode" {
			removeID = entry.ID
		}
	}
	if removeID == "" {
		t.Fatal("explicitly saved entry missing")
	}

	explicitForget := explicitSave
	explicitForget.MessageRef = "14"
	forgetCtx := WithToolCallerScope(context.Background(), explicitForget)
	result = tool.Execute(forgetCtx, map[string]any{
		"action": "remove", "target": "current_user", "id": removeID,
	})
	if result.IsError {
		t.Fatalf("explicit forget request rejected: %v", result.Err)
	}

	background := explicitSave
	background.MessageRef = "15"
	backgroundCtx := WithToolCallerScope(context.Background(), background)
	backgroundCtx = WithBackgroundMemoryReview(backgroundCtx, true)
	result = tool.Execute(backgroundCtx, map[string]any{
		"action": "add", "target": "workspace", "content": "Reviewer attempted autonomous capture",
		"type": memory.CuratedTypeOther, "evidence_kind": memory.CuratedEvidenceObserved,
	})
	if !result.IsError || !strings.Contains(result.ForLLM, "explicit_memory_intent_required") {
		t.Fatalf("background reviewer bypassed explicit_only: %#v", result)
	}
}
