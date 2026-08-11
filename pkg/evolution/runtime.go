package evolution

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/skills"
)

var ErrApplyDraftFailed = errors.New("apply draft failed")

type RuntimeOptions struct {
	Config              config.EvolutionConfig
	Now                 func() time.Time
	Store               *Store
	Organizer           *Organizer
	PatternClusterer    PatternClusterer
	SuccessJudge        SuccessJudge
	SkillsRecaller      *SkillsRecaller
	DraftGenerator      DraftGenerator
	GeneratorFactory    func(workspace string) DraftGenerator
	SuccessJudgeFactory func(workspace string) SuccessJudge
	Applier             *Applier
	ApplierFactory      func(workspace string) *Applier
}

type Runtime struct {
	cfg                 config.EvolutionConfig
	mu                  sync.Mutex
	now                 func() time.Time
	writer              *CaseWriter
	store               *Store
	organizer           *Organizer
	patternClusterer    PatternClusterer
	successJudge        SuccessJudge
	skillsRecaller      *SkillsRecaller
	draftGenerator      DraftGenerator
	generatorFactory    func(workspace string) DraftGenerator
	successJudgeFactory func(workspace string) SuccessJudge
	applier             *Applier
	applierFactory      func(workspace string) *Applier
}

type TurnCaseInput struct {
	Workspace             string
	WorkspaceID           string
	TurnID                string
	SessionKey            string
	AgentID               string
	Status                string
	UserMessage           string
	FinalContent          string
	ToolKinds             []string
	ToolExecutions        []ToolExecutionRecord
	ActiveSkillNames      []string
	AttemptedSkillNames   []string
	FinalSuccessfulPath   []string
	SkillContextSnapshots []SkillContextSnapshot
}

func NewRuntime(opts RuntimeOptions) (*Runtime, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	organizer := opts.Organizer
	if organizer == nil {
		organizer = NewOrganizer(OrganizerOptions{
			MinCaseCount:   opts.Config.EffectiveMinTaskCount(),
			MinSuccessRate: opts.Config.EffectiveMinSuccessRatio(),
			Now:            now,
		})
	}

	patternClusterer := opts.PatternClusterer
	if patternClusterer == nil {
		patternClusterer = NewHeuristicPatternClusterer(opts.Config.EffectiveMinTaskCount(), now)
	}

	return &Runtime{
		cfg:                 opts.Config,
		now:                 now,
		store:               opts.Store,
		organizer:           organizer,
		patternClusterer:    patternClusterer,
		successJudge:        opts.SuccessJudge,
		skillsRecaller:      opts.SkillsRecaller,
		draftGenerator:      opts.DraftGenerator,
		generatorFactory:    opts.GeneratorFactory,
		successJudgeFactory: opts.SuccessJudgeFactory,
		applier:             opts.Applier,
		applierFactory:      opts.ApplierFactory,
	}, nil
}

func (rt *Runtime) FinalizeTurn(ctx context.Context, input TurnCaseInput) error {
	if rt == nil || !rt.cfg.Enabled || input.Workspace == "" ||
		!strings.EqualFold(strings.TrimSpace(input.Status), "completed") ||
		shouldSkipLearningRecord(input) {
		return nil
	}
	var scrubFindings []string
	input, scrubFindings = scrubTurnCaseInput(input)
	if strings.TrimSpace(input.FinalContent) == "" {
		return nil
	}

	success := true
	usedSkillNames := buildUsedSkillNames(input)
	workspaceID := input.Workspace
	createdAt := rt.now()

	record := LearningRecord{
		ID:             buildTaskRecordID(input, createdAt),
		Kind:           RecordKindTask,
		WorkspaceID:    workspaceID,
		CreatedAt:      createdAt,
		SessionKey:     evolutionSessionReference(input.SessionKey),
		Summary:        buildRecordSummary(input),
		FinalOutput:    summarizeText(input.FinalContent, 1200),
		Status:         RecordStatus("new"),
		Success:        &success,
		UsedSkillNames: append([]string(nil), usedSkillNames...),
		Signals:        append([]string(nil), scrubFindings...),
	}

	paths := NewPaths(input.Workspace, rt.cfg.StateDir)

	rt.mu.Lock()
	if rt.writer == nil || rt.writer.paths.RootDir != paths.RootDir {
		rt.writer = NewCaseWriter(paths)
	}
	writer := rt.writer
	rt.mu.Unlock()

	if err := writer.AppendCase(ctx, record); err != nil {
		return err
	}

	if err := rt.recordSkillUsage(input, success); err != nil {
		return err
	}

	logger.DebugCF("evolution", "Recorded hot path learning record", map[string]any{
		"workspace_ref":    evolutionLogRef(input.Workspace),
		"success":          success,
		"used_skill_count": len(record.UsedSkillNames),
	})
	return nil
}

func evolutionSessionReference(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(sessionKey))
	return "session-" + hex.EncodeToString(digest[:8])
}

func buildTaskRecordID(input TurnCaseInput, createdAt time.Time) string {
	base := strings.TrimSpace(input.TurnID)
	if base == "" {
		base = "turn"
	}
	base = validSkillNameOrEmpty(base)
	if base == "" {
		base = "turn"
	}
	seed := strings.Join([]string{
		input.Workspace,
		input.SessionKey,
		input.AgentID,
		input.TurnID,
		createdAt.UTC().Format(time.RFC3339Nano),
	}, "\x00")
	sum := sha1.Sum([]byte(seed))
	return base + "-" + hex.EncodeToString(sum[:6])
}

func buildRecordSummary(input TurnCaseInput) string {
	if goal := summarizeText(input.UserMessage, 160); goal != "" {
		return goal
	}
	return fmt.Sprintf("turn %s finished with status=%s", input.TurnID, input.Status)
}

func summarizeText(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxLen <= 0 {
		return text
	}
	if utf8.RuneCountInString(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		runes := []rune(text)
		return string(runes[:maxLen])
	}
	runes := []rune(text)
	return string(runes[:maxLen-3]) + "..."
}

func limitEvolutionRecords(records []LearningRecord, maximum int) []LearningRecord {
	if maximum <= 0 || len(records) <= maximum {
		return records
	}
	ordered := append([]LearningRecord(nil), records...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.After(ordered[j].CreatedAt)
		}
		return ordered[i].ID < ordered[j].ID
	})
	ordered = ordered[:maximum]
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
		}
		return ordered[i].ID < ordered[j].ID
	})
	return ordered
}

func buildUsedSkillNames(input TurnCaseInput) []string {
	if final := uniqueTrimmedNames(input.FinalSuccessfulPath); len(final) > 0 {
		return final
	}
	out := make([]string, 0)
	for _, exec := range input.ToolExecutions {
		if !exec.Success {
			continue
		}
		out = append(out, exec.SkillNames...)
	}
	return uniqueTrimmedNames(out)
}

func shouldSkipLearningRecord(input TurnCaseInput) bool {
	if strings.EqualFold(strings.TrimSpace(input.SessionKey), "heartbeat") {
		return true
	}
	return false
}

