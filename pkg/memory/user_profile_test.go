package memory

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCuratedExplicitPreferenceSupersedesOlderValue(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 10_000, 10_000)
	caller := testCaller("telegram:user-profile")
	first, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers concise responses", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "concise",
	}}, false)
	if err != nil {
		t.Fatalf("first preference: %v", err)
	}
	second, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Now prefers detailed responses", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "detailed",
	}}, false)
	if err != nil {
		t.Fatalf("corrected preference: %v", err)
	}
	old, _ := store.Inspect(CuratedTargetCurrentUser, caller, first.Applied[0].ID)
	current, _ := store.Inspect(CuratedTargetCurrentUser, caller, second.Applied[0].ID)
	if old.EffectiveStatus() != CuratedStatusSuperseded {
		t.Fatalf("old status = %q, want superseded", old.EffectiveStatus())
	}
	if current.EffectiveStatus() != CuratedStatusActive || current.PreferenceValue != "detailed" {
		t.Fatalf("current preference = %#v", current)
	}
	if current.Supersedes != old.ID {
		t.Fatalf("current supersedes = %q, want %q", current.Supersedes, old.ID)
	}
}

func TestCuratedInferenceCannotOverrideExplicitPreference(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 10_000, 10_000)
	caller := testCaller("telegram:user-authority")
	explicit, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "User explicitly prefers Rust", Type: CuratedTypeWorkflowPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "workflow.programming_language", PreferenceValue: "rust",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	inferred, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{
		{
			Action:          CuratedActionAdd,
			Content:         "Recent discussion suggests Go may be preferred",
			Type:            CuratedTypeWorkflowPreference,
			EvidenceKind:    CuratedEvidenceInferred,
			PreferenceKey:   "workflow.programming_language",
			PreferenceValue: "go",
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	keep, _ := store.Inspect(CuratedTargetCurrentUser, caller, explicit.Applied[0].ID)
	weak, _ := store.Inspect(CuratedTargetCurrentUser, caller, inferred.Applied[0].ID)
	if keep.EffectiveStatus() != CuratedStatusActive {
		t.Fatalf("explicit preference lost authority: %#v", keep)
	}
	if weak.EffectiveStatus() != CuratedStatusSuperseded {
		t.Fatalf("inference remained active: %#v", weak)
	}
}

func TestCuratedInferredMemoryIsNotAutomaticallyConfirmed(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 10_000, 10_000)
	caller := testCaller("telegram:user-confidence")
	result, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{
		{
			Action:       CuratedActionAdd,
			Content:      "May prefer examples before theory",
			Type:         CuratedTypeCommunicationPreference,
			EvidenceKind: CuratedEvidenceInferred,
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	entry := result.Applied[0]
	if entry.EffectiveConfidence() >= 0.9 {
		t.Fatalf("inference confidence = %.2f, want conservative", entry.EffectiveConfidence())
	}
	if entry.LastConfirmedAt != nil || entry.LastVerifiedAt != nil {
		t.Fatalf("inference was incorrectly confirmed: %#v", entry)
	}
}

func TestCompileUserProfileIsBoundedDerivedAndIsolated(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 10_000, 10_000)
	userA := testCaller("telegram:user-a-profile")
	userB := testCaller("telegram:user-b-profile")
	mutations := []CuratedMutation{
		{
			Action:          CuratedActionAdd,
			Content:         "Prefers Indonesian",
			Type:            CuratedTypeCommunicationPreference,
			EvidenceKind:    CuratedEvidenceExplicit,
			PreferenceKey:   "communication.language",
			PreferenceValue: "id",
		},
		{
			Action:          CuratedActionAdd,
			Content:         "Prefers copy-paste-ready commands",
			Type:            CuratedTypeWorkflowPreference,
			EvidenceKind:    CuratedEvidenceExplicit,
			PreferenceKey:   "workflow.command_style",
			PreferenceValue: "copy_paste_ready",
		},
		{
			Action:       CuratedActionAdd,
			Content:      "Temporary project branch is experiment/x",
			Type:         CuratedTypeProjectFact,
			EvidenceKind: CuratedEvidenceExplicit,
		},
		{
			Action:       CuratedActionAdd,
			Content:      "Might like diagrams",
			Type:         CuratedTypeCommunicationPreference,
			EvidenceKind: CuratedEvidenceInferred,
		},
	}
	if _, applyErr := store.ApplyBatch(CuratedTargetCurrentUser, userA, mutations, false); applyErr != nil {
		t.Fatal(applyErr)
	}
	profile, err := store.CompileUserProfile(userA, UserProfileOptions{MaxChars: 900, MinConfidence: 0.65})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Communication) != 1 || profile.Communication[0].Key != "communication.language" {
		t.Fatalf("communication profile = %#v", profile.Communication)
	}
	if len(profile.Workflow) != 1 || profile.Workflow[0].Value != "copy_paste_ready" {
		t.Fatalf("workflow profile = %#v", profile.Workflow)
	}
	if len(profile.SourceIDs) != 2 || profile.Characters > 900 {
		t.Fatalf("profile bounds/source ids = %#v", profile)
	}
	other, err := store.CompileUserProfile(userB, UserProfileOptions{MaxChars: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(other.SourceIDs) != 0 {
		t.Fatalf("cross-user profile leak: %#v", other)
	}
}

func TestCompileUserProfileCacheInvalidatesAfterMutation(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 10_000, 10_000)
	caller := testCaller("telegram:user-cache")
	first, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Concise answers", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "concise",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	profile1, _ := store.CompileUserProfile(caller, UserProfileOptions{MaxChars: 500})
	if len(profile1.Communication) != 1 || profile1.Communication[0].Value != "concise" {
		t.Fatalf("first profile = %#v", profile1)
	}
	if _, applyErr := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Detailed answers now", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "detailed",
		Supersedes: first.Applied[0].ID,
	}}, false); applyErr != nil {
		t.Fatal(applyErr)
	}
	profile2, _ := store.CompileUserProfile(caller, UserProfileOptions{MaxChars: 500})
	if len(profile2.Communication) != 1 || profile2.Communication[0].Value != "detailed" {
		t.Fatalf("cache did not invalidate: %#v", profile2)
	}
}

