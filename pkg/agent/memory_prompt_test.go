package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/memory"
	"github.com/sipeed/picoclaw/pkg/providers"
)

func TestMemoryPromptUsesBoundedWorkspaceCurrentUserAndCurrentTopicCheckpoint(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.Checkpoints.Enabled = true
	root := t.TempDir()
	curated, err := memory.NewCuratedStore(filepath.Join(root, "curated"), memory.CuratedStoreOptions{
		WorkspaceCharLimit: 1_000,
		PerUserCharLimit:   1_000,
	})
	if err != nil {
		t.Fatalf("NewCuratedStore() error = %v", err)
	}
	checkpoints, err := memory.NewCheckpointStore(filepath.Join(root, "checkpoints"), memory.CheckpointStoreOptions{
		MaxCount: 10, MaxContextChars: 1_000,
	})
	if err != nil {
		t.Fatalf("NewCheckpointStore() error = %v", err)
	}
	caller := memory.CallerScope{
		AgentID: "main", UserKey: "user-a", Channel: "telegram", Account: "personal",
		SessionKey: "topic-a", SessionRef: "session-a", TopicID: "10", TopicName: "OAuth",
	}
	otherUser := caller
	otherUser.UserKey = "user-b"
	otherTopic := caller
	otherTopic.SessionKey = "topic-b"
	otherTopic.SessionRef = "session-b"
	otherTopic.TopicID = "20"
	if _, err := curated.ApplyBatch(memory.CuratedTargetWorkspace, caller, []memory.CuratedMutation{{
		Action: memory.CuratedActionAdd, Content: "Workspace requires GitHub Actions validation",
	}}, false); err != nil {
		t.Fatalf("ApplyBatch(workspace) error = %v", err)
	}
	if _, err := curated.ApplyBatch(memory.CuratedTargetCurrentUser, caller, []memory.CuratedMutation{{
		Action: memory.CuratedActionAdd, Content: "User prefers Indonesian responses",
	}}, false); err != nil {
		t.Fatalf("ApplyBatch(current user) error = %v", err)
	}
	if _, err := curated.ApplyBatch(memory.CuratedTargetCurrentUser, otherUser, []memory.CuratedMutation{{
		Action: memory.CuratedActionAdd, Content: "Other user private timezone is UTC",
	}}, false); err != nil {
		t.Fatalf("ApplyBatch(other user) error = %v", err)
	}
	kind, title, objective, nextStep := "lesson", "OAuth lesson", "Learn OAuth", "Explain PKCE"
	if _, err := checkpoints.Apply(caller, "", memory.CheckpointMutation{
		Action: memory.CheckpointActionCreate, Kind: &kind, Title: &title,
		Objective: &objective, NextStep: &nextStep,
	}); err != nil {
		t.Fatalf("Apply(checkpoint current topic) error = %v", err)
	}
	otherTitle := "Other topic lesson"
	if _, err := checkpoints.Apply(otherTopic, "", memory.CheckpointMutation{
		Action: memory.CheckpointActionCreate, Kind: &kind, Title: &otherTitle,
		Objective: &objective, NextStep: &nextStep,
	}); err != nil {
		t.Fatalf("Apply(checkpoint other topic) error = %v", err)
	}

	agent := &AgentInstance{ID: "main", CuratedMemory: curated, Checkpoints: checkpoints}
	ts := &turnState{agent: agent}
	parts, private := memoryPromptPartsForTurn(ts, cfg, caller)
	if !private {
		t.Fatal("memory prompt with user/checkpoint data was not marked private")
	}
	var content strings.Builder
	for _, part := range parts {
		content.WriteString(part.Content)
		content.WriteString("\n")
	}
	prompt := content.String()
	for _, expected := range []string{
		"Workspace requires GitHub Actions validation",
		"User prefers Indonesian responses",
		"OAuth lesson",
		"Explain PKCE",
		"<curated_memory",
		"<task_checkpoints>",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("memory prompt missing %q: %s", expected, prompt)
		}
	}
	for _, forbidden := range []string{"Other user private timezone", "Other topic lesson"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("memory prompt leaked %q: %s", forbidden, prompt)
		}
	}
}