func uniqueTrimmedNames(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (rt *Runtime) RunColdPathOnce(ctx context.Context, workspace string) error {
	if rt == nil || !rt.cfg.Enabled || workspace == "" {
		return nil
	}

	mode := rt.cfg.EffectiveMode()
	runID := fmt.Sprintf("%d", rt.now().UnixNano())
	if mode == "" || mode == "observe" {
		logger.DebugCF("evolution", "Skipped cold path run", map[string]any{
			"workspace_ref": evolutionLogRef(workspace),
			"mode":          mode,
			"run_ref":       evolutionLogRef(runID),
		})
		return nil
	}

	logger.InfoCF("evolution", "Started cold path run", map[string]any{
		"workspace_ref": evolutionLogRef(workspace),
		"mode":          mode,
		"run_ref":       evolutionLogRef(runID),
	})

	store := rt.storeForWorkspace(workspace)
	taskRecords, err := store.LoadTaskRecords()
	if err != nil {
		return err
	}
	taskRecords = filterEvolutionRecordsForWorkspace(taskRecords, workspace)
	taskRecords = limitEvolutionRecords(taskRecords, rt.cfg.EffectiveMaxEvidenceRecords())
	patternRecords, err := store.LoadPatternRecords()
	if err != nil {
		return err
	}
	patternRecords = filterEvolutionRecordsForWorkspace(patternRecords, workspace)
	logger.DebugCF("evolution", "Loaded evolution records", map[string]any{
		"workspace_ref": evolutionLogRef(workspace),
		"task_count":    len(taskRecords),
		"pattern_count": len(patternRecords),
		"run_ref":       evolutionLogRef(runID),
	})

	admittedCount := 0
	newRuleCount := 0
	if rt.patternClusterer != nil {
		recordsForOrganizer, evidenceRecordsForOrganizer, inputErr := rt.recordsForColdPathInputs(
			ctx,
			workspace,
			taskRecords,
		)
		if inputErr != nil {
			return inputErr
		}
		recordsForOrganizer = rt.filterRecordsByMinSuccessRatio(
			workspace,
			evidenceRecordsForOrganizer,
			recordsForOrganizer,
		)
		admittedCount = countTaskLearningRecords(recordsForOrganizer)
		logger.DebugCF("evolution", "Admitted task records for cold path", map[string]any{
			"workspace_ref":   evolutionLogRef(workspace),
			"admitted_tasks":  admittedCount,
			"organizer_input": len(recordsForOrganizer),
			"run_ref":         evolutionLogRef(runID),
		})
		var rules []LearningRecord
		var clusteredTaskIDs []string
		if clusterer, ok := rt.patternClusterer.(evidencePatternClusterer); ok {
			rules, clusteredTaskIDs, err = clusterer.BuildPatternsWithEvidence(
				ctx,
				workspace,
				recordsForOrganizer,
				evidenceRecordsForOrganizer,
				patternRecords,
				rt.cfg.EffectiveMinSuccessRatio(),
			)
		} else {
			rules, clusteredTaskIDs, err = rt.patternClusterer.BuildPatterns(
				ctx,
				workspace,
				recordsForOrganizer,
				patternRecords,
			)
		}
		if err != nil {
			return err
		}
		newRuleCount = countNewPatterns(patternRecords, rules, workspace)
		logger.DebugCF("evolution", "Built learning patterns", map[string]any{
			"workspace_ref":  evolutionLogRef(workspace),
			"pattern_count":  len(rules),
			"new_patterns":   newRuleCount,
			"admitted_tasks": admittedCount,
			"run_ref":        evolutionLogRef(runID),
		})
		if len(rules) > 0 {
			merged := mergePatternRecords(patternRecords, rules, workspace)
			if mergeErr := store.MergePatternRecords(rules); mergeErr != nil {
				return mergeErr
			}
			patternRecords = merged
		}
		if len(clusteredTaskIDs) > 0 {
			if markErr := markTaskRecordsClustered(store, clusteredTaskIDs); markErr != nil {
				return markErr
			}
		}
	}

	generator := rt.draftGeneratorForWorkspace(workspace)
	if generator == nil {
		logger.DebugCF("evolution", "Skipped drafting because no draft generator is available", map[string]any{
			"workspace_ref": evolutionLogRef(workspace),
			"run_ref":       evolutionLogRef(runID),
		})
		return rt.runLifecycleMaintenance(workspace, store, runID)
	}

	recaller := rt.skillsRecallerForWorkspace(workspace)
	applier := rt.applierForWorkspace(workspace)
	readyRules := filterReadyRules(patternRecords, workspace)
	readyRules = enrichReadyRulesForDrafts(readyRules, taskRecords)
	if len(readyRules) == 0 {
		logger.DebugCF("evolution", "Finished cold path run without ready patterns", map[string]any{
			"workspace_ref":  evolutionLogRef(workspace),
			"record_count":   len(taskRecords),
			"new_patterns":   newRuleCount,
			"admitted_tasks": admittedCount,
			"run_ref":        evolutionLogRef(runID),
		})
		return rt.runLifecycleMaintenance(workspace, store, runID)
	}

	existingDrafts, err := store.LoadDrafts()
	if err != nil {
		return err
	}
	readyRuleByID := make(map[string]LearningRecord, len(readyRules))
	for _, rule := range readyRules {
		readyRuleByID[rule.ID] = rule
	}
	appliedExistingDrafts := 0
	changedExistingDrafts := false
	for _, draft := range existingDrafts {
		if draft.WorkspaceID != workspace || draft.Status != DraftStatusCandidate {
			continue
		}
		rule, ok := readyRuleByID[draft.SourceRecordID]
		if !ok {
			logger.DebugCF(
				"evolution",
				"Skipped existing candidate draft because its source pattern is not ready",
				map[string]any{
					"workspace_ref": evolutionLogRef(workspace),
					"status":        string(draft.Status),
					"run_ref":       evolutionLogRef(runID),
				},
			)
			continue
		}
		matches, recallErr := recaller.RecallSimilarSkills(rule)
		if recallErr != nil {
			return recallErr
		}
		draft.MatchedSkillRefs = collectSkillRefs(matches)
		var normalizationNotes []string
		evidence := draftEvidenceForRule(rule, taskRecords)
		draft = withDraftEvidenceMetrics(draft, evidence)
		draft, normalizationNotes = rt.normalizeDraftForWorkspace(workspace, rule, matches, evidence, draft)
		review := ReviewDraftWithPolicy(draft, rt.cfg.EffectiveMaxDraftChars())
		draft.Status = review.Status
		draft.ReviewNotes = appendUniqueStrings(draft.ReviewNotes, append(review.ReviewNotes, normalizationNotes...)...)
		draft.ScanFindings = appendUniqueStrings(draft.ScanFindings, review.Findings...)
		changedExistingDrafts = true
		if draft.Status != DraftStatusCandidate || !rt.cfg.AutoAppliesDrafts() || applier == nil ||
			!rt.draftMeetsEvidenceThresholds(draft) {
			if saveErr := store.SaveDrafts([]SkillDraft{draft}); saveErr != nil {
				return saveErr
			}
			continue
		}
		updatedDraft, applyErr := rt.applyCandidateDraft(ctx, workspace, store, applier, draft, runID)
		if applyErr != nil {
			return applyErr
		}
		if updatedDraft.Status == DraftStatusAccepted {
			appliedExistingDrafts++
			changedExistingDrafts = true
		}
	}
	if changedExistingDrafts {
		existingDrafts, err = store.LoadDrafts()
		if err != nil {
			return err
		}
	}
	existingBySource := existingDraftSourceSet(existingDrafts, workspace)
	logger.DebugCF("evolution", "Selected ready patterns for drafting", map[string]any{
		"workspace_ref":        evolutionLogRef(workspace),
		"ready_patterns":       len(readyRules),
		"existing_draft_count": len(existingBySource),
		"applied_existing":     appliedExistingDrafts,
		"run_ref":              evolutionLogRef(runID),
	})

	processedRules := 0
	for _, rule := range readyRules {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if _, exists := existingBySource[rule.ID]; exists {
			logger.DebugCF(
				"evolution",
				"Skipped pattern because a non-quarantined draft already exists",
				map[string]any{
					"workspace_ref": evolutionLogRef(workspace),
					"run_ref":       evolutionLogRef(runID),
				},
			)
			continue
		}

		evidence := draftEvidenceForRule(rule, taskRecords)
		rule = enrichRuleWithDraftEvidence(rule, evidence)
		matches, err := recaller.RecallSimilarSkills(rule)
		if err != nil {
			return err
		}
		logger.DebugCF("evolution", "Generating skill draft", map[string]any{
			"workspace_ref":       evolutionLogRef(workspace),
			"matched_skill_count": len(matches),
			"run_ref":             evolutionLogRef(runID),
		})

		draftCtx, cancelDraft := context.WithTimeout(
			ctx,
			time.Duration(rt.cfg.EffectiveDraftTimeoutSeconds())*time.Second,
		)
		draft, err := generateDraftWithEvidence(draftCtx, generator, rule, matches, evidence)
		cancelDraft()
		if err != nil {
			return err
		}

		draft = rt.finalizeDraft(workspace, rule, matches, evidence, draft)
		draftSaved := false
		logger.DebugCF("evolution", "Finalized skill draft", map[string]any{
			"workspace_ref": evolutionLogRef(workspace),
			"change_kind":   string(draft.ChangeKind),
			"status":        string(draft.Status),
			"run_ref":       evolutionLogRef(runID),
		})
		if rt.cfg.AutoAppliesDrafts() && applier != nil && draft.Status == DraftStatusCandidate &&
			rt.draftMeetsEvidenceThresholds(draft) {
			var err error
			draft, err = rt.applyCandidateDraft(ctx, workspace, store, applier, draft, runID)
			if err != nil {
				return err
			}
			draftSaved = true
		}

		if !draftSaved {
			if err := store.SaveDrafts([]SkillDraft{draft}); err != nil {
				return err
			}
		}
		logger.DebugCF("evolution", "Saved skill draft", map[string]any{
			"workspace_ref": evolutionLogRef(workspace),
			"status":        string(draft.Status),
			"run_ref":       evolutionLogRef(runID),
		})
		existingBySource[rule.ID] = struct{}{}
		processedRules++
	}

	logger.InfoCF("evolution", "Finished cold path run", map[string]any{
		"workspace_ref":      evolutionLogRef(workspace),
		"ready_patterns":     len(readyRules),
		"processed_patterns": processedRules,
		"new_patterns":       newRuleCount,
		"run_ref":            evolutionLogRef(runID),
	})
	return rt.runLifecycleMaintenance(workspace, store, runID)
}

func filterEvolutionRecordsForWorkspace(records []LearningRecord, workspace string) []LearningRecord {
	out := make([]LearningRecord, 0, len(records))
	for _, record := range records {
		if record.WorkspaceID == workspace {
			out = append(out, record)
		}
	}
	return out
}

func (rt *Runtime) recordsForColdPathInputs(
	ctx context.Context,
	workspace string,
	records []LearningRecord,
) ([]LearningRecord, []LearningRecord, error) {
	admitted := make([]LearningRecord, 0, len(records))
	evidence := make([]LearningRecord, 0, len(records))
	judge := rt.successJudgeForWorkspace(workspace)

	for _, record := range records {
		if !isTaskRecordKind(record.Kind) || record.WorkspaceID != workspace {
			continue
		}
		if reason := coldPathEvidenceRejectReason(record); reason != "" {
			logger.DebugCF("evolution", "Rejected task record for cold path", map[string]any{
				"workspace_ref": evolutionLogRef(workspace),
				"reason_class":  reason,
			})
			continue
		}

		evidenceRecord := record
		if record.Success != nil && *record.Success && judge != nil {
			decision, err := judge.JudgeTaskRecord(ctx, record)
			if err != nil {
				return nil, nil, err
			}
			judgedSuccess := decision.Success
			evidenceRecord.Success = &judgedSuccess
			if !decision.Success {
				logger.DebugCF("evolution", "Rejected task record by success judge", map[string]any{
					"workspace_ref": evolutionLogRef(workspace),
					"reason_class":  "judge_rejected",
				})
			}
		}
		evidence = append(evidence, evidenceRecord)
		if evidenceRecord.Success == nil || !*evidenceRecord.Success {
			continue
		}
		admitted = append(admitted, evidenceRecord)
	}
	return admitted, evidence, nil
}

func (rt *Runtime) filterRecordsByMinSuccessRatio(
	workspace string,
	allRecords []LearningRecord,
	admittedRecords []LearningRecord,
) []LearningRecord {
	minRatio := rt.cfg.EffectiveMinSuccessRatio()
	if minRatio <= 0 {
		return admittedRecords
	}

	type successStats struct {
		success int
		total   int
	}
	statsByKey := make(map[string]successStats)
	for _, record := range allRecords {
		key, ok := coldPathSuccessRatioKey(workspace, record)
		if !ok {
			continue
		}
		stats := statsByKey[key]
		stats.total++
		if record.Success != nil && *record.Success {
			stats.success++
		}
		statsByKey[key] = stats
	}

	out := make([]LearningRecord, 0, len(admittedRecords))
	for _, record := range admittedRecords {
		if !isTaskRecordKind(record.Kind) {
			out = append(out, record)
			continue
		}
		key, ok := coldPathSuccessRatioKey(workspace, record)
		if !ok {
			continue
		}
		stats := statsByKey[key]
		if stats.total == 0 {
			continue
		}
		ratio := float64(stats.success) / float64(stats.total)
		if ratio < minRatio {
			logger.DebugCF("evolution", "Rejected task record below cold path success ratio", map[string]any{
				"workspace_ref":     evolutionLogRef(workspace),
				"success_ratio":     ratio,
				"min_success_ratio": minRatio,
				"success_count":     stats.success,
				"total_count":       stats.total,
			})
			continue
		}
		out = append(out, record)
	}
	return out
}

func coldPathSuccessRatioKey(workspace string, record LearningRecord) (string, bool) {
	if !isTaskRecordKind(record.Kind) || record.WorkspaceID != workspace {
		return "", false
	}
	if record.Status != "" && record.Status != RecordStatus("new") {
		return "", false
	}
	if strings.EqualFold(strings.TrimSpace(record.SessionKey), "heartbeat") {
		return "", false
	}
	if strings.EqualFold(strings.TrimSpace(record.FinalOutput), "HEARTBEAT_OK") {
		return "", false
	}
	if strings.TrimSpace(record.Summary) == "" {
		return "", false
	}
	key := heuristicClusterKey(record)
	if key == "" {
		return "", false
	}
	return key, true
}

func coldPathEvidenceRejectReason(record LearningRecord) string {
	if !isTaskRecordKind(record.Kind) {
		return "not a task record"
	}
	if record.Success == nil {
		return "task success unknown"
	}
	if record.Status != "" && record.Status != RecordStatus("new") {
		return "task already processed"
	}
	if strings.EqualFold(strings.TrimSpace(record.SessionKey), "heartbeat") {
		return "heartbeat session"
	}
	if strings.EqualFold(strings.TrimSpace(record.FinalOutput), "HEARTBEAT_OK") {
		return "heartbeat output"
	}
	if strings.TrimSpace(record.Summary) == "" {
		return "missing summary"
	}
	if strings.TrimSpace(record.FinalOutput) == "" {
		return "missing final output"
	}
	return ""
}

func (rt *Runtime) storeForWorkspace(workspace string) *Store {
	paths := NewPaths(workspace, rt.cfg.StateDir)
	if rt.store != nil && rt.store.paths.RootDir == paths.RootDir && rt.store.paths.Workspace == paths.Workspace {
		return rt.store
	}
	return NewStore(paths)
}

func (rt *Runtime) skillsRecallerForWorkspace(workspace string) *SkillsRecaller {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if rt.skillsRecaller == nil || rt.skillsRecaller.workspace != workspace {
		rt.skillsRecaller = NewSkillsRecaller(workspace)
	}
	return rt.skillsRecaller
}

func (rt *Runtime) draftGeneratorForWorkspace(workspace string) DraftGenerator {
	if rt.generatorFactory != nil {
		if generator := rt.generatorFactory(workspace); generator != nil {
			return generator
		}
	}
	if rt.draftGenerator != nil {
		return rt.draftGenerator
	}
	return NewDefaultDraftGenerator(workspace)
}

func (rt *Runtime) successJudgeForWorkspace(workspace string) SuccessJudge {
	if rt.successJudgeFactory != nil {
		if judge := rt.successJudgeFactory(workspace); judge != nil {
			return judge
		}
	}
	if rt.successJudge != nil {
		return rt.successJudge
	}
	return &HeuristicSuccessJudge{}
}

func (rt *Runtime) applierForWorkspace(workspace string) *Applier {
	if rt.applierFactory != nil {
		if applier := rt.applierFactory(workspace); applier != nil {
			return applier
		}
	}
	return rt.applier
}

func (rt *Runtime) finalizeDraft(
	workspace string,
	rule LearningRecord,
	matches []skills.SkillInfo,
	evidence DraftEvidence,
	draft SkillDraft,
) SkillDraft {
	if draft.ID == "" {
		draft.ID = "draft-" + rule.ID
	}
	if draft.CreatedAt.IsZero() {
		draft.CreatedAt = rt.now()
	}
	draft.WorkspaceID = workspace
	draft.SourceRecordID = rule.ID
	draft.MatchedSkillRefs = collectSkillRefs(matches)
	draft = withDraftEvidenceMetrics(draft, evidence)

	draft, normalizationNotes := rt.normalizeDraftForWorkspace(workspace, rule, matches, evidence, draft)
	review := ReviewDraftWithPolicy(draft, rt.cfg.EffectiveMaxDraftChars())
	draft.Status = review.Status
	draft.ReviewNotes = append([]string(nil), review.ReviewNotes...)
	draft.ReviewNotes = append(draft.ReviewNotes, normalizationNotes...)
	if len(review.Findings) == 0 {
		draft.ScanFindings = nil
		return draft
	}
	draft.ScanFindings = append([]string(nil), review.Findings...)
	return draft
}

func withDraftEvidenceMetrics(draft SkillDraft, evidence DraftEvidence) SkillDraft {
	draft.EvidenceCount = len(evidence.TaskRecords)
	draft.SuccessRatio = 0
	if draft.EvidenceCount == 0 {
		return draft
	}
	successes := 0
	for _, record := range evidence.TaskRecords {
		if record.Success != nil && *record.Success {
			successes++
		}
	}
	draft.SuccessRatio = float64(successes) / float64(draft.EvidenceCount)
	return draft
}

func (rt *Runtime) draftMeetsEvidenceThresholds(draft SkillDraft) bool {
	return draft.EvidenceCount >= rt.cfg.EffectiveMinTaskCount() &&
		draft.SuccessRatio >= rt.cfg.EffectiveMinSuccessRatio()
}

func (rt *Runtime) normalizeDraftForWorkspace(
	workspace string,
	rule LearningRecord,
	matches []skills.SkillInfo,
	evidence DraftEvidence,
	draft SkillDraft,
) (SkillDraft, []string) {
	target := strings.TrimSpace(draft.TargetSkillName)
	if workspace == "" || target == "" {
		return draft, nil
	}
	if validateEvolutionSkillTarget(target) != nil {
		return draft, []string{"rejected unsafe target before workspace path resolution"}
	}

	notes := make([]string, 0, 4)
	if combinedTarget := inferCombinedSkillName(rule); combinedTarget != "" && combinedTarget != target {
		originalTarget := target
		draft.TargetSkillName = combinedTarget
		target = combinedTarget
		notes = append(notes, fmt.Sprintf(
			"retargeted draft from %q to combined shortcut skill %q because the winning path was a stable multi-skill chain",
			originalTarget,
			combinedTarget,
		))
	}

	skillPath := filepath.Join(workspace, "skills", target, "SKILL.md")
	_, err := os.Stat(skillPath)
	hasExisting := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return draft, notes
	}

	if combinedTarget := inferCombinedSkillName(rule); combinedTarget != "" && combinedTarget == target {
		draft.HumanSummary = buildCombinedSkillHumanSummary(target, rule, hasExisting)
		draft.PreferredEntryPath = []string{target}
		draft.AvoidPatterns = appendUniqueStrings(
			draft.AvoidPatterns,
			buildCombinedSkillAvoidPattern(target, rule),
		)
		if hasExisting {
			draft.ChangeKind = ChangeKindAppend
			draft.BodyOrPatch = synthesizeCombinedSkillAppendBody(target, draft, rule, matches, evidence)
			notes = append(notes, "normalized combined shortcut draft to append onto the existing combined skill")
		} else {
			draft.ChangeKind = ChangeKindCreate
			draft.BodyOrPatch = synthesizeCombinedSkillDocument(target, draft, rule, matches, evidence)
			notes = append(notes, "normalized combined shortcut draft to create a new standalone shortcut skill")
		}
		return draft, notes
	}

	if !hasExisting {
		switch draft.ChangeKind {
		case ChangeKindAppend, ChangeKindMerge, ChangeKindReplace:
			draft.ChangeKind = ChangeKindCreate
			notes = append(notes, "normalized change_kind to create because target skill did not exist")
			if !looksLikeSkillDocument(draft.BodyOrPatch) {
				draft.BodyOrPatch = synthesizeSkillDocumentFromPartialDraft(target, draft, rule, evidence)
				notes = append(notes, "synthesized full skill document because draft body was partial")
			}
		}
		return draft, notes
	}

	if draft.ChangeKind == ChangeKindCreate && !looksLikeSkillDocument(draft.BodyOrPatch) {
		draft.ChangeKind = ChangeKindAppend
		notes = append(notes, "normalized change_kind to append because target skill already existed")
	}
	return draft, notes
}

