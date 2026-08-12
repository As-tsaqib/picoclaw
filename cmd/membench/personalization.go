package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sipeed/picoclaw/pkg/memory"
)

type personalizationBenchReport struct {
	PreferenceCorrectionPass  bool    `json:"preference_correction_pass"`
	InferenceResistancePass   bool    `json:"inference_resistance_pass"`
	PrivacyIsolationPass      bool    `json:"privacy_isolation_pass"`
	GroupIsolationPass        bool    `json:"group_isolation_pass"`
	SemanticParaphrasePass    bool    `json:"semantic_paraphrase_pass"`
	LongHorizonPass           bool    `json:"long_horizon_pass"`
	CrossTopicProfilePass     bool    `json:"cross_topic_profile_pass"`
	FalseMemoryResistancePass bool    `json:"false_memory_resistance_pass"`
	PreferenceAdherence       float64 `json:"preference_adherence_accuracy"`
	SemanticRecallAtK         float64 `json:"semantic_recall_at_k"`
	FalseMemoryWriteRate      float64 `json:"false_memory_write_rate"`
	ProfileCharacters         int     `json:"profile_characters"`
	RetrievedCharacters       int     `json:"retrieved_characters"`
	PromptCharacterOverhead   int     `json:"prompt_character_overhead"`
	PromptTokenEstimate       int     `json:"prompt_token_estimate"`
	ProfileSources            int     `json:"profile_sources"`
	ProfileBuildMicros        int64   `json:"profile_build_micros"`
	ProfileCachedMicros       int64   `json:"profile_cached_micros"`
	RetrievalMicros           int64   `json:"retrieval_micros"`
	CorrectionMutations       int     `json:"correction_mutations"`
	LongHorizonTurns          int     `json:"long_horizon_turns"`
	MemoryEntriesBefore       int     `json:"memory_entries_before"`
	MemoryEntriesAfter        int     `json:"memory_entries_after"`
	PrivacyLeakageRate        float64 `json:"privacy_leakage_rate"`
	MemoryPollutionRate       float64 `json:"memory_pollution_rate"`
	StalePreferenceRate       float64 `json:"stale_preference_violation_rate"`
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
	userA := memory.CallerScope{
		AgentID:    "main",
		UserKey:    "bench:user-a",
		Channel:    "cli",
		SessionKey: "bench-a",
		SessionRef: "bench-a",
	}
	userB := memory.CallerScope{
		AgentID:    "main",
		UserKey:    "bench:user-b",
		Channel:    "cli",
		SessionKey: "bench-b",
		SessionRef: "bench-b",
	}
	sharedGroup := userA
	sharedGroup.ChatID = "bench-group"
	sharedGroup.GroupID = "bench-group"
	sharedGroup.SessionKey = "bench-group-topic"
	sharedGroup.SessionRef = "bench-group-topic"
	first, err := store.ApplyBatch(memory.CuratedTargetCurrentUser, userA, []memory.CuratedMutation{
		{
			Action:          memory.CuratedActionAdd,
			Content:         "Prefers Indonesian",
			Type:            memory.CuratedTypeCommunicationPreference,
			EvidenceKind:    memory.CuratedEvidenceExplicit,
			PreferenceKey:   "communication.language",
			PreferenceValue: "id",
		},
		{
			Action:          memory.CuratedActionAdd,
			Content:         "Prefers concise explanations",
			Type:            memory.CuratedTypeCommunicationPreference,
			EvidenceKind:    memory.CuratedEvidenceExplicit,
			PreferenceKey:   "communication.verbosity",
			PreferenceValue: "concise",
		},
		{
			Action:          memory.CuratedActionAdd,
			Content:         "Prefers copy-paste-ready commands",
			Type:            memory.CuratedTypeWorkflowPreference,
			EvidenceKind:    memory.CuratedEvidenceExplicit,
			PreferenceKey:   "workflow.command_style",
			PreferenceValue: "copy_paste_ready",
		},
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
	correction, err := store.ApplyBatch(memory.CuratedTargetCurrentUser, userA, []memory.CuratedMutation{
		{
			Action:          memory.CuratedActionAdd,
			Content:         "Now prefers detailed explanations",
			Type:            memory.CuratedTypeCommunicationPreference,
			EvidenceKind:    memory.CuratedEvidenceExplicit,
			PreferenceKey:   "communication.verbosity",
			PreferenceValue: "detailed",
			Supersedes:      verbosityID,
		},
	}, false)
	if err != nil {
		return err
	}
	profile2, err := store.CompileUserProfile(userA, memory.UserProfileOptions{MaxChars: 1_200, MinConfidence: 0.65})
	if err != nil {
		return err
	}
	correctionPass := profileHasValue(profile2, "communication.verbosity", "detailed") &&
		!profileHasValue(profile2, "communication.verbosity", "concise")
	_, err = store.ApplyBatch(memory.CuratedTargetCurrentUser, userA, []memory.CuratedMutation{
		{
			Action:          memory.CuratedActionAdd,
			Content:         "Recent messages might suggest concise replies",
			Type:            memory.CuratedTypeCommunicationPreference,
			EvidenceKind:    memory.CuratedEvidenceInferred,
			PreferenceKey:   "communication.verbosity",
			PreferenceValue: "concise",
		},
	}, false)
	if err != nil {
		return err
	}
	requestedInferenceConfidence := 1.0
	weakInference, err := store.ApplyBatch(
		memory.CuratedTargetCurrentUser,
		userA,
		[]memory.CuratedMutation{{
			Action:          memory.CuratedActionAdd,
			Content:         "Recent discussion may suggest interest in Rust",
			Type:            memory.CuratedTypeWorkflowPreference,
			Confidence:      &requestedInferenceConfidence,
			EvidenceKind:    memory.CuratedEvidenceInferred,
			PreferenceKey:   "workflow.programming_language",
			PreferenceValue: "rust",
		}},
		false,
	)
	if err != nil {
		return err
	}
	if len(weakInference.Applied) != 1 {
		return fmt.Errorf("benchmark setup missing weak inference")
	}
	profile3, err := store.CompileUserProfile(userA, memory.UserProfileOptions{MaxChars: 1_200, MinConfidence: 0.65})
	if err != nil {
		return err
	}
	inferencePass := profileHasValue(profile3, "communication.verbosity", "detailed")
	statsBefore, err := store.Stats(memory.CuratedTargetCurrentUser, userA)
	if err != nil {
		return err
	}
	const longHorizonTurns = 100
	for turn := 0; turn < longHorizonTurns; turn++ {
		query := fmt.Sprintf("temporary unrelated turn %d about weather and lunch", turn)
		if _, retrieveErr := store.Retrieve(memory.CuratedTargetCurrentUser, userA, memory.CuratedRetrievalOptions{
			Query: query, MaxResults: 6, MaxChars: 2_800, PinnedChars: 800,
			MinimumScore: 0.35, RecencyWeight: 0.25, RecencyHalfLifeDays: 90,
			StaleAfterDays: 180, FuzzyWeight: 0.75, RecentFallbackCount: 0,
		}); retrieveErr != nil {
			return retrieveErr
		}
	}
	profileAfterLongHorizon, err := store.CompileUserProfile(
		userA,
		memory.UserProfileOptions{MaxChars: 1_200, MinConfidence: 0.65},
	)
	if err != nil {
		return err
	}
	statsAfter, err := store.Stats(memory.CuratedTargetCurrentUser, userA)
	if err != nil {
		return err
	}
	longHorizonPass := profileHasValue(profileAfterLongHorizon, "communication.language", "id") &&
		profileHasValue(profileAfterLongHorizon, "communication.verbosity", "detailed") &&
		!profileHasValue(profileAfterLongHorizon, "communication.verbosity", "concise")
	crossTopicPass := profileHasValue(profileAfterLongHorizon, "workflow.command_style", "copy_paste_ready")
	pollutionRate := float64(statsAfter.Entries-statsBefore.Entries) / float64(longHorizonTurns)
	if pollutionRate < 0 {
		pollutionRate = 0
	}
	stalePreferenceRate := 0.0
	if profileHasValue(profileAfterLongHorizon, "communication.verbosity", "concise") {
		stalePreferenceRate = 1
	}
	other, err := store.CompileUserProfile(userB, memory.UserProfileOptions{MaxChars: 1_200, MinConfidence: 0.65})
	if err != nil {
		return err
	}
	privacyPass := len(other.SourceIDs) == 0
	start = time.Now()
	retrieved, err := store.Retrieve(memory.CuratedTargetCurrentUser, userA, memory.CuratedRetrievalOptions{
		Query: "detailed explanation preference", MaxResults: 6, MaxChars: 2_800, PinnedChars: 800,
		MinimumScore: 0.1, RecencyWeight: 0.25, RecencyHalfLifeDays: 90, StaleAfterDays: 180, FuzzyWeight: 0.75,
		RecentFallbackCount: 2,
	})
	retrievalDuration := time.Since(start)
	if err != nil {
		return err
	}
	semantic, err := memory.NewRetrievalEngine(memory.RetrievalEngineSemanticRerank).Retrieve(
		store,
		memory.CuratedTargetCurrentUser,
		userA,
		memory.CuratedRetrievalOptions{
			Query:      "Jangan jelaskan kepanjangan; kasih perintah yang tinggal saya jalankan",
			MaxResults: 6, MaxChars: 2_800, PinnedChars: 800,
			MinimumScore: 0.35, RecencyWeight: 0.25, RecencyHalfLifeDays: 90,
			StaleAfterDays: 180, FuzzyWeight: 0.75, RecentFallbackCount: 0,
		},
	)
	if err != nil {
		return err
	}
	semanticPass := entriesContainPreference(
		semantic.Entries,
		"workflow.command_style",
		"copy_paste_ready",
	)
	groupPass := !memory.AllowsPrivateUserMemory(sharedGroup)
	inferredRust := weakInference.Applied[0]
	falseMemoryWrites := 0
	if inferredRust.EffectiveEvidenceKind() != memory.CuratedEvidenceInferred ||
		inferredRust.EffectiveConfidence() > 0.60 ||
		profileHasValue(profileAfterLongHorizon, "workflow.programming_language", "rust") {
		falseMemoryWrites++
	}
	falseMemoryWriteRate := float64(falseMemoryWrites)
	falseMemoryPass := falseMemoryWriteRate == 0 &&
		!entriesContainContent(semantic.Entries, "permanently prefers Rust")
	promptOverhead := profileAfterLongHorizon.Characters + retrieved.Characters
	preferenceChecks := []bool{
		profileHasValue(profileAfterLongHorizon, "communication.language", "id"),
		profileHasValue(profileAfterLongHorizon, "communication.verbosity", "detailed"),
		profileHasValue(profileAfterLongHorizon, "workflow.command_style", "copy_paste_ready"),
	}
	adherence := passedFraction(preferenceChecks)
	semanticRecall := 0.0
	if semanticPass {
		semanticRecall = 1
	}
	report := personalizationBenchReport{
		PreferenceCorrectionPass:  correctionPass,
		InferenceResistancePass:   inferencePass,
		PrivacyIsolationPass:      privacyPass,
		GroupIsolationPass:        groupPass,
		SemanticParaphrasePass:    semanticPass,
		LongHorizonPass:           longHorizonPass,
		CrossTopicProfilePass:     crossTopicPass,
		FalseMemoryResistancePass: falseMemoryPass,
		PreferenceAdherence:       adherence,
		SemanticRecallAtK:         semanticRecall,
		ProfileCharacters:         profile1.Characters,
		RetrievedCharacters:       retrieved.Characters,
		PromptCharacterOverhead:   promptOverhead,
		PromptTokenEstimate:       (promptOverhead + 3) / 4,
		ProfileSources:            len(profile1.SourceIDs),
		ProfileBuildMicros:        profileBuild.Microseconds(),
		ProfileCachedMicros:       profileCached.Microseconds(),
		RetrievalMicros:           retrievalDuration.Microseconds(),
		CorrectionMutations:       len(correction.Applied),
		LongHorizonTurns:          longHorizonTurns,
		MemoryEntriesBefore:       statsBefore.Entries,
		MemoryEntriesAfter:        statsAfter.Entries,
		MemoryPollutionRate:       pollutionRate,
		FalseMemoryWriteRate:      falseMemoryWriteRate,
		StalePreferenceRate:       stalePreferenceRate,
	}
	if !privacyPass {
		report.PrivacyLeakageRate = 1
	}
	if mkdirErr := os.MkdirAll(flagOut, 0o755); mkdirErr != nil {
		return mkdirErr
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(flagOut, "personalization.json")
	if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
		return writeErr
	}
	fmt.Printf("%s\n", data)
	if !correctionPass || !inferencePass || !privacyPass || !groupPass || !semanticPass ||
		!falseMemoryPass || !longHorizonPass || !crossTopicPass || adherence != 1 ||
		semanticRecall != 1 || falseMemoryWriteRate != 0 || pollutionRate != 0 || stalePreferenceRate != 0 ||
		promptOverhead > 4_000 {
		return fmt.Errorf("personalization benchmark failed; see %s", path)
	}
	return nil
}

func entriesContainPreference(entries []memory.CuratedEntry, key, value string) bool {
	for _, entry := range entries {
		if entry.PreferenceKey == key && entry.PreferenceValue == value {
			return true
		}
	}
	return false
}

func entriesContainContent(entries []memory.CuratedEntry, value string) bool {
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Content), strings.ToLower(value)) {
			return true
		}
	}
	return false
}

func passedFraction(values []bool) float64 {
	if len(values) == 0 {
		return 0
	}
	passed := 0
	for _, value := range values {
		if value {
			passed++
		}
	}
	return float64(passed) / float64(len(values))
}

func profileHasValue(profile memory.UserProfileSnapshot, key, value string) bool {
	groups := [][]memory.UserProfileField{
		profile.Identity,
		profile.Communication,
		profile.Workflow,
		profile.Interaction,
		profile.Boundaries,
	}
	for _, group := range groups {
		for _, field := range group {
			if field.Key == key && field.Value == value {
				return true
			}
		}
	}
	return false
}
