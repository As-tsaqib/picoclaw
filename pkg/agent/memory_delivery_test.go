package agent

import (
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/memory"
)

func TestDeferredMemoryDeliveryUsesActuallyDeliveredContent(t *testing.T) {
	al, agent, caller := newMemoryReviewerHarness(t, &mockProvider{})
	checkpoints, storeErr := memory.NewCheckpointStore(
		filepath.Join(t.TempDir(), "checkpoints"),
		memory.CheckpointStoreOptions{MaxCount: 10, MaxContextChars: 1_000},
	)
	if storeErr != nil {
		t.Fatalf("NewCheckpointStore() error = %v", storeErr)
	}
	agent.Checkpoints = checkpoints
	kind, title, objective, nextStep := "lesson", "Go interfaces", "Learn interfaces", "Explain method sets"
	created, err := checkpoints.Apply(caller, "turn-1", memory.CheckpointMutation{
		Action: memory.CheckpointActionCreate, Kind: &kind, Title: &title,
		Objective: &objective, NextStep: &nextStep,
	})
	if err != nil {
		t.Fatalf("Apply(checkpoint) error = %v", err)
	}
	al.pendingMemoryDeliveries.Store(caller.SessionKey, deferredMemoryDelivery{
		agent: agent, caller: caller, turnID: "turn-1", userContent: "teach me",
		assistantContent: "planned but suppressed model response",
	})
	al.acknowledgeDeferredMemoryDelivery(caller.SessionKey, "text actually delivered by message tool", true)

	checkpoint, err := checkpoints.Get(caller, created.ID)
	if err != nil {
		t.Fatalf("Get(checkpoint) error = %v", err)
	}
	if checkpoint.LastDelivered == nil ||
		checkpoint.LastDelivered.Excerpt != "text actually delivered by message tool" {
		t.Fatalf("checkpoint LastDelivered = %#v", checkpoint.LastDelivered)
	}
	records, _, err := agent.RecallMemory.RecordsAfter(caller, 0, config.DefaultMemoryRecallChars)
	if err != nil {
		t.Fatalf("RecordsAfter() error = %v", err)
	}
	if len(records) != 2 || records[1].Content != "text actually delivered by message tool" {
		t.Fatalf("delivered recall records = %#v", records)
	}
}

func TestFailedDeliveryDiscardsCheckpointProgress(t *testing.T) {
	al, agent, caller := newMemoryReviewerHarness(t, &mockProvider{})
	checkpoints, storeErr := memory.NewCheckpointStore(
		filepath.Join(t.TempDir(), "checkpoints"),
		memory.CheckpointStoreOptions{MaxCount: 10, MaxContextChars: 1_000},
	)
	if storeErr != nil {
		t.Fatalf("NewCheckpointStore() error = %v", storeErr)
	}
	agent.Checkpoints = checkpoints
	kind, title, objective, nextStep := "debugging", "OAuth issue", "Fix callback", "Inspect state"
	created, err := checkpoints.Apply(caller, "", memory.CheckpointMutation{
		Action: memory.CheckpointActionCreate, Kind: &kind, Title: &title,
		Objective: &objective, NextStep: &nextStep,
	})
	if err != nil {
		t.Fatalf("Apply(create) error = %v", err)
	}
	nextStep = "Rotate nonce"
	if _, updateErr := checkpoints.Apply(caller, "turn-failed", memory.CheckpointMutation{
		Action: memory.CheckpointActionUpdate, ID: created.ID, NextStep: &nextStep,
	}); updateErr != nil {
		t.Fatalf("Apply(update) error = %v", updateErr)
	}
	al.commitMemoryDelivery(deferredMemoryDelivery{
		agent: agent, caller: caller, turnID: "turn-failed",
		assistantContent: "response that was interrupted",
	}, false)
	persisted, err := checkpoints.Get(caller, created.ID)
	if err != nil || persisted.NextStep != "Inspect state" || persisted.LastDelivered != nil {
		t.Fatalf("failed delivery advanced checkpoint: %#v, %v", persisted, err)
	}
}

func TestClearSessionMemoryStatePreservesCuratedMemoryAndDurableCheckpoint(t *testing.T) {
	_, agent, caller := newMemoryReviewerHarness(t, &mockProvider{})
	checkpoints, storeErr := memory.NewCheckpointStore(
		filepath.Join(t.TempDir(), "checkpoints"),
		memory.CheckpointStoreOptions{MaxCount: 10, MaxContextChars: 1_000},
	)
	if storeErr != nil {
		t.Fatalf("NewCheckpointStore() error = %v", storeErr)
	}
	agent.Checkpoints = checkpoints
	if _, applyErr := agent.CuratedMemory.ApplyBatch(
		memory.CuratedTargetCurrentUser,
		caller,
		[]memory.CuratedMutation{{
			Action: memory.CuratedActionAdd, Content: "Prefers concise responses",
		}},
		false,
	); applyErr != nil {
		t.Fatalf("ApplyBatch(curated) error = %v", applyErr)
	}
	kind, title, objective, nextStep := "lesson", "Go lesson", "Learn Go", "Explain interfaces"
	checkpoint, err := checkpoints.Apply(caller, "", memory.CheckpointMutation{
		Action: memory.CheckpointActionCreate, Kind: &kind, Title: &title,
		Objective: &objective, NextStep: &nextStep,
	})
	if err != nil {
		t.Fatalf("Apply(durable checkpoint) error = %v", err)
	}
	nextStep = "Undelivered next step"
	if _, updateErr := checkpoints.Apply(caller, "pending-turn", memory.CheckpointMutation{
		Action: memory.CheckpointActionUpdate, ID: checkpoint.ID, NextStep: &nextStep,
	}); updateErr != nil {
		t.Fatalf("Apply(pending checkpoint) error = %v", updateErr)
	}
	appendReviewerTurn(t, agent, caller, "turn-1")
	if _, recordErr := agent.MemoryReviewState.RecordSuccessfulTurn(caller); recordErr != nil {
		t.Fatalf("RecordSuccessfulTurn() error = %v", recordErr)
	}

	if clearErr := clearSessionMemoryState(agent, caller); clearErr != nil {
		t.Fatalf("clearSessionMemoryState() error = %v", clearErr)
	}
	entries, err := agent.CuratedMemory.List(memory.CuratedTargetCurrentUser, caller)
	if err != nil || len(entries) != 1 {
		t.Fatalf("curated memory was cleared: %#v, %v", entries, err)
	}
	persisted, err := checkpoints.Get(caller, checkpoint.ID)
	if err != nil || persisted.NextStep != "Explain interfaces" {
		t.Fatalf("durable checkpoint changed: %#v, %v", persisted, err)
	}
	if pending, listErr := checkpoints.ListForTurn(caller, "pending-turn", false); listErr != nil ||
		len(pending) != 1 || pending[0].NextStep != "Explain interfaces" {
		t.Fatalf("pending checkpoint mutation survived clear: %#v, %v", pending, listErr)
	}
	records, _, err := agent.RecallMemory.RecordsAfter(caller, 0, 4_000)
	if err != nil || len(records) != 0 {
		t.Fatalf("current session recall was not cleared: %#v, %v", records, err)
	}
	cursor, err := agent.MemoryReviewState.Get(caller)
	if err != nil || cursor.SuccessfulTurns != 0 || cursor.LastReviewedSequence != 0 {
		t.Fatalf("review cursor was not cleared: %#v, %v", cursor, err)
	}
}
