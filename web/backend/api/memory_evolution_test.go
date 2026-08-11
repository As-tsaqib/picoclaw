package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/evolution"
	"github.com/sipeed/picoclaw/pkg/memory"
	"github.com/sipeed/picoclaw/pkg/routing"
)

type managementTestHarness struct {
	configPath string
	workspace  string
	mux        *http.ServeMux
}

func newManagementTestHarness(t *testing.T) managementTestHarness {
	t.Helper()
	configPath, cleanup := setupOAuthTestEnv(t)
	t.Cleanup(cleanup)
	workspace := t.TempDir()
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Evolution = config.EvolutionConfig{
		Enabled: true, Mode: "apply", ApplyPolicy: config.EvolutionApplyApprovalRequired,
		PrivateDataScrubbing: true, MinTaskCount: 2, MinSuccessRatio: 0.8,
		DraftTimeoutSeconds: 45, MaxEvidenceRecords: 50, MaxDraftChars: 12_000,
		RollbackRetention: 10, ColdPathTrigger: "manual",
	}
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return managementTestHarness{configPath: configPath, workspace: workspace, mux: mux}
}

func managementRequest(
	t *testing.T,
	mux *http.ServeMux,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	reader = bytes.NewReader([]byte(body))
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeManagementResponse[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response status=%d body=%q: %v", rec.Code, rec.Body.String(), err)
	}
	return value
}

func TestMemoryManagementAPIWorkspaceLifecycleAndPendingDiff(t *testing.T) {
	harness := newManagementTestHarness(t)

	add := managementRequest(t, harness.mux, http.MethodPost, "/api/memory/workspace", `{
		"action":"add","content":"Use remote CI for release validation","type":"project_fact","confidence":0.95
	}`)
	if add.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", add.Code, add.Body.String())
	}
	added := decodeManagementResponse[memory.CuratedBatchResult](t, add)
	if len(added.Applied) != 1 || !memory.ValidCuratedEntryID(added.Applied[0].ID) {
		t.Fatalf("add result=%#v", added)
	}
	id := added.Applied[0].ID

	search := managementRequest(t, harness.mux, http.MethodGet, "/api/memory/workspace?query=release", "")
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), id) {
		t.Fatalf("search status=%d body=%s", search.Code, search.Body.String())
	}

	for _, request := range []string{
		`{"action":"replace","id":"` + id + `","content":"Use required remote GitHub Actions for release validation","type":"project_fact"}`,
		`{"action":"pin","id":"` + id + `"}`,
		`{"action":"archive","id":"` + id + `"}`,
		`{"action":"restore","id":"` + id + `"}`,
	} {
		rec := managementRequest(t, harness.mux, http.MethodPost, "/api/memory/workspace", request)
		if rec.Code != http.StatusOK {
			t.Fatalf("mutation %s status=%d body=%s", request, rec.Code, rec.Body.String())
		}
	}

	store, err := memory.NewCuratedStore(
		filepath.Join(agent.StructuredMemoryRoot(harness.workspace, routing.DefaultAgentID), "curated"),
		memory.CuratedStoreOptions{WorkspaceCharLimit: 12_000, PerUserCharLimit: 8_000},
	)
	if err != nil {
		t.Fatalf("NewCuratedStore: %v", err)
	}
	pending, err := store.ApplyBatch(
		memory.CuratedTargetWorkspace,
		memory.CallerScope{AgentID: routing.DefaultAgentID},
		[]memory.CuratedMutation{{
			Action: memory.CuratedActionAdd, Content: "Repository uses conventional commits",
			Type:       memory.CuratedTypeProjectFact,
			Provenance: memory.Provenance{Source: "background_review"},
		}},
		true,
	)
	if err != nil || pending.Pending == nil {
		t.Fatalf("stage pending=%#v err=%v", pending, err)
	}

	pendingList := managementRequest(t, harness.mux, http.MethodGet, "/api/memory/pending", "")
	if pendingList.Code != http.StatusOK ||
		!strings.Contains(pendingList.Body.String(), pending.Pending.ID) ||
		!strings.Contains(pendingList.Body.String(), "Repository uses conventional commits") {
		t.Fatalf("pending status=%d body=%s", pendingList.Code, pendingList.Body.String())
	}
	approve := managementRequest(
		t,
		harness.mux,
		http.MethodPost,
		"/api/memory/pending/"+pending.Pending.ID+"/approve",
		"{}",
	)
	if approve.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approve.Code, approve.Body.String())
	}

	remove := managementRequest(
		t,
		harness.mux,
		http.MethodPost,
		"/api/memory/workspace",
		`{"action":"remove","id":"`+id+`"}`,
	)
	if remove.Code != http.StatusOK {
		t.Fatalf("remove status=%d body=%s", remove.Code, remove.Body.String())
	}
	listed := managementRequest(t, harness.mux, http.MethodGet, "/api/memory/workspace", "")
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), id) {
		t.Fatalf("list after remove status=%d body=%s", listed.Code, listed.Body.String())
	}
}

