package memory

import "testing"

func TestReviewStatePersistsCountersAndScopesUsers(t *testing.T) {
	root := t.TempDir()
	store, err := NewReviewStateStore(root)
	if err != nil {
		t.Fatalf("NewReviewStateStore() error = %v", err)
	}
	callerA := recallCaller("shared-session", "canonical-user-a", "group-1", "10", "OAuth")
	callerB := recallCaller("shared-session", "canonical-user-b", "group-1", "10", "OAuth")
	for range 3 {
		if _, err := store.RecordSuccessfulTurn(callerA); err != nil {
			t.Fatalf("RecordSuccessfulTurn(A) error = %v", err)
		}
	}
	if _, err := store.RecordSuccessfulTurn(callerB); err != nil {
		t.Fatalf("RecordSuccessfulTurn(B) error = %v", err)
	}

	restarted, err := NewReviewStateStore(root)
	if err != nil {
		t.Fatalf("NewReviewStateStore(restart) error = %v", err)
	}
	cursorA, err := restarted.Get(callerA)
	if err != nil || cursorA.SuccessfulTurns != 3 || cursorA.ScopeDigest == "" {
		t.Fatalf("Get(A) = %#v, %v", cursorA, err)
	}
	cursorB, err := restarted.Get(callerB)
	if err != nil || cursorB.SuccessfulTurns != 1 || cursorB.ScopeDigest == cursorA.ScopeDigest {
		t.Fatalf("Get(B) = %#v, %v", cursorB, err)
	}
}

func TestReviewStateSuccessfulReviewPreservesTurnsArrivingDuringReview(t *testing.T) {
	store, err := NewReviewStateStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewReviewStateStore() error = %v", err)
	}
	caller := recallCaller("session", "canonical-user", "group-1", "10", "OAuth")
	for range 10 {
		if _, err := store.RecordSuccessfulTurn(caller); err != nil {
			t.Fatalf("RecordSuccessfulTurn() error = %v", err)
		}
	}
	// Two successful turns arrive after the reviewer took its ten-turn snapshot.
	for range 2 {
		if _, err := store.RecordSuccessfulTurn(caller); err != nil {
			t.Fatalf("RecordSuccessfulTurn(late) error = %v", err)
		}
	}
	if err := store.MarkSuccessfulReview(caller, 10, 10); err != nil {
		t.Fatalf("MarkSuccessfulReview() error = %v", err)
	}
	cursor, err := store.Get(caller)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if cursor.SuccessfulTurns != 2 || cursor.LastReviewedSequence != 10 || cursor.LastSuccessfulReviewAt.IsZero() {
		t.Fatalf("cursor after review = %#v", cursor)
	}
	if err := store.ForgetSession(caller.SessionRef); err != nil {
		t.Fatalf("ForgetSession() error = %v", err)
	}
	cursor, err = store.Get(caller)
	if err != nil || cursor.SuccessfulTurns != 0 || cursor.LastReviewedSequence != 0 {
		t.Fatalf("cursor after forget = %#v, %v", cursor, err)
	}
}