func TestMemoryPromptExcludesCurrentUserMemoryFromSharedChat(t *testing.T) {
	cfg := config.DefaultConfig()
	store, err := memory.NewCuratedStore(t.TempDir(), memory.CuratedStoreOptions{
		WorkspaceCharLimit: 1_000,
		PerUserCharLimit:   1_000,
	})
	if err != nil {
		t.Fatalf("NewCuratedStore() error = %v", err)
	}
	caller := memory.CallerScope{
		AgentID: "main", UserKey: "telegram:user-a", Channel: "telegram",
		Account: "personal", ChatID: "group-1/10", GroupID: "group-1",
		TopicID: "10", SessionKey: "topic-a", SessionRef: "session-a",
	}
	if _, err := store.ApplyBatch(memory.CuratedTargetWorkspace, caller, []memory.CuratedMutation{{
		Action: memory.CuratedActionAdd, Content: "Workspace convention remains safe in groups",
	}}, false); err != nil {
		t.Fatalf("ApplyBatch(workspace) error = %v", err)
	}
	if _, err := store.ApplyBatch(memory.CuratedTargetCurrentUser, caller, []memory.CuratedMutation{{
		Action: memory.CuratedActionAdd, Content: "Private timezone is Asia/Makassar",
	}}, false); err != nil {
		t.Fatalf("ApplyBatch(current user) error = %v", err)
	}

	ts := &turnState{
		agent:       &AgentInstance{ID: "main", CuratedMemory: store},
		userMessage: "Which conventions apply?",
	}
	parts, _ := memoryPromptPartsForTurn(ts, cfg, caller)
	if !promptPartsContain(parts, "Workspace convention remains safe in groups") {
		t.Fatal("shared-chat prompt omitted workspace memory")
	}
	if promptPartsContain(parts, "Private timezone is Asia/Makassar") ||
		promptPartsContain(parts, "target=\"current_user\"") {
		t.Fatal("shared-chat prompt exposed current-user memory")
	}
	usage := ts.stagedCuratedUsage()
	for _, item := range usage {
		if item.Target == memory.CuratedTargetCurrentUser {
			t.Fatalf("shared-chat prompt staged private usage: %#v", usage)
		}
	}
}

func TestCuratedMemoryChangesAppearOnNextPromptAssembly(t *testing.T) {
	cfg := config.DefaultConfig()
	store, err := memory.NewCuratedStore(t.TempDir(), memory.CuratedStoreOptions{
		WorkspaceCharLimit: 1_000,
		PerUserCharLimit:   1_000,
	})
	if err != nil {
		t.Fatalf("NewCuratedStore() error = %v", err)
	}
	caller := memory.CallerScope{AgentID: "main", UserKey: "user-a", SessionKey: "topic-a"}
	ts := &turnState{agent: &AgentInstance{ID: "main", CuratedMemory: store}}
	parts, _ := memoryPromptPartsForTurn(ts, cfg, caller)
	if promptPartsContain(parts, "New durable convention") {
		t.Fatal("new convention appeared before it was written")
	}
	if _, err := store.ApplyBatch(memory.CuratedTargetWorkspace, caller, []memory.CuratedMutation{{
		Action: memory.CuratedActionAdd, Content: "New durable convention",
	}}, false); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}
	parts, _ = memoryPromptPartsForTurn(ts, cfg, caller)
	if !promptPartsContain(parts, "New durable convention") {
		t.Fatal("memory change was not visible on the next prompt assembly")
	}
}

