package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
)

func toolMemoryCaller(sessionRef, userKey, groupID, topicID string) memory.CallerScope {
	return memory.CallerScope{
		AgentID: "main", UserKey: userKey, Channel: "telegram", Account: "personal",
		ChatID: "group-1/" + topicID, GroupID: groupID, TopicID: topicID,
		TopicName: "Topic " + topicID, SessionKey: "session-key-" + topicID,
		SessionRef: sessionRef,
	}
}

func toolContext(caller memory.CallerScope, turnID string) context.Context {
	ctx := WithToolCallerScope(context.Background(), caller)
	return WithToolTurnID(ctx, turnID)
}

func decodeToolResult(t *testing.T, result *ToolResult) map[string]any {
	t.Helper()
	if result == nil {
		t.Fatal("tool result is nil")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.ContentForLLM()), &payload); err != nil {
		t.Fatalf("tool result JSON error = %v, payload=%q", err, result.ContentForLLM())
	}
	return payload
}

func TestMemoryManageToolCRUDScopeAndApproval(t *testing.T) {
	store, err := memory.NewCuratedStore(filepath.Join(t.TempDir(), "curated"), memory.CuratedStoreOptions{
		WorkspaceCharLimit: 1_000,
		PerUserCharLimit:   1_000,
	})
	if err != nil {
		t.Fatalf("NewCuratedStore() error = %v", err)
	}
	callerA := toolMemoryCaller("session-a", "user-a", "group-1", "10")
	callerB := toolMemoryCaller("session-b", "user-b", "group-1", "20")
	callerA.GroupID, callerA.ChatID, callerA.TopicID = "", "user-a", ""
	callerB.GroupID, callerB.ChatID, callerB.TopicID = "", "user-b", ""
	tool := NewMemoryManageTool(store, false, nil)
	added := decodeToolResult(t, tool.Execute(toolContext(callerA, "turn-a"), map[string]any{
		"action": "add", "target": "current_user", "content": "Prefers concise responses",
	}))
	if added["ok"] != true || added["outcome"] != "added" {
		t.Fatalf("add payload = %#v", added)
	}
	listedB := decodeToolResult(t, tool.Execute(toolContext(callerB, "turn-b"), map[string]any{
		"action": "list", "target": "current_user", "user_id": "user-a",
	}))
	entriesB, ok := listedB["entries"].([]any)
	if !ok || len(entriesB) != 0 {
		t.Fatalf("model-supplied user ID bypassed scope: %#v", listedB)
	}

	approvalTool := NewMemoryManageTool(store, true, nil)
	backgroundCtx := WithBackgroundMemoryReview(toolContext(callerA, ""), true)
	staged := decodeToolResult(t, approvalTool.Execute(backgroundCtx, map[string]any{
		"action": "add", "target": "workspace", "content": "Use GitHub Actions for validation",
	}))
	if staged["ok"] != true || staged["outcome"] != "pending" ||
		!strings.Contains(string(stagedJSON(t, staged)), "pending") {
		t.Fatalf("background approval payload = %#v", staged)
	}
	workspaceEntries, err := store.List(memory.CuratedTargetWorkspace, callerA)
	if err != nil || len(workspaceEntries) != 0 {
		t.Fatalf("approval write was applied immediately: %#v, %v", workspaceEntries, err)
	}
}