func looksLikeSkillDocument(body string) bool {
	body = strings.TrimSpace(body)
	return strings.HasPrefix(body, "---\n") && strings.Contains(body, "\n# ")
}

func synthesizeSkillDocumentFromPartialDraft(
	target string,
	draft SkillDraft,
	rule LearningRecord,
	evidence DraftEvidence,
) string {
	description := strings.TrimSpace(draft.HumanSummary)
	if description == "" {
		description = fmt.Sprintf("Learned workflow for %s.", target)
	}

	bodyContent := strings.TrimSpace(draft.BodyOrPatch)
	if bodyContent == "" {
		bodyContent = "No learned content was generated."
	}
	if strings.HasPrefix(bodyContent, "# ") {
		return buildSkillDocument(target, description, bodyContent)
	}

	body := strings.Join([]string{
		"# " + titleCaseSkillName(target),
		"",
		"## Start Here",
		synthesizedStartHereLine(rule, target),
		"",
		"## Learned Evolution",
		bodyContent,
		"",
		"## Expected Result",
		synthesizedExpectedResultLine(evidence),
		"",
		"## Source Evidence",
		synthesizedEvidenceLine(rule, evidence),
		"",
	}, "\n")
	return buildSkillDocument(target, description, body)
}

func synthesizeCombinedSkillDocument(
	target string,
	draft SkillDraft,
	rule LearningRecord,
	matches []skills.SkillInfo,
	evidence DraftEvidence,
) string {
	description := strings.TrimSpace(draft.HumanSummary)
	if description == "" {
		description = buildCombinedSkillHumanSummary(target, rule, false)
	}

	body := strings.Join([]string{
		"# " + titleCaseSkillName(target),
		"",
		"## When To Use",
		synthesizedCombinedWhenToUseLine(rule, target),
		"",
		"## Procedure",
		synthesizedCombinedStartHereLine(rule, target),
		synthesizedCombinedProcedure(matches, rule),
		"",
		"## Source Skills",
		synthesizedComponentBreakdown(matches),
		"",
		"## Learned Context",
		synthesizedCombinedLearnedContent(draft.BodyOrPatch, rule),
		"",
		"## Expected Result",
		synthesizedExpectedResultLine(evidence),
		"",
		"## Source Evidence",
		synthesizedEvidenceLine(rule, evidence),
		"",
	}, "\n")
	return buildSkillDocument(target, description, body)
}

