package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/memory"
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

func promptPartsContain(parts []PromptPart, value string) bool {
	for _, part := range parts {
		if strings.Contains(part.Content, value) {
			return true
		}
	}
	return false
}