func TestRestoredPreferenceReconcilesSameKey(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	store, err := NewCuratedStore(
		filepath.Join(t.TempDir(), "curated"),
		CuratedStoreOptions{Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	caller := testCaller("telegram:user-restore")
	old, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers concise responses", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "concise",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, applyErr := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionArchive, ID: old.Applied[0].ID,
	}}, false); applyErr != nil {
		t.Fatal(applyErr)
	}
	now = now.Add(time.Hour)
	current, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers detailed responses", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "detailed",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, applyErr := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionRestore, ID: old.Applied[0].ID,
	}}, false); applyErr != nil {
		t.Fatal(applyErr)
	}
	oldEntry, _ := store.Inspect(CuratedTargetCurrentUser, caller, old.Applied[0].ID)
	currentEntry, _ := store.Inspect(CuratedTargetCurrentUser, caller, current.Applied[0].ID)
	if oldEntry.EffectiveStatus() == CuratedStatusActive && currentEntry.EffectiveStatus() == CuratedStatusActive {
		t.Fatalf("restore produced two active values: old=%#v current=%#v", oldEntry, currentEntry)
	}
	if currentEntry.EffectiveStatus() != CuratedStatusActive || currentEntry.PreferenceValue != "detailed" {
		t.Fatalf("newer confirmed preference lost after restore: %#v", currentEntry)
	}
}

func TestStructuredSupersedesRejectsDifferentPreferenceKey(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 10_000, 10_000)
	caller := testCaller("telegram:user-key-safety")
	language, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers Indonesian", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.language", PreferenceValue: "id",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers detailed answers", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "detailed",
		Supersedes: language.Applied[0].ID,
	}}, false)
	if !errors.Is(err, ErrCuratedInvalidPreferenceKey) {
		t.Fatalf("cross-key supersedes error = %v, want ErrCuratedInvalidPreferenceKey", err)
	}
}