func TestMemoryPromptRetrievalIncludesPinnedAndRelevantButExcludesIrrelevant(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Retrieval.RecentFallbackCount = 0
	cfg.Memory.Retrieval.MinimumScore = 0.1
	store, err := memory.NewCuratedStore(t.TempDir(), memory.CuratedStoreOptions{
		WorkspaceCharLimit: 5_000,
		PerUserCharLimit:   5_000,
	})
	if err != nil {
		t.Fatalf("NewCuratedStore() error = %v", err)
	}
	caller := memory.CallerScope{
		AgentID: "main", UserKey: "telegram:user-a", Channel: "telegram",
		Account: "personal", SessionKey: "topic-a", SessionRef: "session-a",
	}
	add := func(target, content, entryType string) memory.CuratedEntry {
		t.Helper()
		result, addErr := store.ApplyBatch(target, caller, []memory.CuratedMutation{{
			Action: memory.CuratedActionAdd, Content: content, Type: entryType,
		}}, false)
		if addErr != nil {
			t.Fatalf("ApplyBatch(%q) error = %v", content, addErr)
		}
		return result.Applied[0]
	}
	workspaceRelevant := add(
		memory.CuratedTargetWorkspace,
		"Go repositories use remote GitHub Actions validation",
		memory.CuratedTypeProjectFact,
	)
	userRelevant := add(
		memory.CuratedTargetCurrentUser,
		"Prefers concise Go explanations in Indonesian",
		memory.CuratedTypeCommunicationPreference,
	)
	pinned := add(
		memory.CuratedTargetCurrentUser,
		"Verified profile preference applies to technical answers",
		memory.CuratedTypeIdentity,
	)
	if _, err := store.ApplyBatch(memory.CuratedTargetCurrentUser, caller, []memory.CuratedMutation{{
		Action: memory.CuratedActionPin, ID: pinned.ID,
	}}, false); err != nil {
		t.Fatalf("pin error = %v", err)
	}
	irrelevant := add(
		memory.CuratedTargetCurrentUser,
		"Enjoys gardening books on weekends",
		memory.CuratedTypeOther,
	)

	ts := &turnState{
		agent:       &AgentInstance{ID: "main", CuratedMemory: store},
		userMessage: "Explain the Go CI workflow concisely",
	}
	parts, _ := memoryPromptPartsForTurn(ts, cfg, caller)
	var prompt strings.Builder
	for _, part := range parts {
		prompt.WriteString(part.Content)
		prompt.WriteByte('\n')
	}
	content := prompt.String()
	for _, expected := range []string{
		workspaceRelevant.Content,
		userRelevant.Content,
		pinned.Content,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("query-aware prompt missing %q: %s", expected, content)
		}
	}
	if strings.Contains(content, irrelevant.Content) {
		t.Fatalf("query-aware prompt injected irrelevant entry: %s", content)
	}
	usage := ts.stagedCuratedUsage()
	if len(usage) != 2 {
		t.Fatalf("staged usage = %#v, want separate workspace and user records", usage)
	}
}

func TestMemoryPromptRetrievalDisabledPreservesBoundedLegacySelection(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Retrieval.Enabled = false
	store, err := memory.NewCuratedStore(t.TempDir(), memory.CuratedStoreOptions{
		WorkspaceCharLimit: 5_000,
		PerUserCharLimit:   5_000,
	})
	if err != nil {
		t.Fatalf("NewCuratedStore() error = %v", err)
	}
	caller := memory.CallerScope{AgentID: "main", UserKey: "user-a", SessionKey: "topic-a"}
	active, err := store.ApplyBatch(memory.CuratedTargetCurrentUser, caller, []memory.CuratedMutation{{
		Action: memory.CuratedActionAdd, Content: "Legacy selection keeps unrelated active memory",
	}}, false)
	if err != nil {
		t.Fatalf("ApplyBatch(active) error = %v", err)
	}
	archived, err := store.ApplyBatch(memory.CuratedTargetCurrentUser, caller, []memory.CuratedMutation{{
		Action: memory.CuratedActionAdd, Content: "Archived memory must stay excluded",
	}}, false)
	if err != nil {
		t.Fatalf("ApplyBatch(archived) error = %v", err)
	}
	if _, err := store.ApplyBatch(memory.CuratedTargetCurrentUser, caller, []memory.CuratedMutation{{
		Action: memory.CuratedActionArchive, ID: archived.Applied[0].ID,
	}}, false); err != nil {
		t.Fatalf("archive error = %v", err)
	}
	ts := &turnState{
		agent:       &AgentInstance{ID: "main", CuratedMemory: store},
		userMessage: "completely unrelated query",
	}
	parts, _ := memoryPromptPartsForTurn(ts, cfg, caller)
	if !promptPartsContain(parts, active.Applied[0].Content) {
		t.Fatal("retrieval-disabled mode did not preserve active legacy selection")
	}
	if promptPartsContain(parts, archived.Applied[0].Content) {
		t.Fatal("retrieval-disabled mode injected archived memory")
	}
}