func synthesizeCombinedSkillAppendBody(
	target string,
	draft SkillDraft,
	rule LearningRecord,
	matches []skills.SkillInfo,
	evidence DraftEvidence,
) string {
	lines := []string{
		"## Learned Shortcut Update",
		fmt.Sprintf("- Shortcut skill: `%s`", target),
		fmt.Sprintf("- Task summary: %s", fallbackEvolutionSummary(rule)),
		fmt.Sprintf("- Wrapped path: %s", synthesizedWrappedPathLine(rule)),
		"- Guidance: prefer this shortcut directly instead of replaying the whole path when the task matches.",
		fmt.Sprintf("- Expected result: %s", synthesizedExpectedResultLine(evidence)),
		fmt.Sprintf("- Evidence: %s", synthesizedEvidenceLine(rule, evidence)),
		"",
		"### Source Skills",
		synthesizedComponentBreakdown(matches),
		"",
		synthesizedCombinedLearnedContent(draft.BodyOrPatch, rule),
		"",
	}
	return strings.Join(lines, "\n")
}

func synthesizedStartHereLine(rule LearningRecord, target string) string {
	if len(rule.WinningPath) > 0 {
		return fmt.Sprintf(
			"Start with `%s` for tasks like `%s`.",
			strings.Join(rule.WinningPath, " -> "),
			strings.TrimSpace(rule.Summary),
		)
	}
	if summary := strings.TrimSpace(rule.Summary); summary != "" {
		return fmt.Sprintf("Use `%s` when the task matches `%s`.", target, summary)
	}
	return fmt.Sprintf("Use `%s` for the learned task pattern.", target)
}

