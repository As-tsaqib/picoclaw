package memory

import (
	"path/filepath"
	"testing"
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
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, userA, mutations, false); err != nil {
		t.Fatal(err)
	}
	profile, err := store.CompileUserProfile(userA, UserProfileOptions{MaxChars: 500, MinConfidence: 0.65})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Communication) != 1 || profile.Communication[0].Key != "communication.language" {
		t.Fatalf("communication profile = %#v", profile.Communication)
	}
	if len(profile.Workflow) != 1 || profile.Workflow[0].Value != "copy_paste_ready" {
		t.Fatalf("workflow profile = %#v", profile.Workflow)
	}
	if len(profile.SourceIDs) != 2 || profile.Characters > 500 {
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
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Detailed answers now", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "detailed",
		Supersedes: first.Applied[0].ID,
	}}, false); err != nil {
		t.Fatal(err)
	}
	profile2, _ := store.CompileUserProfile(caller, UserProfileOptions{MaxChars: 500})
	if len(profile2.Communication) != 1 || profile2.Communication[0].Value != "detailed" {
		t.Fatalf("cache did not invalidate: %#v", profile2)
	}
}
