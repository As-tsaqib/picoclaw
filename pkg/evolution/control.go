package evolution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

type ControlStatus struct {
	Enabled         bool       `json:"enabled"`
	Mode            string     `json:"mode"`
	ApplyPolicy     string     `json:"apply_policy"`
	TaskRecords     int        `json:"task_records"`
	PatternRecords  int        `json:"pattern_records"`
	Drafts          int        `json:"drafts"`
	PendingDrafts   int        `json:"pending_drafts"`
	ApprovedDrafts  int        `json:"approved_drafts"`
	Quarantined     int        `json:"quarantined_drafts"`
	Profiles        int        `json:"profiles"`
	LastObservation *time.Time `json:"last_observation,omitempty"`
	LastAudit       *time.Time `json:"last_audit,omitempty"`
}

type EvidenceSummary struct {
	DraftID       string   `json:"draft_id"`
	SourceRecord  string   `json:"source_record_id"`
	EvidenceCount int      `json:"evidence_count"`
	SuccessRatio  float64  `json:"success_ratio"`
	TaskIDs       []string `json:"task_ids,omitempty"`
}

func (rt *Runtime) ControlStatus(workspace string) (ControlStatus, error) {
	if rt == nil {
		return ControlStatus{}, fmt.Errorf("evolution runtime is unavailable")
	}
	status := ControlStatus{
		Enabled:     rt.cfg.Enabled,
		Mode:        rt.cfg.EffectiveMode(),
		ApplyPolicy: rt.cfg.EffectiveApplyPolicy(),
	}
	store := rt.storeForWorkspace(workspace)
	tasks, err := store.LoadTaskRecords()
	if err != nil {
		return status, err
	}
	patterns, err := store.LoadPatternRecords()
	if err != nil {
		return status, err
	}
	drafts, err := store.LoadDrafts()
	if err != nil {
		return status, err
	}
	profiles, err := store.LoadProfiles()
	if err != nil {
		return status, err
	}
	status.TaskRecords = countWorkspaceRecords(tasks, workspace)
	status.PatternRecords = countWorkspaceRecords(patterns, workspace)
	for _, record := range tasks {
		if record.WorkspaceID != workspace {
			continue
		}
		if status.LastObservation == nil || record.CreatedAt.After(*status.LastObservation) {
			value := record.CreatedAt
			status.LastObservation = &value
		}
	}
	for _, draft := range drafts {
		if draft.WorkspaceID != workspace {
			continue
		}
		status.Drafts++
		switch draft.Status {
		case DraftStatusCandidate:
			status.PendingDrafts++
		case DraftStatusApproved:
			status.ApprovedDrafts++
		case DraftStatusQuarantined:
			status.Quarantined++
		}
	}
	for _, profile := range profiles {
		if profile.WorkspaceID == workspace {
			status.Profiles++
		}
	}
	audit, err := store.LoadAuditForWorkspace(workspace, 1)
	if err != nil {
		return status, err
	}
	if len(audit) > 0 {
		value := audit[len(audit)-1].Timestamp
		status.LastAudit = &value
	}
	return status, nil
}

func countWorkspaceRecords(records []LearningRecord, workspace string) int {
	count := 0
	for _, record := range records {
		if record.WorkspaceID == workspace {
			count++
		}
	}
	return count
}

func (rt *Runtime) RunManualReview(ctx context.Context, workspace string) error {
	if rt == nil || !rt.cfg.Enabled {
		return fmt.Errorf("evolution is disabled")
	}
	timeout := time.Duration(rt.cfg.EffectiveDraftTimeoutSeconds()) * time.Second
	reviewCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return rt.RunColdPathOnce(reviewCtx, workspace)
}