func synthesizedCombinedStartHereLine(rule LearningRecord, target string) string {
	return fmt.Sprintf("Use `%s` directly when the task matches `%s`.", target, fallbackEvolutionSummary(rule))
}

func synthesizedCombinedWhenToUseLine(rule LearningRecord, target string) string {
	if len(rule.WinningPath) == 0 {
		return fmt.Sprintf("Use `%s` when the learned task pattern appears again.", target)
	}
	return fmt.Sprintf(
		"Use `%s` as a direct shortcut instead of replaying `%s` step by step.",
		target,
		strings.Join(rule.WinningPath, " -> "),
	)
}

func synthesizedCombinedProcedure(matches []skills.SkillInfo, rule LearningRecord) string {
	components := synthesizedComponentBreakdown(matches)
	if !strings.HasPrefix(strings.TrimSpace(components), "- `") {
		if len(rule.WinningPath) == 0 {
			return "Use the learned shortcut directly and keep the response focused on the requested result."
		}
		return fmt.Sprintf(
			"Apply the recorded path `%s`, then return the final result with only the necessary explanation.",
			strings.Join(rule.WinningPath, " -> "),
		)
	}
	return "Follow the source skill guidance below as one compact procedure, then return the final result without replaying unnecessary discovery steps."
}

func synthesizedExpectedResultLine(evidence DraftEvidence) string {
	if excerpt := firstFinalOutputExcerpt(evidence, 360); excerpt != "" {
		return excerpt
	}
	return "Return the completed result for the matched task without restating unrelated discovery steps."
}

func synthesizedEvidenceLine(rule LearningRecord, evidence DraftEvidence) string {
	if len(evidence.TaskRecords) > 0 {
		ids := make([]string, 0, len(evidence.TaskRecords))
		for _, task := range evidence.TaskRecords {
			if id := strings.TrimSpace(task.ID); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			return "learned from task records: " + strings.Join(ids, ", ")
		}
	}
	if len(rule.TaskRecordIDs) > 0 {
		return "learned from task records: " + strings.Join(rule.TaskRecordIDs, ", ")
	}
	return "learned from the pattern record."
}

func synthesizedWrappedPathLine(rule LearningRecord) string {
	if len(rule.WinningPath) == 0 {
		return "No explicit wrapped path was recorded."
	}
	return strings.Join(rule.WinningPath, " -> ")
}

func synthesizedCombinedLearnedContent(body string, rule LearningRecord) string {
	content := strings.TrimSpace(stripSkillFrontmatter(body))
	if content == "" {
		return fmt.Sprintf(
			"Learned from `%s`; use this shortcut directly when the same task pattern appears again.",
			fallbackEvolutionSummary(rule),
		)
	}
	content = removeVerboseCombinedSections(content)
	content = strings.Join(strings.Fields(content), " ")
	if content == "" {
		return fmt.Sprintf(
			"Learned from `%s`; use this shortcut directly when the same task pattern appears again.",
			fallbackEvolutionSummary(rule),
		)
	}
	content = trimAtReadableBoundary(content, 1200)
	return "- Learned task: " + fallbackEvolutionSummary(rule) + "\n- Reusable guidance: " + content
}

