package memory

import "testing"

func TestNewRetrievalEngineKeepsLexicalFallback(t *testing.T) {
	for _, value := range []string{"", "hybrid_lexical", "future_semantic_engine"} {
		engine := NewRetrievalEngine(value)
		if engine == nil || engine.Name() != RetrievalEngineHybridLexical {
			t.Fatalf("engine(%q) = %#v", value, engine)
		}
	}
}

func TestSemanticRerankFindsMultilingualParaphrase(t *testing.T) {
	store, err := NewCuratedStore(t.TempDir(), CuratedStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	caller := CallerScope{AgentID: "main", UserKey: "telegram:semantic-user"}
	added, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{
		{
			Action: CuratedActionAdd, Content: "User prefers copy-paste-ready shell commands",
			Type: CuratedTypeWorkflowPreference, EvidenceKind: CuratedEvidenceExplicit,
		},
		{
			Action: CuratedActionAdd, Content: "User enjoys gardening books",
			Type: CuratedTypeOther, EvidenceKind: CuratedEvidenceExplicit,
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewRetrievalEngine(RetrievalEngineSemanticRerank)
	if engine.Name() != RetrievalEngineSemanticRerank {
		t.Fatalf("engine name = %q", engine.Name())
	}
	result, err := engine.Retrieve(store, CuratedTargetCurrentUser, caller, CuratedRetrievalOptions{
		Query: "Kasih perintah yang tinggal saya jalankan", MaxResults: 2, MaxChars: 1_000,
		MinimumScore: 0.35, SemanticWeight: 2.5, RecentFallbackCount: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].ID != added.Applied[0].ID {
		t.Fatalf("semantic result = %#v, want only copy-ready preference", result.Entries)
	}
}

func TestSemanticRerankSupportsConciseCrossLanguageQuery(t *testing.T) {
	store, err := NewCuratedStore(t.TempDir(), CuratedStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	caller := CallerScope{AgentID: "main", UserKey: "telegram:concise-user"}
	added, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "User prefers concise answers",
		Type: CuratedTypeCommunicationPreference, EvidenceKind: CuratedEvidenceExplicit,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewRetrievalEngine(RetrievalEngineSemanticRerank).Retrieve(
		store, CuratedTargetCurrentUser, caller, CuratedRetrievalOptions{
			Query: "Jangan jelaskan kepanjangan", MaxResults: 2, MaxChars: 1_000,
			MinimumScore: 0.35, RecentFallbackCount: 0,
		},
	)
	if err != nil || len(result.Entries) != 1 || result.Entries[0].ID != added.Applied[0].ID {
		t.Fatalf("cross-language result = %#v err=%v", result.Entries, err)
	}
}