func TestCuratedPromptBudgetsRemainSeparateByTarget(t *testing.T) {
	retrieval := config.MemoryRetrievalConfig{MaxTotalChars: 101}
	if workspace := curatedPromptCharBudget(retrieval, memory.CuratedTargetWorkspace); workspace != 31 {
		t.Fatalf("workspace budget = %d, want 31", workspace)
	}
	if user := curatedPromptCharBudget(retrieval, memory.CuratedTargetCurrentUser); user != 70 {
		t.Fatalf("user budget = %d, want 70", user)
	}
}

func promptPartsContain(parts []PromptPart, value string) bool {
	for _, part := range parts {
		if strings.Contains(part.Content, value) {
			return true
		}
	}
	return false
}

func TestMemoryPromptIncludesCompiledProfileWithoutRetrievalMatch(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Retrieval.RecentFallbackCount = 0
	cfg.Memory.Retrieval.MinimumScore = 9 // force ordinary retrieval to reject it
	store, err := memory.NewCuratedStore(t.TempDir(), memory.CuratedStoreOptions{WorkspaceCharLimit: 5_000, PerUserCharLimit: 5_000})
	if err != nil {
		t.Fatal(err)
	}
	caller := memory.CallerScope{AgentID: "main", UserKey: "telegram:user-profile", Channel: "telegram", Account: "personal", SessionKey: "topic-a", SessionRef: "session-a"}
	if _, err := store.ApplyBatch(memory.CuratedTargetCurrentUser, caller, []memory.CuratedMutation{{
		Action: memory.CuratedActionAdd, Content: "User explicitly prefers Indonesian", Type: memory.CuratedTypeCommunicationPreference,
		EvidenceKind: memory.CuratedEvidenceExplicit, PreferenceKey: "communication.language", PreferenceValue: "id",
	}}, false); err != nil {
		t.Fatal(err)
	}
	ts := &turnState{agent: &AgentInstance{ID: "main", CuratedMemory: store}, userMessage: "What is a B-tree?"}
	parts, private := memoryPromptPartsForTurn(ts, cfg, caller)
	if !private || !promptPartsContain(parts, "<user_profile>") || !promptPartsContain(parts, `"communication.language"`) {
		t.Fatalf("compiled profile missing from private prompt: %#v", parts)
	}
	if len(ts.stagedCuratedUsage()) != 0 {
		t.Fatalf("always-on profile must not create presentation feedback: %#v", ts.stagedCuratedUsage())
	}
}

func TestCuratedRetrievalQueryUsesSummaryAndRecentUserTurns(t *testing.T) {
	ts := &turnState{
		userMessage:         "lanjut yang tadi",
		restorePointSummary: "We are configuring an OpenWrt WireGuard interface.",
		restorePointHistory: []providers.Message{
			{Role: "user", Content: "Set the firewall zone for WireGuard."},
			{Role: "assistant", Content: "Use a dedicated wg zone."},
			{Role: "user", Content: "Show the second approach."},
		},
	}
	query := curatedRetrievalQuery(ts)
	for _, expected := range []string{"lanjut yang tadi", "OpenWrt WireGuard", "second approach", "firewall zone"} {
		if !strings.Contains(query, expected) {
			t.Fatalf("retrieval query missing %q: %q", expected, query)
		}
	}
}