func stripSkillFrontmatter(body string) string {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "---\n") {
		return trimmed
	}
	rest := strings.TrimPrefix(trimmed, "---\n")
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return trimmed
	}
	return strings.TrimSpace(rest[end+5:])
}

func removeVerboseCombinedSections(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			normalized := strings.ToLower(title)
			switch normalized {
			case "component skill breakdown", "source skills", "wrapped path", "start here", "when to use", "procedure":
				skip = true
				continue
			default:
				skip = false
			}
		}
		if skip {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func fallbackEvolutionSummary(rule LearningRecord) string {
	if summary := strings.TrimSpace(rule.Summary); summary != "" {
		return summary
	}
	if len(rule.WinningPath) > 0 {
		return strings.Join(rule.WinningPath, " -> ")
	}
	return "the learned task pattern"
}

func buildCombinedSkillHumanSummary(target string, rule LearningRecord, hasExisting bool) string {
	_ = hasExisting
	summary := fallbackEvolutionSummary(rule)
	if strings.TrimSpace(summary) == "" || summary == "the learned task pattern" {
		summary = titleCaseSkillName(target)
	}
	return fmt.Sprintf("Use this skill to %s when the task requires this workflow.", sentenceFragment(summary))
}

func buildCombinedSkillAvoidPattern(target string, rule LearningRecord) string {
	if len(rule.WinningPath) == 0 {
		return fmt.Sprintf("avoid bypassing `%s` when the same learned task pattern appears again", target)
	}
	return fmt.Sprintf("avoid replaying %s before trying `%s` directly", strings.Join(rule.WinningPath, " -> "), target)
}

func collectSkillRefs(matches []skills.SkillInfo) []string {
	if len(matches) == 0 {
		return nil
	}

	refs := make([]string, 0, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(match.Name)
		if name == "" || validateEvolutionSkillTarget(name) != nil {
			continue
		}
		refs = append(refs, name)
	}
	return uniqueTrimmedNames(refs)
}

func countTaskLearningRecords(records []LearningRecord) int {
	count := 0
	for _, record := range records {
		if isTaskRecordKind(record.Kind) {
			count++
		}
	}
	return count
}

func (rt *Runtime) runLifecycleMaintenance(workspace string, store *Store, runID string) error {
	if rt == nil || store == nil || workspace == "" {
		return nil
	}

	paths := NewPaths(workspace, rt.cfg.StateDir)
	logger.DebugCF("evolution", "Started lifecycle maintenance", map[string]any{
		"workspace_ref": evolutionLogRef(workspace),
		"run_ref":       evolutionLogRef(runID),
	})

	summary, err := RunLifecycleOnce(store, paths, workspace, rt.now())
	if err != nil {
		logger.WarnCF("evolution", "Lifecycle maintenance failed", map[string]any{
			"workspace_ref": evolutionLogRef(workspace),
			"run_ref":       evolutionLogRef(runID),
			"error_class":   "lifecycle_maintenance_failed",
		})
		return err
	}

	logger.DebugCF("evolution", "Finished lifecycle maintenance", map[string]any{
		"workspace_ref":         evolutionLogRef(workspace),
		"run_ref":               evolutionLogRef(runID),
		"evaluated_profiles":    summary.EvaluatedProfiles,
		"transitioned_profiles": summary.TransitionedProfiles,
		"deleted_skills":        summary.DeletedSkills,
	})
	return nil
}

func enrichReadyRulesForDrafts(rules, taskRecords []LearningRecord) []LearningRecord {
	if len(rules) == 0 || len(taskRecords) == 0 {
		return rules
	}
	out := make([]LearningRecord, 0, len(rules))
	for _, rule := range rules {
		evidence := draftEvidenceForRule(rule, taskRecords)
		out = append(out, enrichRuleWithDraftEvidence(rule, evidence))
	}
	return out
}

func draftEvidenceForRule(rule LearningRecord, taskRecords []LearningRecord) DraftEvidence {
	if len(rule.TaskRecordIDs) == 0 || len(taskRecords) == 0 {
		return DraftEvidence{}
	}
	idSet := make(map[string]struct{}, len(rule.TaskRecordIDs))
	for _, id := range rule.TaskRecordIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		idSet[id] = struct{}{}
	}
	if len(idSet) == 0 {
		return DraftEvidence{}
	}
	tasks := make([]LearningRecord, 0, len(idSet))
	for _, task := range taskRecords {
		if rule.WorkspaceID != "" && task.WorkspaceID != rule.WorkspaceID {
			continue
		}
		if _, ok := idSet[task.ID]; !ok {
			continue
		}
		tasks = append(tasks, task)
	}
	return DraftEvidence{TaskRecords: tasks}
}

func generateDraftWithEvidence(
	ctx context.Context,
	generator DraftGenerator,
	rule LearningRecord,
	matches []skills.SkillInfo,
	evidence DraftEvidence,
) (SkillDraft, error) {
	if generator == nil {
		return SkillDraft{}, nil
	}
	if evidenceAware, ok := generator.(EvidenceAwareDraftGenerator); ok {
		return evidenceAware.GenerateDraftWithEvidence(ctx, rule, matches, evidence)
	}
	return generator.GenerateDraft(ctx, rule, matches)
}

func countNewPatterns(existing, patterns []LearningRecord, workspace string) int {
	existingIDs := make(map[string]struct{}, len(existing))
	for _, pattern := range existing {
		if !isPatternRecordKind(pattern.Kind) || pattern.WorkspaceID != workspace {
			continue
		}
		existingIDs[pattern.ID] = struct{}{}
	}
	count := 0
	for _, pattern := range patterns {
		if pattern.WorkspaceID != workspace {
			continue
		}
		if _, ok := existingIDs[pattern.ID]; ok {
			continue
		}
		count++
	}
	return count
}

func mergePatternRecords(existing, updates []LearningRecord, workspace string) []LearningRecord {
	out := append([]LearningRecord(nil), existing...)
	indexByID := make(map[string]int, len(out))
	for i, pattern := range out {
		indexByID[pattern.ID] = i
	}
	for _, update := range updates {
		if update.WorkspaceID != workspace {
			continue
		}
		if idx, ok := indexByID[update.ID]; ok {
			out[idx] = update
			continue
		}
		indexByID[update.ID] = len(out)
		out = append(out, update)
	}
	return out
}

func markTaskRecordsClustered(store *Store, ids []string) error {
	if store == nil || len(ids) == 0 {
		return nil
	}
	return store.MarkTaskRecordsClustered(ids)
}

