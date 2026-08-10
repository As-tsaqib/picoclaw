package memory

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func newTestCheckpointStore(
	t *testing.T,
	root string,
	now func() time.Time,
) *CheckpointStore {
	t.Helper()
	store, err := NewCheckpointStore(root, CheckpointStoreOptions{
		MaxCount: 20, MaxContextChars: 1_000,
		CompletedRetention: 30 * 24 * time.Hour,
		Now:                now,
	})
	if err != nil {
		t.Fatalf("NewCheckpointStore() error = %v", err)
	}
	return store
}

func checkpointCaller(topic string) CallerScope {
	return CallerScope{
		AgentID: "main", UserKey: "canonical-user", Channel: "telegram", Account: "personal",
		ChatID: "group-1/" + topic, GroupID: "group-1", TopicID: topic,
		TopicName: "Topic " + topic, SessionKey: "session-key-" + topic,
		SessionRef: "session-ref-" + topic,
	}
}

func stringPointer(value string) *string { return &value }

func createCheckpointMutation(title, nextStep string) CheckpointMutation {
	return CheckpointMutation{
		Action: CuratedActionAdd,
		Kind:   stringPointer("lesson"), Title: stringPointer(title),
		Objective:   stringPointer("Complete " + title),
		CurrentStep: stringPointer("Introduction"), NextStep: stringPointer(nextStep),
		ImportantContext: stringPointer("Use short exercises"),
	}
}

func TestCheckpointProgressCommitsOnlyAfterDeliveredResponse(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	store := newTestCheckpointStore(t, root, func() time.Time { return now })
	caller := checkpointCaller("10")
	mutation := createCheckpointMutation("Go interfaces", "Explain method sets")
	mutation.Action = CheckpointActionCreate

	staged, err := store.Apply(caller, "turn-interrupted", mutation)
	if err != nil {
		t.Fatalf("Apply(staged create) error = %v", err)
	}
	if visible, err := store.ListForTurn(caller, "turn-interrupted", false); err != nil || len(visible) != 1 {
		t.Fatalf("ListForTurn(staged) = %#v, %v", visible, err)
	}
	if durable, err := store.List(caller, false); err != nil || len(durable) != 0 {
		t.Fatalf("staged checkpoint persisted before delivery: %#v, %v", durable, err)
	}
	store.DiscardTurn("turn-interrupted")
	if durable, err := store.List(caller, false); err != nil || len(durable) != 0 {
		t.Fatalf("discarded checkpoint persisted: %#v, %v", durable, err)
	}

	created, err := store.Apply(caller, "turn-delivered", mutation)
	if err != nil {
		t.Fatalf("Apply(delivered create) error = %v", err)
	}
	now = now.Add(time.Minute)
	if err := store.CommitDelivered("turn-delivered", caller.SessionKey, "Here is the interfaces lesson.", "msg-100"); err != nil {
		t.Fatalf("CommitDelivered() error = %v", err)
	}
	durable, err := store.Get(caller, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if durable.LastDelivered == nil || durable.LastDelivered.Excerpt != "Here is the interfaces lesson." ||
		durable.LastDelivered.MessageRef != "msg-100" {
		t.Fatalf("LastDelivered = %#v", durable.LastDelivered)
	}

	completed := []string{"Introduction"}
	update := CheckpointMutation{
		Action: CheckpointActionUpdate, ID: created.ID,
		CompletedItems: &completed,
		CurrentStep:    stringPointer("Method sets"),
		NextStep:       stringPointer("Practice pointer receivers"),
	}
	now = now.Add(time.Minute)
	if _, err := store.Apply(caller, "turn-update-interrupted", update); err != nil {
		t.Fatalf("Apply(staged update) error = %v", err)
	}
	store.DiscardTurn("turn-update-interrupted")
	unchanged, err := store.Get(caller, created.ID)
	if err != nil || unchanged.NextStep != "Explain method sets" || len(unchanged.CompletedItems) != 0 {
		t.Fatalf("interrupted update advanced checkpoint: %#v, %v", unchanged, err)
	}

	now = now.Add(time.Minute)
	if _, err := store.Apply(caller, "turn-update-delivered", update); err != nil {
		t.Fatalf("Apply(update) error = %v", err)
	}
	now = now.Add(time.Minute)
	if err := store.CommitDelivered("turn-update-delivered", caller.SessionKey, "Method sets are now complete.", "msg-101"); err != nil {
		t.Fatalf("CommitDelivered(update) error = %v", err)
	}
	advanced, err := store.Get(caller, created.ID)
	if err != nil || advanced.NextStep != "Practice pointer receivers" || len(advanced.CompletedItems) != 1 {
		t.Fatalf("delivered update did not advance checkpoint: %#v, %v", advanced, err)
	}

	restarted := newTestCheckpointStore(t, root, func() time.Time { return now })
	persisted, err := restarted.Get(caller, created.ID)
	if err != nil || persisted.NextStep != advanced.NextStep || persisted.LastDelivered == nil {
		t.Fatalf("checkpoint restart persistence = %#v, %v", persisted, err)
	}
	if staged.ID == created.ID {
		t.Fatal("discarded and recreated checkpoints unexpectedly reused a stable ID")
	}
}

func TestCheckpointContinuationResolutionAndTopicIsolation(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	store := newTestCheckpointStore(t, t.TempDir(), func() time.Time { return now })
	caller := checkpointCaller("10")
	otherTopic := checkpointCaller("20")
	firstMutation := createCheckpointMutation("OAuth lesson", "Explain authorization code flow")
	firstMutation.Action = CheckpointActionCreate
	first, err := store.Apply(caller, "", firstMutation)
	if err != nil {
		t.Fatalf("Apply(first) error = %v", err)
	}
	now = now.Add(time.Hour)
	secondMutation := createCheckpointMutation("Go lesson", "Explain interfaces")
	secondMutation.Action = CheckpointActionCreate
	second, err := store.Apply(caller, "", secondMutation)
	if err != nil {
		t.Fatalf("Apply(second) error = %v", err)
	}

	resolved, err := store.ResolveContinuation(caller, "lanjutkan yang tadi")
	if err != nil || resolved.ID != second.ID {
		t.Fatalf("generic continuation = %#v, %v, want latest", resolved, err)
	}
	resolved, err = store.ResolveContinuation(caller, "lanjutkan OAuth")
	if err != nil || resolved.ID != first.ID {
		t.Fatalf("relevant continuation = %#v, %v, want OAuth", resolved, err)
	}
	if _, err := store.Get(otherTopic, first.ID); !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("other topic Get() error = %v, want not found", err)
	}

	// An unrelated question does not mutate or replace the active checkpoint.
	before, err := store.Get(caller, second.ID)
	if err != nil {
		t.Fatalf("Get(before unrelated question) error = %v", err)
	}
	after, err := store.Get(caller, second.ID)
	if err != nil || after.NextStep != before.NextStep || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("unrelated question changed checkpoint: before=%#v after=%#v err=%v", before, after, err)
	}
}

