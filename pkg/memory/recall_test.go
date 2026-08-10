package memory

import (
	"strings"
	"testing"
	"time"
)

func newTestRecallStore(t *testing.T, root string, maxRecords int) *RecallStore {
	t.Helper()
	store, err := NewRecallStore(root, maxRecords)
	if err != nil {
		t.Fatalf("NewRecallStore() error = %v", err)
	}
	return store
}

func recallCaller(sessionRef, userKey, groupID, topicID, topicName string) CallerScope {
	return CallerScope{
		AgentID: "main", UserKey: userKey, Channel: "telegram", Account: "personal",
		ChatID: "group-1/" + topicID, GroupID: groupID, TopicID: topicID,
		TopicName: topicName, SessionKey: "session-key-" + topicID, SessionRef: sessionRef,
	}
}

func appendRecallTurn(t *testing.T, store *RecallStore, caller CallerScope, user, assistant string) uint64 {
	t.Helper()
	sequence, err := store.AppendDeliveredTurn(caller, "", user, assistant, "assistant-message")
	if err != nil {
		t.Fatalf("AppendDeliveredTurn() error = %v", err)
	}
	return sequence
}

func TestRecallStoreUserRecallAcrossTopicsAndPrivacyBoundaries(t *testing.T) {
	root := t.TempDir()
	store := newTestRecallStore(t, root, 100)
	current := recallCaller("session_current", "canonical-user-a", "group-1", "30", "Current")
	oauth := recallCaller("session_oauth", "canonical-user-a", "group-1", "10", "OAuth")
	otherUser := recallCaller("session_private", "canonical-user-b", "group-1", "20", "Private")
	otherAccount := recallCaller("session_other_account", "canonical-user-a", "group-1", "40", "Other account")
	otherAccount.Account = "work"
	appendRecallTurn(t, store, current, "current oauth question", "current answer")
	appendRecallTurn(t, store, oauth, "OAuth callback returned invalid state", "Rotate the state nonce")
	appendRecallTurn(t, store, otherUser, "OAuth private payroll error", "Private answer")
	appendRecallTurn(t, store, otherAccount, "OAuth work account error", "Work answer")

	results, err := store.Search(current, "OAuth invalid state", RecallSearchOptions{
		Mode: RecallModeUserRecall, MaxResults: 10, MaxChars: 4_000,
	})
	if err != nil {
		t.Fatalf("Search(user_recall) error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search(user_recall) returned no cross-topic result")
	}
	for _, result := range results {
		if result.SessionRef != oauth.SessionRef || result.TopicID != "10" || result.TopicName != "OAuth" {
			t.Fatalf("user recall escaped scope or lost provenance: %#v", result)
		}
		if result.Role == "" || result.Timestamp.IsZero() || result.Excerpt == "" {
			t.Fatalf("recall provenance incomplete: %#v", result)
		}
	}

	restarted := newTestRecallStore(t, root, 100)
	results, err = restarted.Search(current, "invalid state", RecallSearchOptions{
		Mode: RecallModeUserRecall, MaxResults: 10, MaxChars: 4_000,
	})
	if err != nil || len(results) == 0 {
		t.Fatalf("Search() after restart = %#v, %v", results, err)
	}
}

func TestRecallStoreModesEnforceServerSideScope(t *testing.T) {
	store := newTestRecallStore(t, t.TempDir(), 100)
	current := recallCaller("session_current", "canonical-user-a", "group-1", "30", "Current")
	sameGroupOtherUser := recallCaller("session_group", "canonical-user-b", "group-1", "20", "Group topic")
	otherGroup := recallCaller("session_other_group", "canonical-user-c", "group-2", "20", "Other group")
	otherChannel := recallCaller("session_discord", "canonical-user-a", "group-1", "50", "Discord")
	otherChannel.Channel = "discord"
	appendRecallTurn(t, store, sameGroupOtherUser, "shared kubernetes rollout policy", "use canary")
	appendRecallTurn(t, store, otherGroup, "shared kubernetes production secret", "do not expose")
	appendRecallTurn(t, store, otherChannel, "shared kubernetes discord note", "discord only")

	isolated, err := store.Search(current, "kubernetes", RecallSearchOptions{Mode: RecallModeIsolated})
	if err != nil || len(isolated) != 0 {
		t.Fatalf("isolated Search() = %#v, %v", isolated, err)
	}
	group, err := store.Search(current, "kubernetes", RecallSearchOptions{
		Mode: RecallModeGroupRecall, MaxResults: 20, MaxChars: 20_000,
	})
	if err != nil {
		t.Fatalf("group Search() error = %v", err)
	}
	if len(group) == 0 {
		t.Fatal("group recall returned no same-group result")
	}
	for _, result := range group {
		if result.SessionRef != sameGroupOtherUser.SessionRef {
			t.Fatalf("group recall escaped group/channel/account: %#v", result)
		}
	}

	user, err := store.Search(current, "kubernetes", RecallSearchOptions{
		Mode: RecallModeUserRecall, MaxResults: 20, MaxChars: 20_000,
	})
	if err != nil || len(user) != 0 {
		t.Fatalf("same-user Search() leaked another user = %#v, %v", user, err)
	}
}

