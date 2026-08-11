package memory

import (
	"path/filepath"
	"testing"
)

func BenchmarkCompileUserProfileCached(b *testing.B) {
	store, err := NewCuratedStore(
		filepath.Join(b.TempDir(), "curated"),
		CuratedStoreOptions{WorkspaceCharLimit: 12_000, PerUserCharLimit: 8_000},
	)
	if err != nil {
		b.Fatal(err)
	}
	caller := testCaller("bench:profile")
	for i, item := range []struct{ key, value, content string }{
		{"communication.language", "id", "Prefers Indonesian"},
		{"communication.verbosity", "concise", "Prefers concise answers"},
		{"workflow.command_style", "copy_paste_ready", "Prefers commands ready to copy"},
		{"interaction.basic_explanations", "avoid_unless_requested", "Avoid repeated basic explanations"},
	} {
		entryType := CuratedTypeCommunicationPreference
		if i >= 2 {
			entryType = CuratedTypeWorkflowPreference
		}
		if _, err := store.ApplyBatch(
			CuratedTargetCurrentUser,
			caller,
			[]CuratedMutation{
				{
					Action:          CuratedActionAdd,
					Content:         item.content,
					Type:            entryType,
					EvidenceKind:    CuratedEvidenceExplicit,
					PreferenceKey:   item.key,
					PreferenceValue: item.value,
				},
			},
			false,
		); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := store.CompileUserProfile(
		caller,
		UserProfileOptions{MaxChars: 1_200, MinConfidence: 0.65},
	); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.CompileUserProfile(
			caller,
			UserProfileOptions{MaxChars: 1_200, MinConfidence: 0.65},
		); err != nil {
			b.Fatal(err)
		}
	}
}