func TestCheckpointAmbiguityCompletionAndRetention(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	root := t.TempDir()
	store := newTestCheckpointStore(t, root, func() time.Time { return now })
	caller := checkpointCaller("10")
	for _, title := range []string{"OAuth basics", "OAuth debugging"} {
		mutation := createCheckpointMutation(title, "Continue OAuth")
		mutation.Action = CheckpointActionCreate
		if _, err := store.Apply(caller, "", mutation); err != nil {
			t.Fatalf("Apply(%s) error = %v", title, err)
		}
	}
	if _, err := store.ResolveContinuation(caller, "lanjutkan OAuth"); err == nil {
		t.Fatal("ResolveContinuation() error = nil, want ambiguity")
	} else {
		var ambiguous *AmbiguousCheckpointError
		if !errors.As(err, &ambiguous) || len(ambiguous.Candidates) < 2 {
			t.Fatalf("ambiguity error = %#v", err)
		}
	}

	checkpoints, err := store.List(caller, false)
	if err != nil || len(checkpoints) != 2 {
		t.Fatalf("List() = %#v, %v", checkpoints, err)
	}
	completed, err := store.Apply(caller, "", CheckpointMutation{
		Action: CheckpointActionComplete, ID: checkpoints[0].ID,
	})
	if err != nil || completed.Status != CheckpointStatusCompleted {
		t.Fatalf("complete = %#v, %v", completed, err)
	}
	if _, err := store.Apply(caller, "", CheckpointMutation{
		Action: CheckpointActionResume, ID: completed.ID,
	}); !errors.Is(err, ErrCheckpointNotResumable) {
		t.Fatalf("resume completed error = %v", err)
	}
	active, err := store.List(caller, false)
	if err != nil || len(active) != 1 || active[0].ID == completed.ID {
		t.Fatalf("completed checkpoint resumed/listed accidentally: %#v, %v", active, err)
	}

	now = now.Add(31 * 24 * time.Hour)
	mutation := createCheckpointMutation("Fresh task", "Continue")
	mutation.Action = CheckpointActionCreate
	if _, err := store.Apply(caller, "", mutation); err != nil {
		t.Fatalf("Apply(trigger prune) error = %v", err)
	}
	if _, err := store.Get(caller, completed.ID); !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("expired completed checkpoint error = %v, want not found", err)
	}
}

func TestCheckpointConcurrentTopicsPreserveEachOther(t *testing.T) {
	store := newTestCheckpointStore(t, t.TempDir(), time.Now)
	callers := []CallerScope{checkpointCaller("10"), checkpointCaller("20")}
	var wg sync.WaitGroup
	errs := make(chan error, len(callers))
	for _, caller := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mutation := createCheckpointMutation("Topic task "+caller.TopicID, "Next topic step")
			mutation.Action = CheckpointActionCreate
			turnID := "turn-" + caller.TopicID
			if _, err := store.Apply(caller, turnID, mutation); err != nil {
				errs <- err
				return
			}
			errs <- store.CommitDelivered(turnID, caller.SessionKey, "Delivered topic response", "")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent checkpoint error = %v", err)
		}
	}
	for _, caller := range callers {
		checkpoints, err := store.List(caller, false)
		if err != nil || len(checkpoints) != 1 || checkpoints[0].Provenance.TopicID != caller.TopicID {
			t.Fatalf("topic %s checkpoints = %#v, %v", caller.TopicID, checkpoints, err)
		}
	}
}
