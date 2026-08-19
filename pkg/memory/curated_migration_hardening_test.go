package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateLegacyUserStoreToPersonScopeBeyondBatchLimitAndIdempotent(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 200_000, 200_000)
	legacyKey := "channel:telegram|account:default|user:42"
	legacyCaller := CallerScope{AgentID: "migration", UserKey: legacyKey}
	for i := 0; i < MaxCuratedBatchMutations+7; i++ {
		_, err := store.ApplyBatch(CuratedTargetCurrentUser, legacyCaller, []CuratedMutation{{
			Action:  CuratedActionAdd,
			Content: "unique migrated fact number " + time.Unix(int64(i+1), 0).UTC().Format(time.RFC3339Nano),
			Type:    CuratedTypeOther,
		}}, false)
		if err != nil {
			t.Fatalf("seed legacy entry %d: %v", i, err)
		}
	}

	migrated, err := store.MigrateLegacyUserStoreToPersonScope([]string{legacyKey}, "person:owner")
	if err != nil {
		t.Fatalf("MigrateLegacyUserStoreToPersonScope() error = %v", err)
	}
	if want := MaxCuratedBatchMutations + 7; migrated != want {
		t.Fatalf("migrated = %d, want %d", migrated, want)
	}
	entries, err := store.List(CuratedTargetCurrentUser, CallerScope{AgentID: "migration", UserKey: "person:owner"})
	if err != nil || len(entries) != migrated {
		t.Fatalf("person entries = %d, err=%v, want %d", len(entries), err, migrated)
	}
	again, err := store.MigrateLegacyUserStoreToPersonScope([]string{legacyKey}, "person:owner")
	if err != nil || again != 0 {
		t.Fatalf("second migration = %d, err=%v, want idempotent zero", again, err)
	}
}

func TestMigrateLegacyUserStorePreservesHistoricalMetadata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "curated")
	historical := time.Date(2025, 7, 4, 5, 6, 7, 0, time.UTC)
	seed, seedErr := NewCuratedStore(root, CuratedStoreOptions{
		WorkspaceCharLimit: 100_000,
		PerUserCharLimit:   100_000,
		Now:                func() time.Time { return historical },
	})
	if seedErr != nil {
		t.Fatal(seedErr)
	}
	legacyKey := "channel:telegram|account:default|user:owner"
	legacyCaller := CallerScope{AgentID: "migration", UserKey: legacyKey}
	confidence := 1.0
	seedResult, seedApplyErr := seed.ApplyBatch(CuratedTargetCurrentUser, legacyCaller, []CuratedMutation{{
		Action:          CuratedActionAdd,
		Content:         "Use Indonesian",
		Type:            CuratedTypeCommunicationPreference,
		Confidence:      &confidence,
		EvidenceKind:    CuratedEvidenceExplicit,
		EvidenceCount:   7,
		PreferenceKey:   "communication.language",
		PreferenceValue: "id",
		Provenance:      Provenance{Source: "user_request", Channel: "telegram", MessageRef: "old-message"},
	}}, false)
	if seedApplyErr != nil || len(seedResult.Applied) != 1 {
		t.Fatalf("seed result=%#v err=%v", seedResult, seedApplyErr)
	}
	if _, pinErr := seed.ApplyBatch(CuratedTargetCurrentUser, legacyCaller, []CuratedMutation{{
		Action: CuratedActionPin, ID: seedResult.Applied[0].ID,
	}}, false); pinErr != nil {
		t.Fatal(pinErr)
	}
	legacyEntries, legacyListErr := seed.List(CuratedTargetCurrentUser, legacyCaller)
	if legacyListErr != nil || len(legacyEntries) != 1 {
		t.Fatalf("legacy entries=%#v err=%v", legacyEntries, legacyListErr)
	}
	legacyEntry := legacyEntries[0]

	store := newTestCuratedStore(t, root, 100_000, 100_000)
	if _, migrateErr := store.MigrateLegacyUserStoreToPersonScope(
		[]string{legacyKey},
		"person:owner",
	); migrateErr != nil {
		t.Fatal(migrateErr)
	}
	entries, personListErr := store.List(
		CuratedTargetCurrentUser,
		CallerScope{AgentID: "migration", UserKey: "person:owner"},
	)
	if personListErr != nil || len(entries) != 1 {
		t.Fatalf("entries=%#v err=%v", entries, personListErr)
	}
	entry := entries[0]
	if entry.EvidenceKind != CuratedEvidenceExplicit || entry.EvidenceCount != 7 || !entry.Pinned {
		t.Fatalf("evidence/pin metadata not preserved: %#v", entry)
	}
	if entry.Provenance != legacyEntry.Provenance || !entry.CreatedAt.Equal(legacyEntry.CreatedAt) ||
		!entry.UpdatedAt.Equal(legacyEntry.UpdatedAt) || entry.LastConfirmedAt == nil ||
		legacyEntry.LastConfirmedAt == nil || !entry.LastConfirmedAt.Equal(*legacyEntry.LastConfirmedAt) ||
		entry.EvidenceCount != legacyEntry.EvidenceCount || entry.ObservationCount != legacyEntry.ObservationCount ||
		entry.Pinned != legacyEntry.Pinned {
		t.Fatalf("historical metadata not preserved: legacy=%#v migrated=%#v", legacyEntry, entry)
	}
}

