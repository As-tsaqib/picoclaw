package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/evolution"
	"github.com/sipeed/picoclaw/pkg/memory"
	"github.com/sipeed/picoclaw/pkg/routing"
)

const managementBodyLimit = 256 << 10

var errManagementInvalidRequest = errors.New("invalid management request")

type workspaceMemoryMutationRequest struct {
	Action     string   `json:"action"`
	ID         string   `json:"id,omitempty"`
	Content    string   `json:"content,omitempty"`
	Type       string   `json:"type,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
	Supersedes string   `json:"supersedes,omitempty"`
}

type pendingMemoryDiff struct {
	PendingID    string `json:"pending_id"`
	Action       string `json:"action"`
	Target       string `json:"target"`
	ID           string `json:"id,omitempty"`
	Type         string `json:"type,omitempty"`
	OldValue     string `json:"old_value,omitempty"`
	Proposed     string `json:"proposed_value,omitempty"`
	Provenance   string `json:"provenance,omitempty"`
	CreatedAt    string `json:"created_at"`
	MutationRank int    `json:"mutation_index"`
}

func (h *Handler) registerMemoryEvolutionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/memory/workspace", h.handleListWorkspaceMemory)
	mux.HandleFunc("POST /api/memory/workspace", h.handleMutateWorkspaceMemory)
	mux.HandleFunc("GET /api/memory/status", h.handleMemoryManagementStatus)
	mux.HandleFunc("GET /api/memory/pending", h.handleListWorkspacePendingMemory)
	mux.HandleFunc("POST /api/memory/pending/{id}/{decision}", h.handleResolveWorkspacePendingMemory)

	mux.HandleFunc("GET /api/evolution/status", h.handleEvolutionStatus)
	mux.HandleFunc("POST /api/evolution/review", h.handleEvolutionReview)
	mux.HandleFunc("GET /api/evolution/drafts", h.handleEvolutionDrafts)
	mux.HandleFunc("GET /api/evolution/drafts/{id}", h.handleEvolutionDraft)
	mux.HandleFunc("GET /api/evolution/drafts/{id}/evidence", h.handleEvolutionEvidence)
	mux.HandleFunc("GET /api/evolution/drafts/{id}/preview", h.handleEvolutionPreview)
	mux.HandleFunc("POST /api/evolution/drafts/{id}/{decision}", h.handleEvolutionDraftDecision)
	mux.HandleFunc("GET /api/evolution/versions/{skill}", h.handleEvolutionVersions)
	mux.HandleFunc("POST /api/evolution/rollback", h.handleEvolutionRollback)
}

func (h *Handler) dashboardMemoryStore() (*memory.CuratedStore, *config.Config, string, error) {
	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		return nil, nil, "", err
	}
	workspace := filepath.Clean(strings.TrimSpace(cfg.Agents.Defaults.Workspace))
	if workspace == "" || workspace == "." {
		return nil, nil, "", fmt.Errorf("default workspace is unavailable")
	}
	root := agent.StructuredMemoryRoot(workspace, routing.DefaultAgentID)
	store, err := memory.NewCuratedStore(filepath.Join(root, "curated"), memory.CuratedStoreOptions{
		WorkspaceCharLimit: cfg.Memory.EffectiveWorkspaceCharLimit(),
		PerUserCharLimit:   cfg.Memory.EffectivePerUserCharLimit(),
	})
	return store, cfg, root, err
}

func dashboardWorkspaceCaller() memory.CallerScope {
	return memory.CallerScope{AgentID: routing.DefaultAgentID}
}

func (h *Handler) handleListWorkspaceMemory(w http.ResponseWriter, r *http.Request) {
	if err := validateManagementQuery(r, "query"); err != nil {
		writeManagementError(w, err)
		return
	}
	store, _, _, err := h.dashboardMemoryStore()
	if err != nil {
		writeManagementError(w, err)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	var entries []memory.CuratedEntry
	if query == "" {
		entries, err = store.List(memory.CuratedTargetWorkspace, dashboardWorkspaceCaller())
	} else if utf8.RuneCountInString(query) <= 500 {
		entries, err = store.Search(memory.CuratedTargetWorkspace, dashboardWorkspaceCaller(), query, 100)
	} else {
		err = memory.ErrCuratedInvalidAction
	}
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (h *Handler) handleMutateWorkspaceMemory(w http.ResponseWriter, r *http.Request) {
	if err := validateManagementQuery(r); err != nil {
		writeManagementError(w, err)
		return
	}
	var req workspaceMemoryMutationRequest
	if err := decodeManagementJSON(w, r, &req); err != nil {
		writeManagementError(w, err)
		return
	}
	if (req.ID != "" && !memory.ValidCuratedEntryID(req.ID)) ||
		(req.Supersedes != "" && !memory.ValidCuratedEntryID(req.Supersedes)) {
		writeManagementError(w, errManagementInvalidRequest)
		return
	}
	store, _, _, err := h.dashboardMemoryStore()
	if err != nil {
		writeManagementError(w, err)
		return
	}
	mutation := memory.CuratedMutation{
		Action: strings.ToLower(strings.TrimSpace(req.Action)), ID: strings.TrimSpace(req.ID),
		Content: strings.TrimSpace(req.Content), Type: strings.ToLower(strings.TrimSpace(req.Type)),
		Confidence: req.Confidence, Supersedes: strings.TrimSpace(req.Supersedes),
		Provenance: memory.Provenance{Source: "authenticated_dashboard"},
	}
	result, err := store.ApplyBatch(
		memory.CuratedTargetWorkspace,
		dashboardWorkspaceCaller(),
		[]memory.CuratedMutation{mutation},
		false,
	)
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusOK, result)
}

func (h *Handler) handleMemoryManagementStatus(w http.ResponseWriter, r *http.Request) {
	if err := validateManagementQuery(r); err != nil {
		writeManagementError(w, err)
		return
	}
	store, cfg, root, err := h.dashboardMemoryStore()
	if err != nil {
		writeManagementError(w, err)
		return
	}
	stats, err := store.Stats(memory.CuratedTargetWorkspace, dashboardWorkspaceCaller())
	if err != nil {
		writeManagementError(w, err)
		return
	}
	reviewStore, reviewErr := memory.NewReviewStateStore(filepath.Join(root, "review"))
	var review memory.ReviewStateSummary
	if reviewErr == nil {
		review, reviewErr = reviewStore.Summary()
	}
	if reviewErr != nil {
		writeManagementError(w, reviewErr)
		return
	}
	writeManagementJSON(w, http.StatusOK, map[string]any{
		"enabled": cfg.Memory.Enabled, "approval_mode": cfg.Memory.EffectiveApprovalMode(),
		"background_review_enabled": cfg.Memory.BackgroundReview.Enabled,
		"review_interval":           cfg.Memory.BackgroundReview.EffectiveInterval(),
		"workspace":                 stats, "review": review,
	})
}

func (h *Handler) handleListWorkspacePendingMemory(w http.ResponseWriter, r *http.Request) {
	if err := validateManagementQuery(r); err != nil {
		writeManagementError(w, err)
		return
	}
	store, _, _, err := h.dashboardMemoryStore()
	if err != nil {
		writeManagementError(w, err)
		return
	}
	caller := dashboardWorkspaceCaller()
	pending, err := store.Pending(memory.CuratedTargetWorkspace, caller)
	if err != nil {
		writeManagementError(w, err)
		return
	}
	entries, err := store.List(memory.CuratedTargetWorkspace, caller)
	if err != nil {
		writeManagementError(w, err)
		return
	}
	oldValues := make(map[string]string, len(entries))
	for _, entry := range entries {
		oldValues[entry.ID] = boundedRedactedMemory(entry.Content)
	}
	diffs := make([]pendingMemoryDiff, 0)
	for _, change := range pending {
		for index, mutation := range change.Mutations {
			diffs = append(diffs, pendingMemoryDiff{
				PendingID: change.ID, Action: mutation.Action,
				Target: memory.CuratedTargetWorkspace, ID: mutation.ID,
				Type: memory.NormalizeCuratedType(mutation.Type), OldValue: oldValues[mutation.ID],
				Proposed:   boundedRedactedMemory(mutation.Content),
				Provenance: mutation.Provenance.Source,
				CreatedAt:  change.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), MutationRank: index,
			})
		}
	}
	writeManagementJSON(w, http.StatusOK, map[string]any{"pending": diffs})
}

func (h *Handler) handleResolveWorkspacePendingMemory(w http.ResponseWriter, r *http.Request) {
	if err := validateManagementQuery(r); err != nil {
		writeManagementError(w, err)
		return
	}
	if err := decodeOptionalEmptyManagementJSON(w, r); err != nil {
		writeManagementError(w, err)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	decision := strings.ToLower(strings.TrimSpace(r.PathValue("decision")))
	if !memory.ValidPendingCuratedID(id) || (decision != "approve" && decision != "reject") {
		writeManagementError(w, errManagementInvalidRequest)
		return
	}
	store, _, _, err := h.dashboardMemoryStore()
	if err != nil {
		writeManagementError(w, err)
		return
	}
	var entries []memory.CuratedEntry
	switch decision {
	case "approve":
		entries, err = store.Approve(memory.CuratedTargetWorkspace, dashboardWorkspaceCaller(), id)
	case "reject":
		entries, err = store.Reject(memory.CuratedTargetWorkspace, dashboardWorkspaceCaller(), id)
	}
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (h *Handler) dashboardEvolutionRuntime() (*evolution.Runtime, *config.Config, string, error) {
	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		return nil, nil, "", err
	}
	workspace := filepath.Clean(strings.TrimSpace(cfg.Agents.Defaults.Workspace))
	if workspace == "" || workspace == "." {
		return nil, nil, "", fmt.Errorf("default workspace is unavailable")
	}
	runtime, err := evolution.NewRuntime(evolution.RuntimeOptions{
		Config: cfg.Evolution,
		ApplierFactory: func(value string) *evolution.Applier {
			return evolution.NewApplier(evolution.NewPaths(value, cfg.Evolution.StateDir), nil)
		},
	})
	return runtime, cfg, workspace, err
}

func (h *Handler) handleEvolutionStatus(w http.ResponseWriter, r *http.Request) {
	if err := validateManagementQuery(r); err != nil {
		writeManagementError(w, err)
		return
	}
	runtime, _, workspace, err := h.dashboardEvolutionRuntime()
	if err == nil {
		var status evolution.ControlStatus
		status, err = runtime.ControlStatus(workspace)
		if err == nil {
			writeManagementJSON(w, http.StatusOK, status)
			return
		}
	}
	writeManagementError(w, err)
}

func (h *Handler) handleEvolutionReview(w http.ResponseWriter, r *http.Request) {
	if err := validateManagementQuery(r); err != nil {
		writeManagementError(w, err)
		return
	}
	if err := decodeOptionalEmptyManagementJSON(w, r); err != nil {
		writeManagementError(w, err)
		return
	}
	runtime, _, workspace, err := h.dashboardEvolutionRuntime()
	if err == nil {
		err = runtime.RunManualReview(r.Context(), workspace)
	}
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusAccepted, map[string]any{"status": "completed"})
}

func (h *Handler) handleEvolutionDrafts(w http.ResponseWriter, r *http.Request) {
	if err := validateManagementQuery(r); err != nil {
		writeManagementError(w, err)
		return
	}
	runtime, _, workspace, err := h.dashboardEvolutionRuntime()
	var drafts []evolution.SkillDraft
	if err == nil {
		drafts, err = runtime.ListDrafts(workspace, 100)
	}
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusOK, map[string]any{"drafts": drafts})
}

func (h *Handler) handleEvolutionDraft(w http.ResponseWriter, r *http.Request) {
	if err := validateEvolutionDraftRequest(r); err != nil {
		writeManagementError(w, err)
		return
	}
	runtime, _, workspace, err := h.dashboardEvolutionRuntime()
	var draft evolution.SkillDraft
	if err == nil {
		draft, err = runtime.GetDraft(workspace, r.PathValue("id"))
	}
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusOK, draft)
}

func (h *Handler) handleEvolutionEvidence(w http.ResponseWriter, r *http.Request) {
	if err := validateEvolutionDraftRequest(r); err != nil {
		writeManagementError(w, err)
		return
	}
	runtime, _, workspace, err := h.dashboardEvolutionRuntime()
	var evidence evolution.EvidenceSummary
	if err == nil {
		evidence, err = runtime.DraftEvidence(workspace, r.PathValue("id"))
	}
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusOK, evidence)
}

func (h *Handler) handleEvolutionPreview(w http.ResponseWriter, r *http.Request) {
	if err := validateEvolutionDraftRequest(r); err != nil {
		writeManagementError(w, err)
		return
	}
	runtime, _, workspace, err := h.dashboardEvolutionRuntime()
	var preview evolution.DraftPreview
	if err == nil {
		preview, err = runtime.PreviewDraft(workspace, r.PathValue("id"))
	}
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusOK, preview)
}

func (h *Handler) handleEvolutionDraftDecision(w http.ResponseWriter, r *http.Request) {
	if err := validateManagementQuery(r); err != nil {
		writeManagementError(w, err)
		return
	}
	if err := decodeOptionalEmptyManagementJSON(w, r); err != nil {
		writeManagementError(w, err)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	decision := strings.ToLower(strings.TrimSpace(r.PathValue("decision")))
	if !evolution.ValidControlID(id) ||
		(decision != "approve" && decision != "reject" && decision != "apply") {
		writeManagementError(w, errManagementInvalidRequest)
		return
	}
	runtime, _, workspace, err := h.dashboardEvolutionRuntime()
	if err != nil {
		writeManagementError(w, err)
		return
	}
	var draft evolution.SkillDraft
	switch decision {
	case "approve":
		draft, err = runtime.ApproveDraft(workspace, id, "authenticated_dashboard")
	case "reject":
		draft, err = runtime.RejectDraft(workspace, id, "authenticated_dashboard")
	case "apply":
		draft, err = runtime.ApplyApprovedDraft(r.Context(), workspace, id)
	}
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusOK, draft)
}

func (h *Handler) handleEvolutionVersions(w http.ResponseWriter, r *http.Request) {
	if err := validateManagementQuery(r); err != nil {
		writeManagementError(w, err)
		return
	}
	skillName := strings.TrimSpace(r.PathValue("skill"))
	if !evolution.ValidSkillTarget(skillName) {
		writeManagementError(w, errManagementInvalidRequest)
		return
	}
	runtime, _, workspace, err := h.dashboardEvolutionRuntime()
	var profile evolution.SkillProfile
	if err == nil {
		profile, err = runtime.ListVersions(workspace, skillName)
	}
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusOK, profile)
}

func (h *Handler) handleEvolutionRollback(w http.ResponseWriter, r *http.Request) {
	if err := validateManagementQuery(r); err != nil {
		writeManagementError(w, err)
		return
	}
	var req struct {
		SkillName string `json:"skill_name"`
		Version   string `json:"version,omitempty"`
	}
	if err := decodeManagementJSON(w, r, &req); err != nil {
		writeManagementError(w, err)
		return
	}
	req.SkillName = strings.TrimSpace(req.SkillName)
	req.Version = strings.TrimSpace(req.Version)
	if !evolution.ValidSkillTarget(req.SkillName) ||
		(req.Version != "" && !evolution.ValidControlID(req.Version)) {
		writeManagementError(w, errManagementInvalidRequest)
		return
	}
	runtime, _, workspace, err := h.dashboardEvolutionRuntime()
	var profile evolution.SkillProfile
	if err == nil {
		profile, err = runtime.RollbackSkill(
			workspace, req.SkillName, req.Version,
			"authenticated_dashboard",
		)
	}
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusOK, profile)
}

func decodeManagementJSON(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, managementBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errManagementInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errManagementInvalidRequest
	}
	return nil
}

func decodeOptionalEmptyManagementJSON(w http.ResponseWriter, r *http.Request) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, managementBodyLimit))
	decoder.DisallowUnknownFields()
	var request struct{}
	if err := decoder.Decode(&request); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return errManagementInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errManagementInvalidRequest
	}
	return nil
}

func validateManagementQuery(r *http.Request, allowed ...string) error {
	allowedKeys := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedKeys[key] = struct{}{}
	}
	for key, values := range r.URL.Query() {
		if _, ok := allowedKeys[key]; !ok || len(values) != 1 {
			return errManagementInvalidRequest
		}
	}
	return nil
}

func validateEvolutionDraftRequest(r *http.Request) error {
	if err := validateManagementQuery(r); err != nil {
		return err
	}
	if !evolution.ValidControlID(strings.TrimSpace(r.PathValue("id"))) {
		return errManagementInvalidRequest
	}
	return nil
}

func writeManagementJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeManagementError(w http.ResponseWriter, err error) {
	code := "management_error"
	status := http.StatusBadRequest
	switch {
	case err == nil:
		status = http.StatusInternalServerError
	case errors.Is(err, errManagementInvalidRequest):
		code = "invalid_request"
	case errors.Is(err, os.ErrNotExist), errors.Is(err, memory.ErrCuratedEntryNotFound):
		code, status = "not_found", http.StatusNotFound
	case errors.Is(err, memory.ErrCuratedInvalidPending):
		code, status = "not_found", http.StatusNotFound
	case errors.Is(err, memory.ErrCuratedDuplicate):
		code = "duplicate"
	case errors.Is(err, memory.ErrCuratedUnsafeContent):
		code = "unsafe_content"
	case errors.Is(err, memory.ErrCuratedInvalidAction):
		code = "invalid_action"
	case errors.Is(err, memory.ErrCuratedInvalidType):
		code = "invalid_type"
	case errors.Is(err, memory.ErrCuratedInvalidTarget):
		code = "invalid_target"
	default:
		var capacity *memory.CapacityError
		if errors.As(err, &capacity) {
			code = "memory_full"
		} else {
			status = http.StatusInternalServerError
		}
	}
	writeManagementJSON(w, status, map[string]any{"error": map[string]string{"code": code}})
}

func boundedRedactedMemory(value string) string {
	value = memory.RedactMemoryText(value)
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= 240 {
		return string(runes)
	}
	return string(runes[:239]) + "…"
}