func TestMemoryManageToolReportsNoOpAndRejectedOutcomes(t *testing.T) {
	store, err := memory.NewCuratedStore(filepath.Join(t.TempDir(), "curated"), memory.CuratedStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	caller := toolMemoryCaller("session-a", "user-a", "", "")
	caller.ChatID = "user-a"
	tool := NewMemoryManageTool(store, false, nil)
	args := map[string]any{
		"action": "add", "target": "current_user", "content": "Prefers concise responses",
		"type": memory.CuratedTypeCommunicationPreference, "evidence_kind": memory.CuratedEvidenceExplicit,
		"preference_key": "communication.verbosity", "preference_value": "concise",
	}
	first := decodeToolResult(t, tool.Execute(toolContext(caller, "turn-a"), args))
	if first["outcome"] != "added" {
		t.Fatalf("first outcome = %#v", first)
	}
	second := decodeToolResult(t, tool.Execute(toolContext(caller, "turn-b"), args))
	if second["outcome"] != "no_op" {
		t.Fatalf("same preference outcome = %#v", second)
	}
	rejected := decodeToolResult(t, tool.Execute(toolContext(caller, "turn-c"), map[string]any{
		"action": "add", "target": "current_user", "content": "temporary token sk-secret-1234567890",
	}))
	if rejected["ok"] != false || rejected["outcome"] != "rejected" {
		t.Fatalf("rejected outcome = %#v", rejected)
	}
}

func TestMemoryManageToolExplicitCorrectionAndForgetRebuildProfile(t *testing.T) {
	store, err := memory.NewCuratedStore(filepath.Join(t.TempDir(), "curated"), memory.CuratedStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	caller := toolMemoryCaller("session-a", "user-a", "", "")
	caller.ChatID = "user-a"
	tool := NewMemoryManageToolWithApprovalMode(store, config.MemoryApprovalOff, nil)
	first := decodeToolResult(t, tool.Execute(toolContext(caller, "turn-a"), map[string]any{
		"action": "add", "target": "current_user",
		"content":          "From now on, answer concisely",
		"type":             memory.CuratedTypeCommunicationPreference,
		"evidence_kind":    memory.CuratedEvidenceExplicit,
		"preference_key":   "communication.verbosity",
		"preference_value": "concise",
	}))
	firstResult := first["result"].(map[string]any)
	firstApplied := firstResult["applied"].([]any)
	firstID := firstApplied[0].(map[string]any)["id"].(string)

	corrected := tool.Execute(toolContext(caller, "turn-b"), map[string]any{
		"action": "add", "target": "current_user",
		"content":          "Actually, I now prefer detailed answers",
		"type":             memory.CuratedTypeCommunicationPreference,
		"evidence_kind":    memory.CuratedEvidenceExplicit,
		"preference_key":   "communication.verbosity",
		"preference_value": "detailed",
		"supersedes":       firstID,
	})
	if corrected.IsError {
		t.Fatalf("explicit correction failed: %s", corrected.ContentForLLM())
	}
	profile, err := store.CompileUserProfile(caller, memory.UserProfileOptions{MaxChars: 800})
	if err != nil || len(profile.Communication) != 1 || profile.Communication[0].Value != "detailed" {
		t.Fatalf("corrected profile=%#v err=%v", profile, err)
	}
	currentID := profile.Communication[0].SourceID
	forgotten := tool.Execute(toolContext(caller, "turn-c"), map[string]any{
		"action": "remove", "target": "current_user", "id": currentID,
	})
	if forgotten.IsError {
		t.Fatalf("explicit forget failed: %s", forgotten.ContentForLLM())
	}
	profile, err = store.CompileUserProfile(caller, memory.UserProfileOptions{MaxChars: 800})
	if err != nil || len(profile.SourceIDs) != 0 {
		t.Fatalf("forgotten preference remained in profile=%#v err=%v", profile, err)
	}
}

func TestMemoryManageToolApprovalModesDistinguishInteractiveAndCuratorWrites(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		background bool
		wantStaged bool
	}{
		{name: "off interactive", mode: config.MemoryApprovalOff},
		{name: "off curator", mode: config.MemoryApprovalOff, background: true},
		{name: "background only interactive", mode: config.MemoryApprovalBackgroundOnly},
		{
			name: "background only curator", mode: config.MemoryApprovalBackgroundOnly,
			background: true, wantStaged: true,
		},
		{name: "all writes interactive", mode: config.MemoryApprovalAllWrites, wantStaged: true},
		{
			name: "all writes curator", mode: config.MemoryApprovalAllWrites,
			background: true, wantStaged: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := memory.NewCuratedStore(filepath.Join(t.TempDir(), "curated"), memory.CuratedStoreOptions{
				WorkspaceCharLimit: 1_000,
				PerUserCharLimit:   1_000,
			})
			if err != nil {
				t.Fatalf("NewCuratedStore() error = %v", err)
			}
			caller := toolMemoryCaller("session-a", "user-a", "", "")
			caller.ChatID = "user-a"
			tool := NewMemoryManageToolWithApprovalMode(store, test.mode, nil)
			ctx := toolContext(caller, "turn-a")
			if test.background {
				ctx = WithBackgroundMemoryReview(ctx, true)
			}
			result := tool.Execute(ctx, map[string]any{
				"action": "add", "target": "current_user",
				"content": "Prefers concise Indonesian updates",
				"type":    "communication_preference",
			})
			if result.IsError {
				t.Fatalf("memory write failed: %s", result.ContentForLLM())
			}
			entries, listErr := store.List(memory.CuratedTargetCurrentUser, caller)
			if listErr != nil {
				t.Fatalf("List() error = %v", listErr)
			}
			pending, pendingErr := store.Pending(memory.CuratedTargetCurrentUser, caller)
			if pendingErr != nil {
				t.Fatalf("Pending() error = %v", pendingErr)
			}
			if test.wantStaged {
				if len(entries) != 0 || len(pending) != 1 {
					t.Fatalf("staged write entries=%d pending=%d", len(entries), len(pending))
				}
			} else if len(entries) != 1 || len(pending) != 0 {
				t.Fatalf("immediate write entries=%d pending=%d", len(entries), len(pending))
			}
		})
	}
}