func TestMemoryEvolutionManagementAPIRejectsArbitrarySelectorsAndMalformedInput(t *testing.T) {
	harness := newManagementTestHarness(t)
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/memory/workspace?user_id=another-user", ""},
		{http.MethodGet, "/api/memory/status?path=/tmp/other", ""},
		{http.MethodPost, "/api/memory/workspace", `{"action":"add","content":"safe","type":"project_fact"}{}`},
		{
			http.MethodPost,
			"/api/memory/workspace",
			`{"action":"add","content":"safe","type":"project_fact","user_id":"other"}`,
		},
		{http.MethodPost, "/api/memory/workspace", `{"action":"remove","id":"../../private"}`},
		{http.MethodPost, "/api/memory/pending/not-a-pending-id/approve", "{}"},
		{
			http.MethodPost,
			"/api/memory/pending/pm_0011223344556677/approve?user_id=other",
			"{}",
		},
		{http.MethodGet, "/api/evolution/drafts?workspace=/tmp/other", ""},
		{http.MethodGet, "/api/evolution/drafts/bad$id", ""},
		{http.MethodPost, "/api/evolution/drafts/draft-safe/approve", `{"user_id":"other"}`},
		{http.MethodGet, "/api/evolution/versions/bad_skill", ""},
		{http.MethodPost, "/api/evolution/rollback", `{"skill_name":"../escape","version":"v1"}`},
		{http.MethodPost, "/api/evolution/rollback", `{"skill_name":"safe-skill","version":"../../v1"}`},
	}
	for _, test := range tests {
		rec := managementRequest(t, harness.mux, test.method, test.path, test.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"code":"invalid_request"`) {
			t.Fatalf("%s %s body=%s, want structured invalid_request", test.method, test.path, rec.Body.String())
		}
	}

	privateWorkspace := managementRequest(
		t,
		harness.mux,
		http.MethodPost,
		"/api/memory/workspace",
		`{"action":"add","content":"My name is Alice","type":"identity"}`,
	)
	if privateWorkspace.Code != http.StatusBadRequest ||
		!strings.Contains(privateWorkspace.Body.String(), `"code":"invalid_target"`) {
		t.Fatalf("private workspace status=%d body=%s", privateWorkspace.Code, privateWorkspace.Body.String())
	}
	thirdPersonPrivateWorkspace := managementRequest(
		t,
		harness.mux,
		http.MethodPost,
		"/api/memory/workspace",
		`{"action":"add","content":"User A prefers Go and concise Indonesian","type":"project_fact"}`,
	)
	if thirdPersonPrivateWorkspace.Code != http.StatusBadRequest ||
		!strings.Contains(thirdPersonPrivateWorkspace.Body.String(), `"code":"invalid_target"`) {
		t.Fatalf(
			"third-person private workspace status=%d body=%s",
			thirdPersonPrivateWorkspace.Code,
			thirdPersonPrivateWorkspace.Body.String(),
		)
	}
}

func TestEvolutionManagementAPIPreviewApproveApplyVersionsRejectAndRollback(t *testing.T) {
	harness := newManagementTestHarness(t)
	paths := evolution.NewPaths(harness.workspace, "")
	store := evolution.NewStore(paths)
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	success := true
	tasks := []evolution.LearningRecord{
		{
			ID: "task-api-one", Kind: evolution.RecordKindTask, WorkspaceID: harness.workspace,
			CreatedAt: now.Add(-2 * time.Hour), Summary: "remote validation workflow",
			FinalOutput: "remote checks passed", Status: evolution.RecordStatus("clustered"), Success: &success,
		},
		{
			ID: "task-api-two", Kind: evolution.RecordKindTask, WorkspaceID: harness.workspace,
			CreatedAt: now.Add(-time.Hour), Summary: "remote validation workflow",
			FinalOutput: "remote checks passed", Status: evolution.RecordStatus("clustered"), Success: &success,
		},
	}
	if err := store.AppendTaskRecords(context.Background(), tasks); err != nil {
		t.Fatalf("AppendTaskRecords: %v", err)
	}
	pattern := evolution.LearningRecord{
		ID: "pattern-api", Kind: evolution.RecordKindPattern, WorkspaceID: harness.workspace,
		CreatedAt: now, Summary: "remote validation workflow", Status: evolution.RecordStatus("ready"),
		TaskRecordIDs: []string{"task-api-one", "task-api-two"},
	}
	if err := store.AppendPatternRecords([]evolution.LearningRecord{pattern}); err != nil {
		t.Fatalf("AppendPatternRecords: %v", err)
	}
	drafts := []evolution.SkillDraft{
		{
			ID:              "draft-api-apply",
			WorkspaceID:     harness.workspace,
			CreatedAt:       now,
			SourceRecordID:  pattern.ID,
			TargetSkillName: "remote-validation",
			DraftType:       evolution.DraftTypeWorkflow,
			ChangeKind:      evolution.ChangeKindCreate,
			HumanSummary:    "Run required remote validation",
			BodyOrPatch:     "---\nname: remote-validation\ndescription: Run required remote validation\n---\n# Remote Validation\n\nRun required checks in CI.\n",
			Status:          evolution.DraftStatusCandidate,
		},
		{
			ID:              "draft-api-reject",
			WorkspaceID:     harness.workspace,
			CreatedAt:       now.Add(time.Second),
			SourceRecordID:  pattern.ID,
			TargetSkillName: "remote-validation-rejected",
			DraftType:       evolution.DraftTypeWorkflow,
			ChangeKind:      evolution.ChangeKindCreate,
			HumanSummary:    "Rejected alternative",
			BodyOrPatch:     "---\nname: remote-validation-rejected\ndescription: Rejected alternative\n---\n# Alternative\n\nRun remote checks.\n",
			Status:          evolution.DraftStatusCandidate,
		},
	}
	if err := store.SaveDrafts(drafts); err != nil {
		t.Fatalf("SaveDrafts: %v", err)
	}

	for _, path := range []string{
		"/api/evolution/status",
		"/api/evolution/drafts",
		"/api/evolution/drafts/draft-api-apply",
		"/api/evolution/drafts/draft-api-apply/evidence",
		"/api/evolution/drafts/draft-api-apply/preview",
	} {
		rec := managementRequest(t, harness.mux, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	preview := managementRequest(t, harness.mux, http.MethodGet, "/api/evolution/drafts/draft-api-apply/preview", "")
	if !strings.Contains(preview.Body.String(), "Remote Validation") {
		t.Fatalf("preview body=%s", preview.Body.String())
	}

	approve := managementRequest(t, harness.mux, http.MethodPost, "/api/evolution/drafts/draft-api-apply/approve", "{}")
	if approve.Code != http.StatusOK || !strings.Contains(approve.Body.String(), `"status":"approved"`) {
		t.Fatalf("approve status=%d body=%s", approve.Code, approve.Body.String())
	}
	apply := managementRequest(t, harness.mux, http.MethodPost, "/api/evolution/drafts/draft-api-apply/apply", "{}")
	if apply.Code != http.StatusOK || !strings.Contains(apply.Body.String(), `"status":"accepted"`) {
		t.Fatalf("apply status=%d body=%s", apply.Code, apply.Body.String())
	}
	versions := managementRequest(t, harness.mux, http.MethodGet, "/api/evolution/versions/remote-validation", "")
	if versions.Code != http.StatusOK || !strings.Contains(versions.Body.String(), "draft-api-apply") {
		t.Fatalf("versions status=%d body=%s", versions.Code, versions.Body.String())
	}

	reject := managementRequest(t, harness.mux, http.MethodPost, "/api/evolution/drafts/draft-api-reject/reject", "{}")
	if reject.Code != http.StatusOK || !strings.Contains(reject.Body.String(), `"status":"rejected"`) {
		t.Fatalf("reject status=%d body=%s", reject.Code, reject.Body.String())
	}

	rollback := managementRequest(
		t,
		harness.mux,
		http.MethodPost,
		"/api/evolution/rollback",
		`{"skill_name":"remote-validation"}`,
	)
	if rollback.Code != http.StatusOK {
		t.Fatalf("rollback status=%d body=%s", rollback.Code, rollback.Body.String())
	}
	if _, err := os.Stat(filepath.Join(harness.workspace, "skills", "remote-validation", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("new skill should be absent after baseline rollback: %v", err)
	}
}