func (rt *Runtime) ListDrafts(workspace string, limit int) ([]SkillDraft, error) {
	drafts, err := rt.storeForWorkspace(workspace).LoadDrafts()
	if err != nil {
		return nil, err
	}
	out := make([]SkillDraft, 0, len(drafts))
	for _, draft := range drafts {
		if draft.WorkspaceID == workspace {
			out = append(out, draft)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (rt *Runtime) GetDraft(workspace, id string) (SkillDraft, error) {
	if !validEvolutionControlID(id) {
		return SkillDraft{}, fmt.Errorf("invalid draft id")
	}
	drafts, err := rt.ListDrafts(workspace, 200)
	if err != nil {
		return SkillDraft{}, err
	}
	for _, draft := range drafts {
		if draft.ID == id {
			return draft, nil
		}
	}
	return SkillDraft{}, os.ErrNotExist
}

func (rt *Runtime) DraftEvidence(workspace, id string) (EvidenceSummary, error) {
	draft, err := rt.GetDraft(workspace, id)
	if err != nil {
		return EvidenceSummary{}, err
	}
	patterns, err := rt.storeForWorkspace(workspace).LoadPatternRecords()
	if err != nil {
		return EvidenceSummary{}, err
	}
	summary := EvidenceSummary{
		DraftID: id, SourceRecord: draft.SourceRecordID,
		EvidenceCount: draft.EvidenceCount, SuccessRatio: draft.SuccessRatio,
	}
	for _, pattern := range patterns {
		if pattern.WorkspaceID == workspace && pattern.ID == draft.SourceRecordID {
			summary.TaskIDs = append([]string(nil), pattern.TaskRecordIDs...)
			break
		}
	}
	if len(summary.TaskIDs) > rt.cfg.EffectiveMaxEvidenceRecords() {
		summary.TaskIDs = summary.TaskIDs[:rt.cfg.EffectiveMaxEvidenceRecords()]
	}
	return summary, nil
}

func (rt *Runtime) PreviewDraft(workspace, id string) (DraftPreview, error) {
	draft, err := rt.GetDraft(workspace, id)
	if err != nil {
		return DraftPreview{}, err
	}
	preview, err := BuildDraftPreview(workspace, draft)
	if err != nil {
		return DraftPreview{}, err
	}
	maximum := rt.cfg.EffectiveMaxDraftChars() * 2
	preview.CurrentBody = truncateControlText(preview.CurrentBody, maximum)
	preview.RenderedBody = truncateControlText(preview.RenderedBody, maximum)
	preview.DiffPreview = truncateControlText(preview.DiffPreview, maximum)
	return preview, nil
}

func (rt *Runtime) ApproveDraft(workspace, id, source string) (SkillDraft, error) {
	if rt == nil || !rt.cfg.Enabled {
		return SkillDraft{}, fmt.Errorf("evolution is disabled")
	}
	store := rt.storeForWorkspace(workspace)
	original, err := rt.GetDraft(workspace, id)
	if err != nil {
		return SkillDraft{}, err
	}
	verified, err := rt.verifiedDraftEvidence(workspace, original)
	if err != nil {
		return SkillDraft{}, err
	}
	review := ReviewDraftWithPolicy(verified, rt.cfg.EffectiveMaxDraftChars())
	if review.Status == DraftStatusQuarantined {
		quarantined, updateErr := store.UpdateDraft(workspace, id, func(draft *SkillDraft) error {
			if draft.Status != DraftStatusCandidate {
				return fmt.Errorf("draft must be a candidate before approval")
			}
			now := rt.now()
			draft.EvidenceCount = verified.EvidenceCount
			draft.SuccessRatio = verified.SuccessRatio
			draft.Status = DraftStatusQuarantined
			draft.ScanFindings = appendUniqueStrings(draft.ScanFindings, review.Findings...)
			draft.UpdatedAt = &now
			return nil
		})
		if updateErr != nil {
			return SkillDraft{}, updateErr
		}
		if auditErr := store.AppendAudit(AuditEvent{
			Action: "quarantine", Workspace: workspace, DraftID: id,
			SkillName: quarantined.TargetSkillName, Timestamp: rt.now(),
		}); auditErr != nil {
			restoreErr := store.SaveDrafts([]SkillDraft{original})
			return SkillDraft{}, errorsJoin(auditErr, restoreErr)
		}
		return SkillDraft{}, fmt.Errorf("unsafe draft was quarantined")
	}
	draft, err := store.UpdateDraft(workspace, id, func(draft *SkillDraft) error {
		if draft.Status != DraftStatusCandidate {
			return fmt.Errorf("draft must be a candidate before approval")
		}
		draft.EvidenceCount = verified.EvidenceCount
		draft.SuccessRatio = verified.SuccessRatio
		if draft.EvidenceCount < rt.cfg.EffectiveMinTaskCount() ||
			draft.SuccessRatio < rt.cfg.EffectiveMinSuccessRatio() {
			return fmt.Errorf("draft evidence does not meet configured thresholds")
		}
		now := rt.now()
		draft.Status = DraftStatusApproved
		draft.ApprovedAt = &now
		draft.DecisionSource = boundedDecisionSource(source)
		draft.UpdatedAt = &now
		return nil
	})
	if err != nil {
		return SkillDraft{}, err
	}
	if err := store.AppendAudit(AuditEvent{
		Action: "approve", Workspace: workspace, DraftID: id,
		SkillName: draft.TargetSkillName, Timestamp: rt.now(),
	}); err != nil {
		restoreErr := store.SaveDrafts([]SkillDraft{original})
		return SkillDraft{}, errorsJoin(err, restoreErr)
	}
	return draft, nil
}

func (rt *Runtime) RejectDraft(workspace, id, source string) (SkillDraft, error) {
	if rt == nil || !rt.cfg.Enabled {
		return SkillDraft{}, fmt.Errorf("evolution is disabled")
	}
	store := rt.storeForWorkspace(workspace)
	original, err := rt.GetDraft(workspace, id)
	if err != nil {
		return SkillDraft{}, err
	}
	draft, err := store.UpdateDraft(workspace, id, func(draft *SkillDraft) error {
		if draft.Status != DraftStatusCandidate && draft.Status != DraftStatusApproved {
			return fmt.Errorf("only candidate or approved drafts may be rejected")
		}
		now := rt.now()
		draft.Status = DraftStatusRejected
		draft.RejectedAt = &now
		draft.DecisionSource = boundedDecisionSource(source)
		draft.UpdatedAt = &now
		return nil
	})
	if err != nil {
		return SkillDraft{}, err
	}
	if err := store.AppendAudit(AuditEvent{
		Action: "reject", Workspace: workspace, DraftID: id,
		SkillName: draft.TargetSkillName, Timestamp: rt.now(),
	}); err != nil {
		restoreErr := store.SaveDrafts([]SkillDraft{original})
		return SkillDraft{}, errorsJoin(err, restoreErr)
	}
	return draft, nil
}

func (rt *Runtime) ApplyApprovedDraft(ctx context.Context, workspace, id string) (SkillDraft, error) {
	if rt == nil || !rt.cfg.Enabled {
		return SkillDraft{}, fmt.Errorf("evolution is disabled")
	}
	if rt.cfg.EffectiveMode() != "apply" {
		return SkillDraft{}, fmt.Errorf("evolution mode must be apply")
	}
	store := rt.storeForWorkspace(workspace)
	draft, err := rt.GetDraft(workspace, id)
	if err != nil {
		return SkillDraft{}, err
	}
	if draft.Status != DraftStatusApproved {
		return SkillDraft{}, fmt.Errorf("draft must be explicitly approved before apply")
	}
	draft, err = rt.verifiedDraftEvidence(workspace, draft)
	if err != nil {
		return SkillDraft{}, err
	}
	review := ReviewDraftWithPolicy(draft, rt.cfg.EffectiveMaxDraftChars())
	if review.Status == DraftStatusQuarantined {
		_, _ = store.UpdateDraft(workspace, id, func(value *SkillDraft) error {
			value.Status = DraftStatusQuarantined
			value.ScanFindings = appendUniqueStrings(value.ScanFindings, review.Findings...)
			return nil
		})
		return SkillDraft{}, fmt.Errorf("unsafe draft was quarantined")
	}
	applier := rt.applierForWorkspace(workspace)
	if applier == nil {
		return SkillDraft{}, fmt.Errorf("evolution applier is unavailable")
	}
	updated, err := rt.applyCandidateDraft(ctx, workspace, store, applier, draft, "manual-approved")
	if err != nil {
		return updated, err
	}
	return updated, nil
}

func (rt *Runtime) verifiedDraftEvidence(workspace string, draft SkillDraft) (SkillDraft, error) {
	store := rt.storeForWorkspace(workspace)
	patterns, err := store.LoadPatternRecords()
	if err != nil {
		return SkillDraft{}, err
	}
	var source LearningRecord
	found := false
	for _, pattern := range patterns {
		if pattern.WorkspaceID == workspace && pattern.ID == draft.SourceRecordID &&
			isPatternRecordKind(pattern.Kind) {
			source = pattern
			found = true
			break
		}
	}
	if !found {
		return SkillDraft{}, fmt.Errorf("draft source evidence is unavailable")
	}
	tasks, err := store.LoadTaskRecords()
	if err != nil {
		return SkillDraft{}, err
	}
	tasks = filterEvolutionRecordsForWorkspace(tasks, workspace)
	tasks = limitEvolutionRecords(tasks, rt.cfg.EffectiveMaxEvidenceRecords())
	verified := withDraftEvidenceMetrics(draft, draftEvidenceForRule(source, tasks))
	return verified, nil
}

func (rt *Runtime) ListVersions(workspace, skillName string) (SkillProfile, error) {
	if err := validateEvolutionSkillTarget(skillName); err != nil {
		return SkillProfile{}, err
	}
	profile, err := rt.storeForWorkspace(workspace).LoadProfile(skillName)
	if err != nil {
		return SkillProfile{}, err
	}
	if profile.WorkspaceID != workspace {
		return SkillProfile{}, fmt.Errorf("skill profile scope mismatch")
	}
	return profile, nil
}

func (rt *Runtime) RollbackSkill(workspace, skillName, version, source string) (SkillProfile, error) {
	if rt == nil || !rt.cfg.Enabled {
		return SkillProfile{}, fmt.Errorf("evolution is disabled")
	}
	if err := validateEvolutionSkillTarget(skillName); err != nil {
		return SkillProfile{}, err
	}
	store := rt.storeForWorkspace(workspace)
	profile, err := store.LoadProfile(skillName)
	if err != nil {
		return SkillProfile{}, err
	}
	if profile.WorkspaceID != workspace {
		return SkillProfile{}, fmt.Errorf("skill profile scope mismatch")
	}
	if strings.TrimSpace(profile.CurrentVersion) == "" {
		return SkillProfile{}, fmt.Errorf("current skill version is unavailable")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = previousProfileVersion(profile)
	}
	if version == "" || version == profile.CurrentVersion {
		return SkillProfile{}, fmt.Errorf("no previous skill version is available")
	}
	snapshot, err := store.LoadSkillVersion(workspace, skillName, version)
	if err != nil {
		return SkillProfile{}, err
	}
	if snapshot.Present {
		if err := validateAppliedSkillBody(snapshot.Body, skillName, true); err != nil {
			return SkillProfile{}, err
		}
	}
	currentBody, currentPresent, err := loadCurrentSkillBody(workspace, skillName)
	if err != nil {
		return SkillProfile{}, err
	}
	if _, loadErr := store.LoadSkillVersion(workspace, skillName, profile.CurrentVersion); errors.Is(loadErr, os.ErrNotExist) {
		if saveErr := store.SaveSkillVersion(SkillVersionSnapshot{
			Version: profile.CurrentVersion, SkillName: skillName, Workspace: workspace,
			Body: currentBody, Present: currentPresent, CreatedAt: rt.now(),
		}); saveErr != nil {
			return SkillProfile{}, saveErr
		}
	} else if loadErr != nil {
		return SkillProfile{}, loadErr
	}
	if err := writeSkillVersionBody(workspace, skillName, snapshot.Body, snapshot.Present); err != nil {
		return SkillProfile{}, err
	}
	now := rt.now()
	fromVersion := profile.CurrentVersion
	previousProfile := profile
	profile.CurrentVersion = version
	if snapshot.Present {
		profile.Status = SkillStatusActive
	} else {
		profile.Status = SkillStatusDeleted
	}
	profile.VersionHistory = append(profile.VersionHistory, SkillVersionEntry{
		Version: version, Action: "rollback", Timestamp: now,
		Summary: "Administrator-approved rollback", Rollback: true,
		RollbackReason: boundedDecisionSource(source),
	})
	if len(profile.VersionHistory) > rt.cfg.EffectiveRollbackRetention()*4 {
		profile.VersionHistory = profile.VersionHistory[len(profile.VersionHistory)-rt.cfg.EffectiveRollbackRetention()*4:]
	}
	if err := store.SaveProfile(profile); err != nil {
		restoreErr := writeSkillVersionBody(workspace, skillName, currentBody, currentPresent)
		return SkillProfile{}, errorsJoin(err, restoreErr)
	}
	if err := store.AppendAudit(AuditEvent{
		Action: "rollback", Workspace: workspace, SkillName: skillName, Timestamp: now,
		Details: map[string]any{
			"from_version": fromVersion, "to_version": version,
			"source": boundedDecisionSource(source),
		},
	}); err != nil {
		restoreSkillErr := writeSkillVersionBody(workspace, skillName, currentBody, currentPresent)
		restoreProfileErr := store.SaveProfile(previousProfile)
		return SkillProfile{}, errorsJoin(err, restoreSkillErr, restoreProfileErr)
	}
	return profile, nil
}

func writeSkillVersionBody(workspace, skillName, body string, present bool) error {
	if err := validateEvolutionSkillTarget(skillName); err != nil {
		return err
	}
	skillPath := filepath.Join(workspace, "skills", skillName, "SKILL.md")
	if !present {
		if err := os.Remove(skillPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(skillPath, []byte(body), 0o644)
}

func previousProfileVersion(profile SkillProfile) string {
	for i := len(profile.VersionHistory) - 1; i >= 0; i-- {
		version := strings.TrimSpace(profile.VersionHistory[i].Version)
		if version != "" && version != profile.CurrentVersion {
			return version
		}
	}
	return ""
}

func boundedDecisionSource(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "authenticated_admin"
	}
	return truncateControlText(value, 80)
}

func truncateControlText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if maximum <= 0 || utf8.RuneCountInString(value) <= maximum {
		return value
	}
	runes := []rune(value)
	if maximum == 1 {
		return "…"
	}
	return string(runes[:maximum-1]) + "…"
}
