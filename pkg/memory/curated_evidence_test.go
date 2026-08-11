package memory

import "testing"

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