func TestRecallStoreRankingAndBounds(t *testing.T) {
	store := newTestRecallStore(t, t.TempDir(), 100)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	current := recallCaller("session_current", "canonical-user-a", "group-1", "30", "Current")
	exact := recallCaller("session_exact", "canonical-user-a", "group-1", "10", "OAuth exact")
	partial := recallCaller("session_partial", "canonical-user-a", "group-1", "20", "OAuth partial")
	appendRecallTurn(t, store, partial, "oauth note", strings.Repeat("oauth details ", 80))
	appendRecallTurn(t, store, exact, "oauth invalid state callback invalid state", "exact fix")

	results, err := store.Search(current, "oauth invalid state", RecallSearchOptions{
		Mode: RecallModeUserRecall, MaxResults: 1, MaxChars: 40,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].SessionRef != exact.SessionRef {
		t.Fatalf("ranked results = %#v, want exact topic first", results)
	}
	if len([]rune(results[0].Excerpt)) > 40 {
		t.Fatalf("excerpt chars = %d, want <= 40", len([]rune(results[0].Excerpt)))
	}
}

func TestRecallStoreRecordsAfterKeepsWholeScopedTurnCursor(t *testing.T) {
	store := newTestRecallStore(t, t.TempDir(), 100)
	callerA := recallCaller("session_shared", "canonical-user-a", "group-1", "10", "OAuth")
	callerB := recallCaller("session_shared", "canonical-user-b", "group-1", "10", "OAuth")
	first := appendRecallTurn(
		t,
		store,
		callerA,
		strings.Repeat("first user ", 200),
		strings.Repeat("first assistant ", 200),
	)
	appendRecallTurn(t, store, callerB, "other user's turn", "private other response")
	second := appendRecallTurn(t, store, callerA, "second scoped turn", "second scoped answer")

	records, latest, err := store.RecordsAfter(callerA, 0, 512)
	if err != nil {
		t.Fatalf("RecordsAfter() error = %v", err)
	}
	if latest != first || len(records) != 2 {
		t.Fatalf("first bounded snapshot latest=%d records=%#v, want whole first turn", latest, records)
	}
	for _, record := range records {
		if record.UserDigest != digestUserKey(callerA.UserKey) {
			t.Fatalf("RecordsAfter() leaked another user: %#v", record)
		}
	}
	records, latest, err = store.RecordsAfter(callerA, latest, 8_000)
	if err != nil || latest != second || len(records) != 2 {
		t.Fatalf("second snapshot latest=%d records=%#v err=%v", latest, records, err)
	}
}

func TestRecallStoreRedactsSecretsAndHiddenControls(t *testing.T) {
	store := newTestRecallStore(t, t.TempDir(), 100)
	caller := recallCaller("session_sensitive", "canonical-user-a", "group-1", "10", "Sensitive")
	appendRecallTurn(
		t,
		store,
		caller,
		"api_key=super-secret-value safe\u200btext",
		"authorization: Bearer abcdefghijklmnop safe\ufeffanswer",
	)

	records, _, err := store.RecordsAfter(caller, 0, 4_000)
	if err != nil {
		t.Fatalf("RecordsAfter() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("redacted records = %#v, want two records", records)
	}
	for _, record := range records {
		if strings.Contains(record.Content, "super-secret-value") ||
			strings.Contains(record.Content, "abcdefghijklmnop") ||
			strings.ContainsAny(record.Content, "\u200b\ufeff") {
			t.Fatalf("sensitive recall content survived filtering: %q", record.Content)
		}
	}
}