func TestMemoryManageToolSharedGroupUsesCanonicalOwnerAndFiltersPrivateEntries(t *testing.T) {
	store, err := memory.NewCuratedStore(filepath.Join(t.TempDir(), "curated"), memory.CuratedStoreOptions{
		WorkspaceCharLimit: 1_000,
		PerUserCharLimit:   1_000,
	})
	if err != nil {
		t.Fatalf("NewCuratedStore() error = %v", err)
	}
	caller := toolMemoryCaller("session-a", "user-a", "group-1", "10")
	tool := NewMemoryManageToolWithApprovalMode(store, config.MemoryApprovalOff, nil)
	behavior := tool.Execute(toolContext(caller, "turn-a"), map[string]any{
		"action": "add", "target": "current_user",
		"content":          "Prefers concise replies",
		"type":             memory.CuratedTypeCommunicationPreference,
		"evidence_kind":    memory.CuratedEvidenceExplicit,
		"preference_key":   "communication.verbosity",
		"preference_value": "concise",
		"visibility":       memory.CuratedVisibilityBehavioral,
	})
	if behavior.IsError {
		t.Fatalf("shared-group behavioral mutation failed: %s", behavior.ContentForLLM())
	}
	private := tool.Execute(toolContext(caller, "turn-a"), map[string]any{
		"action": "add", "target": "current_user",
		"content":       "Private timezone is Asia/Makassar",
		"type":          memory.CuratedTypeIdentity,
		"evidence_kind": memory.CuratedEvidenceExplicit,
		"visibility":    memory.CuratedVisibilityPrivate,
	})
	if private.IsError {
		t.Fatalf("shared-group private capture failed: %s", private.ContentForLLM())
	}
	if strings.Contains(private.ContentForLLM(), "Asia/Makassar") {
		t.Fatalf("shared-group private write leaked content through tool result: %s", private.ContentForLLM())
	}
	listed := tool.Execute(toolContext(caller, "turn-b"), map[string]any{
		"action": "list", "target": "current_user",
	})
	if listed.IsError {
		t.Fatalf("shared-group list failed: %s", listed.ContentForLLM())
	}
	payload := listed.ContentForLLM()
	if !strings.Contains(payload, "Prefers concise replies") || strings.Contains(payload, "Asia/Makassar") {
		t.Fatalf("shared-group list visibility failure: %s", payload)
	}

	direct := caller
	direct.GroupID, direct.TopicID, direct.ChatID = "", "", "user-a"
	directList := tool.Execute(toolContext(direct, "turn-c"), map[string]any{
		"action": "list", "target": "current_user",
	})
	if directList.IsError || !strings.Contains(directList.ContentForLLM(), "Asia/Makassar") {
		t.Fatalf("direct canonical owner could not see private memory: %s", directList.ContentForLLM())
	}

	approvalTool := NewMemoryManageToolWithApprovalMode(store, config.MemoryApprovalAllWrites, nil)
	pending := approvalTool.Execute(toolContext(caller, "turn-d"), map[string]any{
		"action": "add", "target": "current_user",
		"content":       "Private contact note 081234567890",
		"type":          memory.CuratedTypeIdentity,
		"evidence_kind": memory.CuratedEvidenceExplicit,
		"visibility":    memory.CuratedVisibilityPrivate,
	})
	if pending.IsError {
		t.Fatalf("shared-group private pending write failed: %s", pending.ContentForLLM())
	}
	if strings.Contains(pending.ContentForLLM(), "081234567890") {
		t.Fatalf("shared-group pending private write leaked content: %s", pending.ContentForLLM())
	}
}

func TestMemoryManageToolRejectsCurrentUserAccessWithoutTrustedIdentity(t *testing.T) {
	store, err := memory.NewCuratedStore(filepath.Join(t.TempDir(), "curated"), memory.CuratedStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	caller := toolMemoryCaller("session-a", "", "", "")
	caller.ChatID = "unknown"
	result := NewMemoryManageTool(store, false, nil).Execute(
		toolContext(caller, "turn-a"),
		map[string]any{
			"action": "list", "target": "current_user",
		},
	)
	if !result.IsError {
		t.Fatalf("unknown identity listed current-user memory: %s", result.ContentForLLM())
	}
	payload := decodeToolResult(t, result)
	errorPayload, _ := payload["error"].(map[string]any)
	if errorPayload["code"] != "user_scope_unavailable" {
		t.Fatalf("unknown identity error payload = %#v", payload)
	}
}

func stagedJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return data
}

