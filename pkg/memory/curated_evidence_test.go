package memory

import (
	"testing"
	"time"
)

func TestLegacyBackgroundReviewDefaultsToInferredEvidence(t *testing.T) {
	entry := CuratedEntry{
		ID:         "mem_0123456789abcdef",
		Content:    "User may prefer diagrams",
		Type:       CuratedTypeCommunicationPreference,
		Confidence: 1,
		Provenance: Provenance{Source: "background_review"},
	}
	if got := entry.EffectiveEvidenceKind(); got != CuratedEvidenceInferred {
		t.Fatalf("EffectiveEvidenceKind() = %q, want %q", got, CuratedEvidenceInferred)
	}

	normalized := normalizedCuratedEntry(entry)
	if normalized.EvidenceKind != CuratedEvidenceInferred {
		t.Fatalf("normalized evidence = %q, want %q", normalized.EvidenceKind, CuratedEvidenceInferred)
	}
	if normalized.LastConfirmedAt != nil || normalized.LastVerifiedAt != nil {
		t.Fatal("legacy background inference became confirmed during normalization")
	}
	if normalized.Confidence != DefaultConfidenceForEvidence(CuratedEvidenceInferred) {
		t.Fatalf("normalized confidence = %.2f, want conservative inferred default", normalized.Confidence)
	}
}

func TestReplaceWithoutEvidencePreservesExistingAuthority(t *testing.T) {
	store, err := NewCuratedStore(t.TempDir(), CuratedStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	caller := CallerScope{AgentID: "main", UserKey: "telegram:u1", Channel: "telegram", ChatID: "u1"}

	added, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action:          CuratedActionAdd,
		Content:         "Prefers concise answers",
		Type:            CuratedTypeCommunicationPreference,
		EvidenceKind:    CuratedEvidenceExplicit,
		PreferenceKey:   "communication.verbosity",
		PreferenceValue: "concise",
	}}, false)
	if err != nil || len(added.Applied) != 1 {
		t.Fatalf("add = %#v, %v", added, err)
	}
	id := added.Applied[0].ID

	result, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action:  CuratedActionReplace,
		ID:      id,
		Content: "Prefers concise technical answers",
	}}, false)
	if err != nil || len(result.Applied) != 1 {
		t.Fatalf("replace = %#v, %v", result, err)
	}
	if got := result.Applied[0].EffectiveEvidenceKind(); got != CuratedEvidenceExplicit {
		t.Fatalf("replacement evidence = %q, want explicit", got)
	}
}

func TestObservedEvidenceRequiresRepeatedObservations(t *testing.T) {
	store, err := NewCuratedStore(t.TempDir(), CuratedStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	caller := CallerScope{AgentID: "main", UserKey: "telegram:u-observed", Channel: "telegram", ChatID: "u-observed"}
	high := 0.99
	weak, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Might prefer examples", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceObserved, EvidenceCount: 1, ObservationCount: 1, Confidence: &high,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	entry := weak.Applied[0]
	if entry.EffectiveEvidenceKind() != CuratedEvidenceInferred {
		t.Fatalf("single observation evidence = %q, want inferred", entry.EffectiveEvidenceKind())
	}
	if entry.EffectiveConfidence() > maxConfidenceForEvidence(CuratedEvidenceInferred) {
		t.Fatalf("single observation confidence = %.2f, exceeds inferred cap", entry.EffectiveConfidence())
	}
	if entry.LastConfirmedAt != nil || entry.LastVerifiedAt != nil {
		t.Fatalf("single observation retained confirmation: %#v", entry)
	}

	repeated, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{
		{
			Action:           CuratedActionAdd,
			Content:          "Repeatedly asks for copy-paste commands",
			Type:             CuratedTypeWorkflowPreference,
			EvidenceKind:     CuratedEvidenceObserved,
			EvidenceCount:    2,
			ObservationCount: 2,
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := repeated.Applied[0].EffectiveEvidenceKind(); got != CuratedEvidenceObserved {
		t.Fatalf("repeated observation evidence = %q, want observed", got)
	}
}

func TestReplaceWithoutExplicitEvidenceDoesNotRefreshConfirmation(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	store, err := NewCuratedStore(t.TempDir(), CuratedStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	caller := CallerScope{AgentID: "main", UserKey: "telegram:u-confirm", Channel: "telegram", ChatID: "u-confirm"}
	added, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers detailed explanations", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "detailed",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	original := added.Applied[0].LastConfirmedAt
	if original == nil {
		t.Fatal("explicit preference missing confirmation")
	}
	now = now.Add(2 * time.Hour)
	replaced, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionReplace, ID: added.Applied[0].ID, Content: "Prefers detailed technical explanations",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := replaced.Applied[0].LastConfirmedAt; got == nil || !got.Equal(*original) {
		t.Fatalf("implicit curator rewrite refreshed confirmation: old=%v new=%v", original, got)
	}
}

func TestReplaceToInferenceClearsConfirmationAndCapsConfidence(t *testing.T) {
	store, err := NewCuratedStore(t.TempDir(), CuratedStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	caller := CallerScope{AgentID: "main", UserKey: "telegram:u-demote", Channel: "telegram", ChatID: "u-demote"}
	added, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers Rust", Type: CuratedTypeWorkflowPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "workflow.programming_language", PreferenceValue: "rust",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	high := 0.99
	replaced, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionReplace, ID: added.Applied[0].ID, Content: "May prefer Rust",
		EvidenceKind: CuratedEvidenceInferred, Confidence: &high,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	entry := replaced.Applied[0]
	if entry.LastConfirmedAt != nil || entry.LastVerifiedAt != nil {
		t.Fatalf("inference retained explicit confirmation: %#v", entry)
	}
	if entry.EffectiveConfidence() > maxConfidenceForEvidence(CuratedEvidenceInferred) {
		t.Fatalf("inference confidence %.2f exceeds cap", entry.EffectiveConfidence())
	}
}
