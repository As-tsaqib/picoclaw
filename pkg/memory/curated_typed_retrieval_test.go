package memory

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCuratedStoreLegacyEntriesLoadWithTypedDefaultsWithoutRewrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "curated")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	document := map[string]any{
		"version":      1,
		"scope_digest": "workspace",
		"entries": []map[string]any{{
			"id": "mem_0000000000000001", "content": "Legacy project convention",
			"provenance": map[string]any{"source": "legacy", "recorded_at": now},
			"created_at": now, "updated_at": now,
		}},
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	path := filepath.Join(root, "workspace.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store := newTestCuratedStore(t, root, 1_000, 1_000)
	entries, err := store.List(CuratedTargetWorkspace, testCaller("telegram:user-a"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].EffectiveType() != CuratedTypeOther ||
		entries[0].EffectiveStatus() != CuratedStatusActive || entries[0].EffectiveConfidence() != 1 {
		t.Fatalf("legacy typed defaults = %#v", entries)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !reflect.DeepEqual(after, data) {
		t.Fatal("read-only legacy schema load rewrote the structured document")
	}
}

func TestCuratedStoreTypedLifecyclePinAndSupersede(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 20_000, 20_000)
	caller := testCaller("telegram:user-a")
	types := []string{
		CuratedTypeIdentity,
		CuratedTypeCommunicationPreference,
		CuratedTypeWorkflowPreference,
		CuratedTypeCorrection,
		CuratedTypeEnvironment,
		CuratedTypeProjectFact,
		CuratedTypeRelationship,
		CuratedTypeEpisodicFact,
		CuratedTypeOther,
	}
	created := make([]CuratedEntry, 0, len(types))
	for index, entryType := range types {
		confidence := 0.8
		result, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
			Action: CuratedActionAdd, Content: "Typed durable fact " + entryType,
			Type: entryType, Confidence: &confidence,
		}}, false)
		if err != nil {
			t.Fatalf("ApplyBatch(type %d %s): %v", index, entryType, err)
		}
		created = append(created, result.Applied[0])
	}
	if len(created) != len(types) {
		t.Fatalf("typed entries = %d, want %d", len(created), len(types))
	}

	first := created[0]
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionPin, ID: first.ID,
	}}, false); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if pinned, err := store.Inspect(CuratedTargetCurrentUser, caller, first.ID); err != nil || !pinned.Pinned {
		t.Fatalf("pinned entry = %#v, %v", pinned, err)
	}
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionArchive, ID: first.ID,
	}}, false); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionPin, ID: first.ID,
	}}, false); !errors.Is(err, ErrCuratedInvalidAction) {
		t.Fatalf("pin archived error = %v, want invalid action", err)
	}
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionRestore, ID: first.ID,
	}}, false); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored, err := store.Inspect(CuratedTargetCurrentUser, caller, first.ID); err != nil ||
		restored.EffectiveStatus() != CuratedStatusActive || restored.ArchivedAt != nil {
		t.Fatalf("restored entry = %#v, %v", restored, err)
	}

	old := created[1]
	result, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Correction replaces the old communication preference",
		Type: CuratedTypeCorrection, Supersedes: old.ID,
	}}, false)
	if err != nil {
		t.Fatalf("superseding add: %v", err)
	}
	if len(result.Applied) != 1 || result.Applied[0].Supersedes != old.ID {
		t.Fatalf("superseding result = %#v", result)
	}
	superseded, err := store.Inspect(CuratedTargetCurrentUser, caller, old.ID)
	if err != nil || superseded.EffectiveStatus() != CuratedStatusSuperseded || superseded.Pinned {
		t.Fatalf("superseded entry = %#v, %v", superseded, err)
	}
}

func TestCuratedWorkspaceRejectsPrivateTypesAndPrivateFactText(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 5_000, 5_000)
	caller := testCaller("telegram:user-a")
	for _, entryType := range []string{
		CuratedTypeIdentity,
		CuratedTypeCommunicationPreference,
		CuratedTypeWorkflowPreference,
		CuratedTypeRelationship,
		CuratedTypeEpisodicFact,
	} {
		_, err := store.ApplyBatch(CuratedTargetWorkspace, caller, []CuratedMutation{{
			Action: CuratedActionAdd, Content: "Private fact for target type " + entryType,
			Type: entryType,
		}}, false)
		if !errors.Is(err, ErrCuratedInvalidTarget) {
			t.Fatalf("workspace type %s error = %v, want invalid target", entryType, err)
		}
	}
	_, err := store.ApplyBatch(CuratedTargetWorkspace, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "The user's timezone is Asia/Makassar",
		Type: CuratedTypeProjectFact,
	}}, false)
	if !errors.Is(err, ErrCuratedInvalidTarget) {
		t.Fatalf("workspace private fact error = %v, want invalid target", err)
	}
}

