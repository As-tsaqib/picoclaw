package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/sipeed/picoclaw/pkg/memory"
)

type personalizationBenchReport struct {
	PreferenceCorrectionPass bool    `json:"preference_correction_pass"`
	InferenceResistancePass  bool    `json:"inference_resistance_pass"`
	PrivacyIsolationPass     bool    `json:"privacy_isolation_pass"`
	ProfileCharacters        int     `json:"profile_characters"`
	ProfileSources           int     `json:"profile_sources"`
	ProfileBuildMicros       int64   `json:"profile_build_micros"`
	ProfileCachedMicros      int64   `json:"profile_cached_micros"`
	RetrievalMicros          int64   `json:"retrieval_micros"`
	CorrectionMutations      int     `json:"correction_mutations"`
	PrivacyLeakageRate       float64 `json:"privacy_leakage_rate"`
}

func personalizationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "personalization",
		Short: "Run deterministic personalization/memory semantics benchmarks",
		RunE:  runPersonalization,
	}
	cmd.Flags().StringVar(&flagOut, "out", "./bench-out", "output working directory")
	return cmd
}

func runPersonalization(_ *cobra.Command, _ []string) error {
	root, err := os.MkdirTemp("", "picoclaw-personalization-bench-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	store, err := memory.NewCuratedStore(filepath.Join(root, "curated"), memory.CuratedStoreOptions{
		WorkspaceCharLimit: 12_000,
		PerUserCharLimit:   8_000,
	})
	if err != nil {
		return err
	}
	userA := memory.CallerScope{AgentID: "main", UserKey: "bench:user-a", Channel: "cli", SessionKey: "bench-a", SessionRef: "bench-a"}
	userB := memory.CallerScope{AgentID: "main", UserKey: "bench:user-b", Channel: "cli", SessionKey: "bench-b", SessionRef: "bench-b"}
	first, err := store.ApplyBatch(memory.CuratedTargetCurrentUser, userA, []memory.CuratedMutation{
		{Action: memory.CuratedActionAdd, Content: "Prefers Indonesian", Type: memory.CuratedTypeCommunicationPreference, EvidenceKind: memory.CuratedEvidenceExplicit, PreferenceKey: "communication.language", PreferenceValue: "id"},
		{Action: memory.CuratedActionAdd, Content: "Prefers concise explanations", Type: memory.CuratedTypeCommunicationPreference, EvidenceKind: memory.CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "concise"},
	}, false)
	if err != nil {
		return err
	}
	start := time.Now()
	profile1, err := store.CompileUserProfile(userA, memory.UserProfileOptions{MaxChars: 1_200, MinConfidence: 0.65})
	profileBuild := time.Since(start)
	if err != nil {
		return err
	}
	start = time.Now()
	_, err = store.CompileUserProfile(userA, memory.UserProfileOptions{MaxChars: 1_200, MinConfidence: 0.65})
	profileCached := time.Since(start)
	if err != nil {
		return err
	}
	verbosityID := ""
	for _, entry := range first.Applied {
		if entry.PreferenceKey == "communication.verbosity" {
			verbosityID = entry.ID
		}
	}
	if verbosityID == "" {
		return fmt.Errorf("benchmark setup missing verbosity preference")
	}
	correction, err := store.ApplyBatch(memory.CuratedTargetCurrentUser, userA, []memory.CuratedMutation{{
		Action: memory.CuratedActionAdd, Content: "Now prefers detailed explanations", Type: memory.CuratedTypeCommunicationPreference,
		EvidenceKind: memory.CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "detailed", Supersedes: verbosityID,
	}}, false)
	if err != nil {
		return err
	}
	profile2, err := store.CompileUserProfile(userA, memory.UserProfileOptions{MaxChars: 1_200, MinConfidence: 0.65})
	if err != nil {
		return err
	}
	correctionPass := profileHasValue(profile2, "communication.verbosity", "detailed") && !profileHasValue(profile2, "communication.verbosity", "concise")
	_, err = store.ApplyBatch(memory.CuratedTargetCurrentUser, userA, []memory.CuratedMutation{{
		Action: memory.CuratedActionAdd, Content: "Recent messages might suggest concise replies", Type: memory.CuratedTypeCommunicationPreference,
		EvidenceKind: memory.CuratedEvidenceInferred, PreferenceKey: "communication.verbosity", PreferenceValue: "concise",
	}}, false)
	if err != nil {
		return err
	}
	profile3, err := store.CompileUserProfile(userA, memory.UserProfileOptions{MaxChars: 1_200, MinConfidence: 0.65})
	if err != nil {
		return err
	}
	inferencePass := profileHasValue(profile3, "communication.verbosity", "detailed")
	other, err := store.CompileUserProfile(userB, memory.UserProfileOptions{MaxChars: 1_200, MinConfidence: 0.65})
	if err != nil {
		return err
	}
	privacyPass := len(other.SourceIDs) == 0
	start = time.Now()
	_, err = store.Retrieve(memory.CuratedTargetCurrentUser, userA, memory.CuratedRetrievalOptions{
		Query: "detailed explanation preference", MaxResults: 6, MaxChars: 2_800, PinnedChars: 800,
		MinimumScore: 0.1, RecencyWeight: 0.25, RecencyHalfLifeDays: 90, StaleAfterDays: 180, FuzzyWeight: 0.75,
		RecentFallbackCount: 2,
	})
	retrievalDuration := time.Since(start)
	if err != nil {
		return err
	}
	report := personalizationBenchReport{
		PreferenceCorrectionPass: correctionPass,
		InferenceResistancePass:  inferencePass,
		PrivacyIsolationPass:     privacyPass,
		ProfileCharacters:        profile1.Characters,
		ProfileSources:           len(profile1.SourceIDs),
		ProfileBuildMicros:       profileBuild.Microseconds(),
		ProfileCachedMicros:      profileCached.Microseconds(),
		RetrievalMicros:          retrievalDuration.Microseconds(),
		CorrectionMutations:      len(correction.Applied),
	}
	if !privacyPass {
		report.PrivacyLeakageRate = 1
	}
	if err := os.MkdirAll(flagOut, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(flagOut, "personalization.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s\n", data)
	if !correctionPass || !inferencePass || !privacyPass {
		return fmt.Errorf("personalization benchmark failed; see %s", path)
	}
	return nil
}

func profileHasValue(profile memory.UserProfileSnapshot, key, value string) bool {
	groups := [][]memory.UserProfileField{profile.Identity, profile.Communication, profile.Workflow, profile.Interaction, profile.Boundaries}
	for _, group := range groups {
		for _, field := range group {
			if field.Key == key && field.Value == value {
				return true
			}
		}
	}
	return false
}