func filterReadyRules(records []LearningRecord, workspace string) []LearningRecord {
	seen := make(map[string]LearningRecord)
	for _, record := range records {
		if !isPatternRecordKind(record.Kind) || record.WorkspaceID != workspace ||
			record.Status != RecordStatus("ready") {
			continue
		}
		seen[record.ID] = record
	}

	out := make([]LearningRecord, 0, len(seen))
	for _, record := range seen {
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func existingDraftSourceSet(drafts []SkillDraft, workspace string) map[string]struct{} {
	out := make(map[string]struct{}, len(drafts))
	for _, draft := range drafts {
		if draft.WorkspaceID != workspace || draft.SourceRecordID == "" {
			continue
		}
		if draft.Status == DraftStatusQuarantined {
			continue
		}
		out[draft.SourceRecordID] = struct{}{}
	}
	return out
}

func (rt *Runtime) saveAppliedProfile(store *Store, workspace string, draft SkillDraft) error {
	now := rt.now()

	return SaveAppliedProfile(
		store,
		workspace,
		draft,
		now,
		rt.cfg.EffectiveRollbackRetention()*4,
	)
}

func (rt *Runtime) applyCandidateDraft(
	ctx context.Context,
	workspace string,
	store *Store,
	applier *Applier,
	draft SkillDraft,
	runID string,
) (SkillDraft, error) {
	review := ReviewDraftWithPolicy(draft, rt.cfg.EffectiveMaxDraftChars())
	if review.Status == DraftStatusQuarantined {
		draft.Status = DraftStatusQuarantined
		draft.ScanFindings = appendUniqueStrings(draft.ScanFindings, review.Findings...)
		if saveErr := store.SaveDrafts([]SkillDraft{draft}); saveErr != nil {
			return draft, errorsJoin(fmt.Errorf("unsafe draft was quarantined"), saveErr)
		}
		return draft, fmt.Errorf("unsafe draft was quarantined")
	}
	if !rt.draftMeetsEvidenceThresholds(draft) {
		return draft, fmt.Errorf("draft evidence does not meet configured thresholds")
	}
	if draft.Status == DraftStatusCandidate && rt.cfg.AutoAppliesDrafts() {
		now := rt.now()
		draft.Status = DraftStatusApproved
		draft.ApprovedAt = &now
		draft.DecisionSource = "automatic_policy"
		draft.UpdatedAt = &now
	} else if draft.Status != DraftStatusApproved {
		return draft, fmt.Errorf("draft must be approved before apply")
	}
	logger.InfoCF("evolution", "Applying skill draft", map[string]any{
		"workspace_ref": evolutionLogRef(workspace),
		"change_kind":   string(draft.ChangeKind),
		"run_ref":       evolutionLogRef(runID),
	})
	currentBody, hadCurrent, currentErr := loadCurrentSkillBody(workspace, draft.TargetSkillName)
	if currentErr != nil {
		return quarantinePreApplyFailure(store, draft, currentErr)
	}
	previousProfile, profileErr := store.LoadProfile(draft.TargetSkillName)
	profileExisted := false
	if profileErr == nil {
		if previousProfile.WorkspaceID != workspace {
			return quarantinePreApplyFailure(
				store,
				draft,
				fmt.Errorf("skill profile scope mismatch"),
			)
		}
		profileExisted = true
	} else if !errors.Is(profileErr, os.ErrNotExist) {
		return quarantinePreApplyFailure(store, draft, profileErr)
	}
	previousVersion := "pre-" + draft.ID
	draft.PreviousVersion = previousVersion
	if err := store.SaveSkillVersion(SkillVersionSnapshot{
		Version: previousVersion, SkillName: draft.TargetSkillName,
		Workspace: workspace, Body: currentBody, Present: hadCurrent, CreatedAt: rt.now(),
	}); err != nil {
		return quarantinePreApplyFailure(store, draft, err)
	}
	rollbackApply, err := applier.applyDraftWithRollback(ctx, workspace, draft)
	if err != nil {
		logger.WarnCF("evolution", "Skill draft apply failed", map[string]any{
			"workspace_ref": evolutionLogRef(workspace),
			"error_class":   "apply_failed",
			"run_ref":       evolutionLogRef(runID),
		})
		draft.Status = DraftStatusQuarantined
		draft.ScanFindings = appendUniqueStrings(draft.ScanFindings, "skill apply failed; no changes were retained")
		if auditErr := rt.recordRollbackAudit(store, draft, err); auditErr != nil {
			draft.ScanFindings = appendUniqueStrings(
				draft.ScanFindings,
				"apply-failure audit could not be recorded",
			)
			if saveErr := store.SaveDrafts([]SkillDraft{draft}); saveErr != nil {
				return draft, errorsJoin(fmt.Errorf("%w: %v", ErrApplyDraftFailed, err), auditErr, saveErr)
			}
			return draft, errorsJoin(fmt.Errorf("%w: %v", ErrApplyDraftFailed, err), auditErr)
		}
		if saveErr := store.SaveDrafts([]SkillDraft{draft}); saveErr != nil {
			return draft, errorsJoin(fmt.Errorf("%w: %v", ErrApplyDraftFailed, err), saveErr)
		}
		return draft, fmt.Errorf("%w: %v", ErrApplyDraftFailed, err)
	}

	now := rt.now()
	draft.Status = DraftStatusAccepted
	draft.AppliedAt = &now
	draft.UpdatedAt = &now
	if saveErr := store.SaveDrafts([]SkillDraft{draft}); saveErr != nil {
		logger.WarnCF("evolution", "Skill draft save failed after apply", map[string]any{
			"workspace_ref": evolutionLogRef(workspace),
			"error_class":   "draft_persistence_failed",
			"run_ref":       evolutionLogRef(runID),
		})
		if rollbackErr := rollbackApply(); rollbackErr != nil {
			return draft, errorsJoin(fmt.Errorf("%w: %v", ErrApplyDraftFailed, saveErr), rollbackErr)
		}
		return draft, fmt.Errorf("%w: %v", ErrApplyDraftFailed, saveErr)
	}

	if err := rt.saveAppliedProfile(store, workspace, draft); err != nil {
		logger.WarnCF("evolution", "Skill profile save failed after apply", map[string]any{
			"workspace_ref": evolutionLogRef(workspace),
			"error_class":   "profile_persistence_failed",
			"run_ref":       evolutionLogRef(runID),
		})
		draft.ScanFindings = appendUniqueStrings(draft.ScanFindings, "skill profile transaction failed")
		return rt.rollbackPostApplyFailure(
			store,
			rollbackApply,
			previousProfile,
			profileExisted,
			draft,
			fmt.Errorf("%w: %v", ErrApplyDraftFailed, err),
		)
	}
	appliedBody, present, snapshotErr := loadCurrentSkillBody(workspace, draft.TargetSkillName)
	if snapshotErr != nil || !present {
		return rt.rollbackPostApplyFailure(
			store, rollbackApply, previousProfile, profileExisted, draft,
			errorsJoin(ErrApplyDraftFailed, snapshotErr),
		)
	}
	if snapshotErr := store.SaveSkillVersion(SkillVersionSnapshot{
		Version: draft.ID, SkillName: draft.TargetSkillName, Workspace: workspace,
		Body: appliedBody, Present: true, CreatedAt: rt.now(),
	}); snapshotErr != nil {
		return rt.rollbackPostApplyFailure(
			store, rollbackApply, previousProfile, profileExisted, draft,
			fmt.Errorf("%w: %v", ErrApplyDraftFailed, snapshotErr),
		)
	}
	if auditErr := store.AppendAudit(AuditEvent{
		Action: "apply", Workspace: workspace, DraftID: draft.ID,
		SkillName: draft.TargetSkillName, Timestamp: rt.now(),
		Details: map[string]any{"change_kind": draft.ChangeKind},
	}); auditErr != nil {
		return rt.rollbackPostApplyFailure(
			store, rollbackApply, previousProfile, profileExisted, draft, auditErr,
		)
	}
	if pruneErr := store.PruneSkillVersions(
		workspace,
		draft.TargetSkillName,
		rt.cfg.EffectiveRollbackRetention(),
		draft.ID,
		draft.PreviousVersion,
	); pruneErr != nil {
		logger.WarnCF("evolution", "Evolution version retention failed", map[string]any{
			"workspace_ref": evolutionLogRef(workspace), "error_class": "version_retention_failed",
		})
	}
	logger.InfoCF("evolution", "Applied skill draft successfully", map[string]any{
		"workspace_ref": evolutionLogRef(workspace),
		"run_ref":       evolutionLogRef(runID),
	})
	return draft, nil
}

func quarantinePreApplyFailure(
	store *Store,
	draft SkillDraft,
	cause error,
) (SkillDraft, error) {
	draft.Status = DraftStatusQuarantined
	draft.ScanFindings = appendUniqueStrings(
		draft.ScanFindings,
		"pre-apply state transaction failed; no skill changes were made",
	)
	var saveErr error
	if store != nil {
		saveErr = store.SaveDrafts([]SkillDraft{draft})
	}
	return draft, errorsJoin(fmt.Errorf("%w: %v", ErrApplyDraftFailed, cause), saveErr)
}

func evolutionLogRef(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "none"
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:6])
}