func TestMemoryManageToolAtomicBatchAndStructuredCapacityError(t *testing.T) {
	store, err := memory.NewCuratedStore(filepath.Join(t.TempDir(), "curated"), memory.CuratedStoreOptions{
		WorkspaceCharLimit: 12,
		PerUserCharLimit:   12,
	})
	if err != nil {
		t.Fatalf("NewCuratedStore() error = %v", err)
	}
	caller := toolMemoryCaller("session-a", "user-a", "group-1", "10")
	tool := NewMemoryManageTool(store, false, nil)
	result := tool.Execute(toolContext(caller, "turn"), map[string]any{
		"action": "batch", "target": "workspace",
		"operations": []map[string]any{
			{"action": "add", "content": "small"},
			{"action": "add", "content": "this is too large"},
		},
	})
	if !result.IsError {
		t.Fatalf("capacity result IsError = false: %s", result.ContentForLLM())
	}
	payload := decodeToolResult(t, result)
	errorPayload, _ := payload["error"].(map[string]any)
	if errorPayload["code"] != "memory_full" {
		t.Fatalf("capacity payload = %#v", payload)
	}
	entries, err := store.List(memory.CuratedTargetWorkspace, caller)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed batch was not atomic: %#v, %v", entries, err)
	}
}

func TestSessionRecallToolRejectsArbitraryScopeAndReviewerUse(t *testing.T) {
	store, err := memory.NewRecallStore(t.TempDir(), 100)
	if err != nil {
		t.Fatalf("NewRecallStore() error = %v", err)
	}
	caller := toolMemoryCaller("session-current", "user-a", "group-1", "30")
	sameUser := toolMemoryCaller("session-same-user", "user-a", "group-1", "10")
	otherUser := toolMemoryCaller("session-other-user", "user-b", "group-1", "20")
	if _, appendErr := store.AppendDeliveredTurn(
		sameUser,
		"turn-1",
		"OAuth invalid state",
		"same user fix",
		"",
	); appendErr != nil {
		t.Fatalf("AppendDeliveredTurn(same user) error = %v", appendErr)
	}
	if _, appendErr := store.AppendDeliveredTurn(
		otherUser,
		"turn-2",
		"OAuth private salary state",
		"other user",
		"",
	); appendErr != nil {
		t.Fatalf("AppendDeliveredTurn(other user) error = %v", appendErr)
	}
	tool := NewSessionRecallTool(store, memory.RecallModeUserRecall, 10, 4_000)
	payload := decodeToolResult(t, tool.Execute(toolContext(caller, "turn-current"), map[string]any{
		"query": "OAuth state", "session_key": otherUser.SessionKey, "user_id": otherUser.UserKey,
	}))
	results, ok := payload["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("recall results = %#v", payload)
	}
	for _, raw := range results {
		result := raw.(map[string]any)
		if result["session_ref"] != sameUser.SessionRef {
			t.Fatalf("arbitrary scope bypassed backend: %#v", result)
		}
	}
	reviewerCtx := WithBackgroundMemoryReview(toolContext(caller, ""), true)
	if result := tool.Execute(reviewerCtx, map[string]any{"query": "OAuth"}); !result.IsError {
		t.Fatalf("background reviewer invoked session_recall: %s", result.ContentForLLM())
	}
}

func TestTaskCheckpointToolStagesAndRestrictsReviewer(t *testing.T) {
	store, err := memory.NewCheckpointStore(t.TempDir(), memory.CheckpointStoreOptions{
		MaxCount: 10, MaxContextChars: 1_000,
	})
	if err != nil {
		t.Fatalf("NewCheckpointStore() error = %v", err)
	}
	caller := toolMemoryCaller("session-current", "user-a", "group-1", "30")
	tool := NewTaskCheckpointTool(store)
	payload := decodeToolResult(t, tool.Execute(toolContext(caller, "turn-1"), map[string]any{
		"action": "create", "kind": "lesson", "title": "Go interfaces",
		"objective": "Learn Go interfaces", "next_step": "Explain method sets",
	}))
	if payload["staged_until_delivery"] != true {
		t.Fatalf("checkpoint payload = %#v", payload)
	}
	checkpoints, err := store.List(caller, false)
	if err != nil || len(checkpoints) != 0 {
		t.Fatalf("checkpoint persisted before delivery: %#v, %v", checkpoints, err)
	}
	reviewerCtx := WithBackgroundMemoryReview(toolContext(caller, ""), true)
	if result := tool.Execute(reviewerCtx, map[string]any{"action": "list"}); !result.IsError {
		t.Fatalf("background reviewer invoked task_checkpoint: %s", result.ContentForLLM())
	}
}