func TestMigrateLegacyUserStoreDoesNotOverwriteEqualOrStrongerPreference(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 100_000, 100_000)
	person := CallerScope{AgentID: "migration", UserKey: "person:owner"}
	legacyKey := "channel:telegram|account:default|user:42"
	legacy := CallerScope{AgentID: "migration", UserKey: legacyKey}
	explicitConfidence := 1.0
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, person, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefer text quizzes",
		Type: CuratedTypeCommunicationPreference, Confidence: &explicitConfidence,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "presentation.quiz.mode", PreferenceValue: "text",
	}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, legacy, []CuratedMutation{
		{
			Action:          CuratedActionAdd,
			Content:         "Prefer native quizzes",
			Type:            CuratedTypeCommunicationPreference,
			EvidenceKind:    CuratedEvidenceInferred,
			PreferenceKey:   "workflow.quiz_format",
			PreferenceValue: "telegram_native_quiz",
		},
	}, false); err != nil {
		t.Fatal(err)
	}
	migrated, err := store.MigrateLegacyUserStoreToPersonScope([]string{legacyKey}, "person:owner")
	if err != nil || migrated != 0 {
		t.Fatalf("migrated=%d err=%v, want weaker conflict preserved only in source", migrated, err)
	}
	entries, err := store.List(CuratedTargetCurrentUser, person)
	if err != nil || len(entries) != 1 || entries[0].PreferenceValue != "text" ||
		entries[0].EffectiveStatus() != CuratedStatusActive {
		t.Fatalf("person preference changed unexpectedly: %#v err=%v", entries, err)
	}
}

func TestMigrateLegacyUserStoreSurfacesMalformedSource(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 100_000, 100_000)
	legacyKey := "channel:telegram|account:default|user:42"
	legacyCaller := CallerScope{AgentID: "migration", UserKey: legacyKey}
	path, _, _, err := store.scopePath(CuratedTargetCurrentUser, legacyCaller)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MigrateLegacyUserStoreToPersonScope([]string{legacyKey}, "person:owner"); err == nil {
		t.Fatal("migration silently ignored malformed legacy store")
	}
}

func TestCuratedDocumentLockRegistryReleasesIdlePaths(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 100_000, 100_000)
	for i := 0; i < 40; i++ {
		caller := CallerScope{
			AgentID: "main",
			UserKey: "person:test-" + time.Unix(int64(i+1), 0).UTC().Format("150405"),
		}
		if _, err := store.List(CuratedTargetCurrentUser, caller); err != nil {
			t.Fatal(err)
		}
	}
	curatedDocumentLocks.Lock()
	remaining := len(curatedDocumentLocks.entries)
	curatedDocumentLocks.Unlock()
	if remaining != 0 {
		t.Fatalf("idle document locks retained = %d, want 0", remaining)
	}
}