func (rt *Runtime) rollbackPostApplyFailure(
	store *Store,
	rollbackApply func() error,
	previousProfile SkillProfile,
	profileExisted bool,
	draft SkillDraft,
	cause error,
) (SkillDraft, error) {
	var rollbackErr error
	if rollbackApply != nil {
		rollbackErr = rollbackApply()
	}
	var profileErr error
	if profileExisted {
		profileErr = store.SaveProfile(previousProfile)
	} else {
		profileErr = store.DeleteProfile(draft.WorkspaceID, draft.TargetSkillName)
	}
	draft.Status = DraftStatusQuarantined
	draft.ScanFindings = appendUniqueStrings(draft.ScanFindings, "post-apply audit/version transaction failed")
	saveErr := store.SaveDrafts([]SkillDraft{draft})
	return draft, errorsJoin(cause, rollbackErr, profileErr, saveErr)
}

func (rt *Runtime) recordRollbackAudit(store *Store, draft SkillDraft, applyErr error) error {
	now := rt.now()
	reason := "apply_failed"
	details := map[string]any{"error_class": "apply_failed"}
	if errors.Is(applyErr, context.Canceled) || errors.Is(applyErr, context.DeadlineExceeded) {
		reason = "canceled"
		details["error_class"] = reason
	}
	profileErr := store.UpdateProfile(
		draft.WorkspaceID,
		draft.TargetSkillName,
		func(profile *SkillProfile, exists bool) error {
			if !exists {
				return nil
			}
			profile.VersionHistory = append(profile.VersionHistory, SkillVersionEntry{
				Version:        profile.CurrentVersion,
				Action:         "rollback",
				Timestamp:      now,
				DraftID:        draft.ID,
				Summary:        "Rolled back failed draft apply",
				Rollback:       true,
				RollbackReason: reason,
			})
			maxHistory := rt.cfg.EffectiveRollbackRetention() * 4
			if len(profile.VersionHistory) > maxHistory {
				profile.VersionHistory = profile.VersionHistory[len(profile.VersionHistory)-maxHistory:]
			}
			return nil
		},
	)
	auditErr := store.AppendAudit(AuditEvent{
		Action: "apply_failed", Workspace: draft.WorkspaceID, DraftID: draft.ID,
		SkillName: draft.TargetSkillName, Timestamp: now, Details: details,
	})
	return errorsJoin(profileErr, auditErr)
}

func profileOrigin(origin string) string {
	if origin == "manual" {
		return origin
	}
	return "evolved"
}

func appendUniqueStrings(existing []string, values ...string) []string {
	seen := make(map[string]struct{}, len(existing))
	for _, value := range existing {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		existing = append(existing, value)
		seen[value] = struct{}{}
	}
	return existing
}

type skillUsageSummary struct {
	All []string
}

func buildSkillUsage(input TurnCaseInput) skillUsageSummary {
	capacity := len(input.ActiveSkillNames) + len(input.AttemptedSkillNames) + len(input.FinalSuccessfulPath)
	for _, snapshot := range input.SkillContextSnapshots {
		capacity += len(snapshot.SkillNames)
	}
	for _, exec := range input.ToolExecutions {
		capacity += len(exec.SkillNames)
	}

	all := make([]string, 0, capacity)
	all = append(all, input.ActiveSkillNames...)
	all = append(all, input.AttemptedSkillNames...)
	all = append(all, input.FinalSuccessfulPath...)
	for _, snapshot := range input.SkillContextSnapshots {
		all = append(all, snapshot.SkillNames...)
	}
	for _, exec := range input.ToolExecutions {
		all = append(all, exec.SkillNames...)
	}
	return skillUsageSummary{All: uniqueTrimmedNames(all)}
}

func (rt *Runtime) recordSkillUsage(input TurnCaseInput, success bool) error {
	usage := buildSkillUsage(input)
	if len(usage.All) == 0 {
		return nil
	}

	store := rt.storeForWorkspace(input.Workspace)
	seen := make(map[string]struct{}, len(usage.All))
	for _, skillName := range usage.All {
		skillName = strings.TrimSpace(skillName)
		if skillName == "" {
			continue
		}
		if _, ok := seen[skillName]; ok {
			continue
		}
		seen[skillName] = struct{}{}

		if err := rt.touchSkillProfile(store, input, skillName, success); err != nil {
			return err
		}
	}
	return nil
}

func (rt *Runtime) touchSkillProfile(store *Store, input TurnCaseInput, skillName string, success bool) error {
	now := rt.now()
	return store.UpdateProfile(input.Workspace, skillName, func(profile *SkillProfile, exists bool) error {
		if !exists {
			*profile = SkillProfile{
				SkillName:      skillName,
				WorkspaceID:    input.Workspace,
				Status:         SkillStatusActive,
				Origin:         "manual",
				HumanSummary:   skillName,
				RetentionScore: 0.2,
			}
		}

		profile.SkillName = skillName
		profile.WorkspaceID = input.Workspace
		if profile.Status == SkillStatusCold || profile.Status == SkillStatusArchived || profile.Status == "" {
			profile.Status = SkillStatusActive
		}
		if profile.Origin == "" {
			profile.Origin = "manual"
		}
		if strings.TrimSpace(profile.HumanSummary) == "" {
			profile.HumanSummary = skillName
		}
		profile.LastUsedAt = now
		profile.UseCount++
		profile.RetentionScore = nextRetentionScore(profile.RetentionScore, success)
		return nil
	})
}

func nextRetentionScore(current float64, success bool) float64 {
	increment := 0.05
	if success {
		increment = 0.1
	}
	current += increment
	if current > 1 {
		return 1
	}
	return current
}