func TestCompileUserProfileCacheExpiresWithMemory(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	store, err := NewCuratedStore(
		filepath.Join(t.TempDir(), "curated"),
		CuratedStoreOptions{Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	caller := testCaller("telegram:user-expiry")
	expires := now.Add(time.Hour)
	if _, applyErr := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{
		{
			Action:          CuratedActionAdd,
			Content:         "Temporarily prefers concise answers",
			Type:            CuratedTypeCommunicationPreference,
			EvidenceKind:    CuratedEvidenceExplicit,
			PreferenceKey:   "communication.verbosity",
			PreferenceValue: "concise",
			ExpiresAt:       &expires,
		},
	}, false); applyErr != nil {
		t.Fatal(applyErr)
	}
	profile, err := store.CompileUserProfile(caller, UserProfileOptions{MaxChars: 800, Now: now})
	if err != nil || len(profile.SourceIDs) != 1 {
		t.Fatalf("initial profile = %#v, %v", profile, err)
	}
	now = now.Add(2 * time.Hour)
	expired, err := store.CompileUserProfile(caller, UserProfileOptions{MaxChars: 800, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(expired.SourceIDs) != 0 {
		t.Fatalf("expired preference survived profile cache: %#v", expired)
	}
}

func TestCompileUserProfileSerializedBudgetIsHardBound(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 20_000, 20_000)
	caller := testCaller("telegram:user-budget")
	for i := 0; i < 12; i++ {
		content := "Stable interaction preference number " + string(rune('A'+i)) + " with additional descriptive text"
		if _, applyErr := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
			Action: CuratedActionAdd, Content: content, Type: CuratedTypeCommunicationPreference,
			EvidenceKind: CuratedEvidenceExplicit,
		}}, false); applyErr != nil {
			t.Fatal(applyErr)
		}
	}
	const maxChars = 600
	profile, err := store.CompileUserProfile(caller, UserProfileOptions{MaxChars: maxChars})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if got := utf8.RuneCount(data); got > maxChars {
		t.Fatalf("serialized profile chars = %d, exceeds %d: %s", got, maxChars, data)
	}
	if profile.Characters != utf8.RuneCount(data) {
		t.Fatalf("profile Characters = %d, actual serialized = %d", profile.Characters, utf8.RuneCount(data))
	}
}

func TestCompileUserProfileCustomNowBypassesRealtimeCache(t *testing.T) {
	base := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	now := base
	store, err := NewCuratedStore(
		filepath.Join(t.TempDir(), "curated"),
		CuratedStoreOptions{Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	caller := testCaller("telegram:user-asof")
	expires := base.Add(time.Hour)
	if _, applyErr := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{
		{
			Action:          CuratedActionAdd,
			Content:         "Temporarily prefers concise answers",
			Type:            CuratedTypeCommunicationPreference,
			EvidenceKind:    CuratedEvidenceExplicit,
			PreferenceKey:   "communication.verbosity",
			PreferenceValue: "concise",
			ExpiresAt:       &expires,
		},
	}, false); applyErr != nil {
		t.Fatal(applyErr)
	}

	// Populate the real-time cache after expiry with an empty profile.
	now = base.Add(2 * time.Hour)
	realtime, err := store.CompileUserProfile(caller, UserProfileOptions{MaxChars: 800})
	if err != nil {
		t.Fatal(err)
	}
	if len(realtime.SourceIDs) != 0 {
		t.Fatalf("expired real-time profile = %#v, want empty", realtime)
	}

	// A historical/as-of query must not reuse the real-time cache.
	historical, err := store.CompileUserProfile(caller, UserProfileOptions{
		MaxChars: 800,
		Now:      base.Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(historical.Communication) != 1 || historical.Communication[0].Value != "concise" {
		t.Fatalf("historical profile reused real-time cache: %#v", historical)
	}
}

func TestExplicitRestoreReaffirmsArchivedPreference(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	store, err := NewCuratedStore(
		filepath.Join(t.TempDir(), "curated"),
		CuratedStoreOptions{Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	caller := testCaller("telegram:user-explicit-restore")
	old, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers concise responses", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "concise",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, applyErr := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionArchive, ID: old.Applied[0].ID,
	}}, false); applyErr != nil {
		t.Fatal(applyErr)
	}

	now = now.Add(time.Hour)
	current, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers detailed responses", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "detailed",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Hour)
	reaffirmedAt := now
	if _, applyErr := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionRestore, ID: old.Applied[0].ID,
		EvidenceKind: CuratedEvidenceExplicit,
		Provenance:   Provenance{Source: "user_command"},
	}}, false); applyErr != nil {
		t.Fatal(applyErr)
	}
	oldEntry, _ := store.Inspect(CuratedTargetCurrentUser, caller, old.Applied[0].ID)
	currentEntry, _ := store.Inspect(CuratedTargetCurrentUser, caller, current.Applied[0].ID)
	if oldEntry.EffectiveStatus() != CuratedStatusActive || oldEntry.PreferenceValue != "concise" {
		t.Fatalf("explicitly restored preference did not become active: %#v", oldEntry)
	}
	if currentEntry.EffectiveStatus() != CuratedStatusSuperseded {
		t.Fatalf("newer preference remained active after explicit reaffirmation: %#v", currentEntry)
	}
	if oldEntry.LastConfirmedAt == nil || !oldEntry.LastConfirmedAt.Equal(reaffirmedAt) {
		t.Fatalf("restore confirmation = %#v, want %s", oldEntry.LastConfirmedAt, reaffirmedAt)
	}
}