func TestCuratedRetrievalRanksBilingualFuzzyAndRecentTypedFacts(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	clock := now.Add(-400 * 24 * time.Hour)
	store, err := NewCuratedStore(filepath.Join(t.TempDir(), "curated"), CuratedStoreOptions{
		WorkspaceCharLimit: 20_000,
		PerUserCharLimit:   20_000,
		Now:                func() time.Time { return clock },
	})
	if err != nil {
		t.Fatalf("NewCuratedStore: %v", err)
	}
	caller := testCaller("telegram:user-a")
	add := func(content, entryType string) CuratedEntry {
		t.Helper()
		result, addErr := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
			Action: CuratedActionAdd, Content: content, Type: entryType,
		}}, false)
		if addErr != nil {
			t.Fatalf("add %q: %v", content, addErr)
		}
		return result.Applied[0]
	}

	stale := add("Prefers Go answers with concise examples", CuratedTypeCommunicationPreference)
	clock = now.Add(-48 * time.Hour)
	recent := add("Prefers Go answers with concise explanations", CuratedTypeCommunicationPreference)
	indonesian := add("Lebih suka jawaban ringkas dalam Bahasa Indonesia", CuratedTypeCommunicationPreference)
	unrelated := add("The project binary is named picoclaw", CuratedTypeProjectFact)
	pinned := add("Use the user's verified profile preferences", CuratedTypeIdentity)
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionPin, ID: pinned.ID,
	}}, false); err != nil {
		t.Fatalf("pin: %v", err)
	}
	archived := add("Go answers must expose a private archived detail", CuratedTypeOther)
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionArchive, ID: archived.ID,
	}}, false); err != nil {
		t.Fatalf("archive: %v", err)
	}
	clock = now.Add(-48 * time.Hour)
	expires := now.Add(-24 * time.Hour)
	expiredResult, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Go answer temporary preference",
		Type: CuratedTypeCommunicationPreference, ExpiresAt: &expires,
	}}, false)
	if err != nil {
		t.Fatalf("add expiring entry: %v", err)
	}
	expired := expiredResult.Applied[0]
	clock = now

	opts := CuratedRetrievalOptions{
		Query: "prefers Go answers concise", MaxResults: 4, MaxChars: 400, PinnedChars: 100,
		MinimumScore: 0.35, RecencyWeight: 0.4, RecencyHalfLifeDays: 90,
		StaleAfterDays: 180, FuzzyWeight: 0.75, RecentFallbackCount: 0, Now: now,
	}
	first, err := store.Retrieve(CuratedTargetCurrentUser, caller, opts)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	second, err := store.Retrieve(CuratedTargetCurrentUser, caller, opts)
	if err != nil {
		t.Fatalf("Retrieve repeat: %v", err)
	}
	if len(first.Entries) < 3 || first.Entries[0].ID != pinned.ID || first.Entries[1].ID != recent.ID ||
		first.Entries[2].ID != stale.ID {
		t.Fatalf("ranked entries = %#v", first.Entries)
	}
	if !reflect.DeepEqual(curatedEntryIDsForTest(first.Entries), curatedEntryIDsForTest(second.Entries)) {
		t.Fatalf("fixed-time ranking was not deterministic: %#v vs %#v", first.Entries, second.Entries)
	}
	for _, forbidden := range []string{unrelated.ID, archived.ID, expired.ID} {
		if containsCuratedID(first.Entries, forbidden) {
			t.Fatalf("irrelevant/ineligible entry %s was retrieved: %#v", forbidden, first.Entries)
		}
	}
	if first.Characters > opts.MaxChars || len(first.Entries) > opts.MaxResults {
		t.Fatalf("retrieval bounds exceeded: %#v", first)
	}

	indo, err := store.Retrieve(CuratedTargetCurrentUser, caller, CuratedRetrievalOptions{
		Query: "jawaban Indonesia ringkas", MaxResults: 3, MaxChars: 240, PinnedChars: 80,
		MinimumScore: 0.35, FuzzyWeight: 0.75, Now: now,
	})
	if err != nil || !containsCuratedID(indo.Entries, indonesian.ID) {
		t.Fatalf("Indonesian retrieval = %#v, %v", indo, err)
	}
	fuzzy, err := store.Retrieve(CuratedTargetCurrentUser, caller, CuratedRetrievalOptions{
		Query: "prefer concis golan explanation", MaxResults: 3, MaxChars: 240, PinnedChars: 80,
		MinimumScore: 0.1, FuzzyWeight: 2, Now: now,
	})
	if err != nil || !containsCuratedID(fuzzy.Entries, recent.ID) {
		t.Fatalf("fuzzy English retrieval = %#v, %v", fuzzy, err)
	}
}

func TestCuratedRetrievalExplicitZeroFallbackDoesNotInjectRecentEntries(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 2_000, 2_000)
	caller := testCaller("telegram:user-a")
	addCurated(t, store, CuratedTargetCurrentUser, caller, "Recent but query-independent preference")
	result, err := store.Retrieve(CuratedTargetCurrentUser, caller, CuratedRetrievalOptions{
		MaxResults: 4, MaxChars: 400, PinnedChars: 100, RecentFallbackCount: 0,
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("explicit zero fallback injected entries: %#v", result.Entries)
	}
}

func curatedEntryIDsForTest(entries []CuratedEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return ids
}

func containsCuratedID(entries []CuratedEntry, id string) bool {
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func TestRedactMemoryTextNeverReturnsSecretPreview(t *testing.T) {
	secret := "authorization: Bearer " + strings.Repeat("a", 24)
	if got := RedactMemoryText(secret); strings.Contains(got, "Bearer") || strings.Contains(got, "aaaa") {
		t.Fatalf("secret preview was not redacted: %q", got)
	}
}
