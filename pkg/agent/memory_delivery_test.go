package agent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
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

func TestCuratedLastPresentedAdvancesOnlyAfterSuccessfulDelivery(t *testing.T) {
	al, agent, caller := newMemoryReviewerHarness(t, &mockProvider{})
	result, err := agent.CuratedMemory.ApplyBatch(
		memory.CuratedTargetCurrentUser,
		caller,
		[]memory.CuratedMutation{{
			Action: memory.CuratedActionAdd, Content: "Prefers concise Go explanations",
			Type: memory.CuratedTypeCommunicationPreference,
		}},
		false,
	)
	if err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}
	id := result.Applied[0].ID
	delivery := deferredMemoryDelivery{
		agent: agent, caller: caller, turnID: "turn-memory-usage",
		userContent: "Explain Go", assistantContent: "Delivered Go explanation",
		curatedUsage: []memory.CuratedUsage{{Target: memory.CuratedTargetCurrentUser, IDs: []string{id}}},
	}
	al.commitMemoryDelivery(delivery, false)
	entry, err := agent.CuratedMemory.Inspect(memory.CuratedTargetCurrentUser, caller, id)
	if err != nil {
		t.Fatalf("Inspect after failed delivery: %v", err)
	}
	if entry.LastPresentedAt != nil {
		t.Fatalf("failed delivery advanced LastPresentedAt: %#v", entry.LastPresentedAt)
	}

	delivery.turnID = "turn-memory-usage-delivered"
	al.commitMemoryDelivery(delivery, true)
	entry, err = agent.CuratedMemory.Inspect(memory.CuratedTargetCurrentUser, caller, id)
	if err != nil {
		t.Fatalf("Inspect after successful delivery: %v", err)
	}
	if entry.LastPresentedAt == nil {
		t.Fatal("successful delivery did not advance LastPresentedAt")
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

func TestClearLifecycleFlushesShortSessionBeforeReset(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "current_user",
		"content":          "Prefers concise progress updates",
		"type":             memory.CuratedTypeCommunicationPreference,
		"evidence_kind":    memory.CuratedEvidenceExplicit,
		"preference_key":   "communication.verbosity",
		"preference_value": "concise",
	}}
	al, agent, _ := newMemoryReviewerHarness(t, provider)
	tracker := &trackingContextManager{}
	al.contextManager = tracker
	opts := &processOptions{Dispatch: DispatchRequest{
		SessionKey: "short-session",
		InboundContext: &bus.InboundContext{
			Channel: "telegram", Account: "personal", ChatID: "user-a", ChatType: "direct", SenderID: "user-a",
		},
	}}
	caller := callerScopeForTurn(agent.ID, al.cfg, *opts)
	appendReviewerTurn(t, agent, caller, "turn-short")
	if _, err := agent.MemoryReviewState.RecordSuccessfulTurn(caller); err != nil {
		t.Fatal(err)
	}
	agent.Sessions.AddMessage(opts.Dispatch.SessionKey, "user", "Mulai sekarang saya lebih suka update singkat")

	if err := al.clearHistoryWithMemoryFlush(context.Background(), agent, opts); err != nil {
		t.Fatalf("clearHistoryWithMemoryFlush() error = %v", err)
	}
	if tracker.clearCalls.Load() != 1 {
		t.Fatalf("context clear calls = %d, want 1", tracker.clearCalls.Load())
	}
	entries, err := agent.CuratedMemory.List(memory.CuratedTargetCurrentUser, caller)
	if err != nil || len(entries) != 1 || entries[0].PreferenceValue != "concise" {
		t.Fatalf("short-session preference was not flushed durably: %#v, %v", entries, err)
	}
	records, _, err := agent.RecallMemory.RecordsAfter(caller, 0, 4_000)
	if err != nil || len(records) != 0 {
		t.Fatalf("session recall survived successful clear: %#v, %v", records, err)
	}
}

func TestClearLifecycleFailsClosedWhenMemoryFlushFails(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{disallowed: "exec"}
	al, agent, _ := newMemoryReviewerHarness(t, provider)
	tracker := &trackingContextManager{}
	al.contextManager = tracker
	opts := &processOptions{Dispatch: DispatchRequest{
		SessionKey: "fail-closed-session",
		InboundContext: &bus.InboundContext{
			Channel: "telegram", Account: "personal", ChatID: "user-a", ChatType: "direct", SenderID: "user-a",
		},
	}}
	caller := callerScopeForTurn(agent.ID, al.cfg, *opts)
	appendReviewerTurn(t, agent, caller, "turn-unreviewed")
	if _, err := agent.MemoryReviewState.RecordSuccessfulTurn(caller); err != nil {
		t.Fatal(err)
	}
	agent.Sessions.AddMessage(opts.Dispatch.SessionKey, "user", "durable unreviewed preference")

	err := al.clearHistoryWithMemoryFlush(context.Background(), agent, opts)
	if err == nil {
		t.Fatal("clearHistoryWithMemoryFlush() error = nil, want flush failure")
	}
	if tracker.clearCalls.Load() != 0 {
		t.Fatalf("context was cleared despite flush failure: %d calls", tracker.clearCalls.Load())
	}
	records, _, recallErr := agent.RecallMemory.RecordsAfter(caller, 0, 4_000)
	if recallErr != nil || len(records) != 2 {
		t.Fatalf("unreviewed recall was lost after failed clear: %#v, %v", records, recallErr)
	}
	cursor, cursorErr := agent.MemoryReviewState.Get(caller)
	if cursorErr != nil || cursor.LastReviewedSequence != 0 || cursor.SuccessfulTurns != 1 {
		t.Fatalf("review cursor advanced after failed clear: %#v, %v", cursor, cursorErr)
	}
	if history := agent.Sessions.GetHistory(opts.Dispatch.SessionKey); len(history) != 1 {
		t.Fatalf("session history was cleared after failed flush: %#v", history)
	}
}
