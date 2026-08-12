package agent

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/memory"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/session"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type scriptedMemoryReviewProvider struct {
	mu          sync.Mutex
	calls       int
	toolNames   [][]string
	messages    [][]providers.Message
	mutation    map[string]any
	disallowed  string
	started     chan struct{}
	block       bool
	finished    chan struct{}
	startedOnce sync.Once
	finishOnce  sync.Once
}

func (p *scriptedMemoryReviewProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	definitions []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Function.Name)
	}
	sort.Strings(names)
	p.toolNames = append(p.toolNames, names)
	p.messages = append(p.messages, append([]providers.Message(nil), messages...))
	p.mu.Unlock()
	if p.started != nil {
		p.startedOnce.Do(func() { close(p.started) })
	}
	if p.block {
		<-ctx.Done()
		if p.finished != nil {
			p.finishOnce.Do(func() { close(p.finished) })
		}
		return nil, ctx.Err()
	}
	if call == 1 && p.disallowed != "" {
		return &providers.LLMResponse{ToolCalls: []providers.ToolCall{{
			ID: "disallowed", Type: "function", Name: p.disallowed,
			Arguments: map[string]any{},
		}}}, nil
	}
	if call == 1 && p.mutation != nil {
		return &providers.LLMResponse{ToolCalls: []providers.ToolCall{{
			ID: "memory-write", Type: "function", Name: tools.MemoryManageToolName,
			Arguments: p.mutation,
		}}}, nil
	}
	return &providers.LLMResponse{Content: "review complete"}, nil
}

func (p *scriptedMemoryReviewProvider) GetDefaultModel() string { return "review-test" }

func (p *scriptedMemoryReviewProvider) snapshot() (int, [][]string, [][]providers.Message) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, append([][]string(nil), p.toolNames...), append([][]providers.Message(nil), p.messages...)
}

func newMemoryReviewerHarness(
	t *testing.T,
	provider providers.LLMProvider,
) (*AgentLoop, *AgentInstance, memory.CallerScope) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.BackgroundReview.Enabled = true
	cfg.Memory.BackgroundReview.Interval = 2
	cfg.Memory.BackgroundReview.TimeoutSeconds = 2
	cfg.Memory.BackgroundReview.MaxIterations = 2
	root := t.TempDir()
	curated, err := memory.NewCuratedStore(filepath.Join(root, "curated"), memory.CuratedStoreOptions{
		WorkspaceCharLimit: 10_000,
		PerUserCharLimit:   10_000,
	})
	if err != nil {
		t.Fatalf("NewCuratedStore() error = %v", err)
	}
	recall, err := memory.NewRecallStore(filepath.Join(root, "recall"), 100)
	if err != nil {
		t.Fatalf("NewRecallStore() error = %v", err)
	}
	reviewState, err := memory.NewReviewStateStore(filepath.Join(root, "review"))
	if err != nil {
		t.Fatalf("NewReviewStateStore() error = %v", err)
	}
	sessions := session.NewSessionManager(filepath.Join(root, "sessions"))
	agent := &AgentInstance{
		ID: "main", Model: "review-test", Provider: provider,
		Tools: tools.NewToolRegistry(), Sessions: sessions,
		CuratedMemory: curated, RecallMemory: recall, MemoryReviewState: reviewState,
		memoryReviewer: &memoryReviewController{},
	}
	al := &AgentLoop{cfg: cfg, bus: bus.NewMessageBus()}
	caller := memory.CallerScope{
		AgentID: "main", UserKey: "telegram:user-a", Channel: "telegram", Account: "personal",
		ChatID: "user-a", TopicName: "OAuth",
		SessionKey: "session-key-10", SessionRef: "session-ref-10", MessageRef: "message-10",
	}
	return al, agent, caller
}

func appendReviewerTurn(
	t *testing.T,
	agent *AgentInstance,
	caller memory.CallerScope,
	turnID string,
) uint64 {
	t.Helper()
	sequence, err := agent.RecallMemory.AppendDeliveredTurn(
		caller,
		turnID,
		"The repository always validates through GitHub Actions",
		"Understood; local builds remain disabled.",
		"assistant-message",
	)
	if err != nil {
		t.Fatalf("AppendDeliveredTurn() error = %v", err)
	}
	return sequence
}

func TestMemoryReviewerUsesRestrictedToolsAndDoesNotTouchSessionHistory(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "workspace",
		"content": "Repository validation runs only through GitHub Actions",
	}}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	agent.Sessions.AddMessage(caller.SessionKey, "user", "normal history")
	historyBefore := agent.Sessions.GetHistory(caller.SessionKey)
	sequence := appendReviewerTurn(t, agent, caller, "turn-1")
	if _, err := agent.MemoryReviewState.RecordSuccessfulTurn(caller); err != nil {
		t.Fatalf("RecordSuccessfulTurn() error = %v", err)
	}
	if reviewErr := al.runMemoryReview(context.Background(), agent, caller); reviewErr != nil {
		t.Fatalf("runMemoryReview() error = %v", reviewErr)
	}

	entries, err := agent.CuratedMemory.List(memory.CuratedTargetWorkspace, caller)
	if err != nil || len(entries) != 1 {
		t.Fatalf("workspace memory = %#v, %v", entries, err)
	}
	cursor, err := agent.MemoryReviewState.Get(caller)
	if err != nil || cursor.LastReviewedSequence != sequence || cursor.SuccessfulTurns != 0 {
		t.Fatalf("review cursor = %#v, %v", cursor, err)
	}
	historyAfter := agent.Sessions.GetHistory(caller.SessionKey)
	if len(historyAfter) != len(historyBefore) || historyAfter[0].Content != historyBefore[0].Content {
		t.Fatalf("reviewer contaminated session history: before=%#v after=%#v", historyBefore, historyAfter)
	}

	calls, definitions, messages := provider.snapshot()
	if calls != 2 {
		t.Fatalf("provider calls = %d, want 2", calls)
	}
	for _, names := range definitions {
		if len(names) != 1 || names[0] != tools.MemoryManageToolName {
			t.Fatalf("reviewer tool definitions = %v, want only memory_manage", names)
		}
	}
	if len(messages) == 0 || len(messages[0]) != 2 ||
		messages[0][0].Role != "system" || messages[0][1].Role != "user" {
		t.Fatalf("reviewer initial messages = %#v", messages)
	}
}

func TestMemoryReviewerStagesBackgroundWritesWhenApprovalEnabled(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "current_user",
		"content": "Prefers concise progress updates",
	}}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	al.cfg.Memory.WriteApproval = true
	appendReviewerTurn(t, agent, caller, "turn-1")
	if _, err := agent.MemoryReviewState.RecordSuccessfulTurn(caller); err != nil {
		t.Fatalf("RecordSuccessfulTurn() error = %v", err)
	}
	if reviewErr := al.runMemoryReview(context.Background(), agent, caller); reviewErr != nil {
		t.Fatalf("runMemoryReview() error = %v", reviewErr)
	}
	entries, err := agent.CuratedMemory.List(memory.CuratedTargetCurrentUser, caller)
	if err != nil || len(entries) != 0 {
		t.Fatalf("approval mode applied entry: %#v, %v", entries, err)
	}
	pending, err := agent.CuratedMemory.Pending(memory.CuratedTargetCurrentUser, caller)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending changes = %#v, %v", pending, err)
	}
}

func TestMemoryReviewerRejectsSharedGroupScope(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "workspace", "content": "should not be reviewed",
	}}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	caller.ChatID, caller.GroupID, caller.TopicID = "group-1/10", "group-1", "10"
	appendReviewerTurn(t, agent, caller, "group-turn")
	if _, err := agent.MemoryReviewState.RecordSuccessfulTurn(caller); err != nil {
		t.Fatal(err)
	}
	if err := al.flushMemoryReview(context.Background(), agent, caller); err != nil {
		t.Fatalf("group flush should safely skip: %v", err)
	}
	if started, err := al.startMemoryReview(agent, caller, true); err == nil || started {
		t.Fatalf("group reviewer = (%v, %v), want rejected", started, err)
	}
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("group reviewer made %d provider calls", calls)
	}
}

func TestRunMemoryReviewerRejectsDeepScopeBypass(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "workspace", "content": "must not be reviewed",
	}}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	appendReviewerTurn(t, agent, caller, "private-turn")
	if _, err := agent.MemoryReviewState.RecordSuccessfulTurn(caller); err != nil {
		t.Fatal(err)
	}

	groupCaller := caller
	groupCaller.GroupID = "group-1"
	if err := al.runMemoryReview(context.Background(), agent, groupCaller); err == nil {
		t.Fatal("deep reviewer accepted a shared group scope")
	}
	otherAgentCaller := caller
	otherAgentCaller.AgentID = "other-agent"
	if err := al.runMemoryReview(context.Background(), agent, otherAgentCaller); err == nil {
		t.Fatal("deep reviewer accepted a mismatched agent scope")
	}
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("deep scope bypass made %d provider calls", calls)
	}
}

func TestFlushMemoryReviewerRejectsMismatchedAgentScope(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "workspace", "content": "must not be reviewed",
	}}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	appendReviewerTurn(t, agent, caller, "private-turn")
	if _, err := agent.MemoryReviewState.RecordSuccessfulTurn(caller); err != nil {
		t.Fatal(err)
	}

	caller.AgentID = "other-agent"
	if err := al.flushMemoryReview(context.Background(), agent, caller); err == nil {
		t.Fatal("flush reviewer accepted a mismatched agent scope")
	}
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("mismatched flush made %d provider calls", calls)
	}
}

func TestMemoryReviewerRejectsUnrelatedToolAndKeepsCursor(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{disallowed: "exec"}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	appendReviewerTurn(t, agent, caller, "turn-1")
	if _, err := agent.MemoryReviewState.RecordSuccessfulTurn(caller); err != nil {
		t.Fatalf("RecordSuccessfulTurn() error = %v", err)
	}
	err := al.runMemoryReview(context.Background(), agent, caller)
	if err == nil {
		t.Fatal("runMemoryReview() error = nil, want disallowed tool failure")
	}
	cursor, getErr := agent.MemoryReviewState.Get(caller)
	if getErr != nil || cursor.LastReviewedSequence != 0 || cursor.SuccessfulTurns != 1 {
		t.Fatalf("failed review advanced cursor: %#v, %v", cursor, getErr)
	}
}

func TestMemoryReviewerTriggersAtIntervalAndOnlyOneRuns(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{
		started: make(chan struct{}), finished: make(chan struct{}), block: true,
	}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	appendReviewerTurn(t, agent, caller, "turn-1")
	al.recordAndMaybeReviewMemory(agent, caller, 1, "ordinary message")
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("review triggered before interval: %d calls", calls)
	}
	appendReviewerTurn(t, agent, caller, "turn-2")
	al.recordAndMaybeReviewMemory(agent, caller, 2, "ordinary message")
	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("review did not trigger at configured interval")
	}
	started, err := al.startMemoryReview(agent, caller, false)
	if err != nil || started {
		t.Fatalf("second concurrent review started=%t err=%v", started, err)
	}
	al.cancelMemoryReviewForLiveTurn(agent, processOptions{Dispatch: DispatchRequest{
		InboundContext: &bus.InboundContext{Channel: "telegram", ChatID: caller.ChatID, SenderID: "user-a"},
	}})
	select {
	case <-provider.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("live turn did not cancel active reviewer")
	}
}

func TestMemoryReviewEligibilityExcludesFailedStyleInternalTurns(t *testing.T) {
	caller := memory.CallerScope{UserKey: "user", Channel: "telegram", SessionRef: "session"}
	base := &turnState{opts: processOptions{Dispatch: DispatchRequest{
		InboundContext: &bus.InboundContext{Channel: "telegram", SenderID: "user"},
	}}}
	if !memoryReviewEligible(base, caller) {
		t.Fatal("normal delivered turn should be review eligible")
	}

	tests := []*turnState{
		{opts: processOptions{NoHistory: true}},
		{opts: processOptions{SuppressMemoryReview: true}},
		{depth: 1},
		{opts: processOptions{Dispatch: DispatchRequest{InboundContext: &bus.InboundContext{SenderID: "cron"}}}},
		{opts: processOptions{Dispatch: DispatchRequest{InboundContext: &bus.InboundContext{SenderID: "heartbeat"}}}},
	}
	for index, state := range tests {
		if memoryReviewEligible(state, caller) {
			t.Fatalf("ineligible state %d was accepted: %#v", index, state.opts)
		}
	}
	internalCaller := caller
	internalCaller.Channel = "system"
	if memoryReviewEligible(base, internalCaller) {
		t.Fatal("internal channel was review eligible")
	}
}

func TestMemoryReviewerTimeoutIsBestEffort(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{
		started: make(chan struct{}), finished: make(chan struct{}), block: true,
	}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	al.cfg.Memory.BackgroundReview.TimeoutSeconds = 1
	appendReviewerTurn(t, agent, caller, "turn-1")
	started, err := al.startMemoryReview(agent, caller, true)
	if err != nil || !started {
		t.Fatalf("startMemoryReview() started=%t err=%v", started, err)
	}
	select {
	case <-provider.finished:
	case <-time.After(3 * time.Second):
		t.Fatal("reviewer did not stop at timeout")
	}
	cursor, err := agent.MemoryReviewState.Get(caller)
	if err != nil {
		t.Fatalf("Get(cursor) error = %v", err)
	}
	if cursor.LastReviewedSequence != 0 {
		t.Fatalf("timed-out review advanced cursor: %#v", cursor)
	}
}

func TestMemoryNotificationPreviewDoesNotExposePrivateGroupContent(t *testing.T) {
	event := tools.MemoryChangeEvent{
		Caller: memory.CallerScope{GroupID: "group-1"},
		Target: memory.CuratedTargetCurrentUser,
		Result: memory.CuratedBatchResult{Applied: []memory.CuratedEntry{{
			ID: "mem_0000000000000000", Content: "Private timezone is Asia/Makassar",
		}}},
	}
	preview := formatMemoryChangePreview(event)
	if preview == "" || containsAny(preview, "timezone", "Makassar") {
		t.Fatalf("private group notification preview = %q", preview)
	}
}

func TestVerboseMemoryNotificationIsRedactedAndUsesTrustedAccount(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Notifications = config.MemoryNotificationVerbose
	messageBus := bus.NewMessageBus()
	al := &AgentLoop{cfg: cfg, bus: messageBus}
	event := tools.MemoryChangeEvent{
		Caller: memory.CallerScope{
			AgentID: "main", Channel: "telegram", Account: "personal",
			ChatID: "group-1/10", GroupID: "group-1", TopicID: "10",
		},
		Target: memory.CuratedTargetCurrentUser,
		Result: memory.CuratedBatchResult{Applied: []memory.CuratedEntry{{
			ID: "mem_0000000000000000", Content: "Private timezone is Asia/Makassar",
		}}},
	}
	al.memoryChangeNotification(context.Background(), event)
	select {
	case outbound := <-messageBus.OutboundChan():
		if outbound.Context.Account != "personal" || outbound.Context.TopicID != "10" {
			t.Fatalf("notification context = %#v", outbound.Context)
		}
		if containsAny(outbound.Content, "timezone", "Makassar") ||
			!strings.Contains(outbound.Content, "private operation") {
			t.Fatalf("verbose notification leaked private content: %q", outbound.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("memory notification was not published")
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func TestHighSalienceMemoryHintsAreMultilingual(t *testing.T) {
	for _, value := range []string{
		"From now on I prefer detailed answers",
		"Mulai sekarang saya lebih suka jawaban singkat",
		"تذكر أنني أفضل الشرح المختصر",
	} {
		if !isHighSalienceMemoryText(value) {
			t.Fatalf("high-salience preference not detected: %q", value)
		}
	}
	if isHighSalienceMemoryText("What is the weather today?") {
		t.Fatal("ordinary transient question treated as high-salience memory")
	}
}

func TestMemoryReviewerHighSalienceCanTriggerBeforeInterval(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{started: make(chan struct{}), finished: make(chan struct{}), block: true}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	appendReviewerTurn(t, agent, caller, "turn-1")
	al.recordAndMaybeReviewMemory(agent, caller, 1, "Mulai sekarang saya lebih suka jawaban singkat")
	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("high-salience preference did not trigger early review")
	}
	al.cancelMemoryReviewForLiveTurn(agent, processOptions{Dispatch: DispatchRequest{
		InboundContext: &bus.InboundContext{Channel: "telegram", ChatID: caller.ChatID, SenderID: "user-a"},
	}})
	select {
	case <-provider.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("high-salience reviewer did not stop")
	}
}

func TestUnreviewedRecallSurvivesStoreRestartAndCanBeCurated(t *testing.T) {
	root := t.TempDir()
	curatedPath := filepath.Join(root, "curated")
	recallPath := filepath.Join(root, "recall")
	reviewPath := filepath.Join(root, "review")
	caller := memory.CallerScope{
		AgentID: "main", UserKey: "telegram:user-restart", Channel: "telegram", Account: "personal",
		SessionKey: "restart-session", SessionRef: "restart-session-ref",
	}
	curated, err := memory.NewCuratedStore(curatedPath, memory.CuratedStoreOptions{
		WorkspaceCharLimit: 10_000, PerUserCharLimit: 10_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	recall, err := memory.NewRecallStore(recallPath, 100)
	if err != nil {
		t.Fatal(err)
	}
	reviewState, err := memory.NewReviewStateStore(reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, appendErr := recall.AppendDeliveredTurn(
		caller, "turn-before-restart", "I prefer detailed explanations", "Understood.", "assistant-message",
	); appendErr != nil {
		t.Fatal(appendErr)
	}
	if _, recordErr := reviewState.RecordSuccessfulTurn(caller); recordErr != nil {
		t.Fatal(recordErr)
	}
	_ = curated // created before restart to prove all stores can be reopened together

	// Recreate all stores as a process restart would. The unreviewed transcript
	// and cursor must remain durable even without a shutdown-time model call.
	reopenedCurated, err := memory.NewCuratedStore(curatedPath, memory.CuratedStoreOptions{
		WorkspaceCharLimit: 10_000, PerUserCharLimit: 10_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	reopenedRecall, err := memory.NewRecallStore(recallPath, 100)
	if err != nil {
		t.Fatal(err)
	}
	reopenedReview, err := memory.NewReviewStateStore(reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	records, latest, err := reopenedRecall.RecordsAfter(caller, 0, 4_000)
	if err != nil || len(records) != 2 || latest == 0 {
		t.Fatalf("unreviewed recall did not survive restart: %#v latest=%d err=%v", records, latest, err)
	}
	cursor, err := reopenedReview.Get(caller)
	if err != nil || cursor.SuccessfulTurns != 1 || cursor.LastReviewedSequence != 0 {
		t.Fatalf("review cursor did not survive restart: %#v err=%v", cursor, err)
	}

	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "current_user",
		"content":          "Prefers detailed explanations",
		"type":             memory.CuratedTypeCommunicationPreference,
		"evidence_kind":    memory.CuratedEvidenceExplicit,
		"preference_key":   "communication.verbosity",
		"preference_value": "detailed",
	}}
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.BackgroundReview.Enabled = true
	cfg.Memory.BackgroundReview.MaxIterations = 2
	al := &AgentLoop{cfg: cfg}
	agent := &AgentInstance{
		ID: "main", Model: "review-test", Provider: provider,
		Tools: tools.NewToolRegistry(), CuratedMemory: reopenedCurated, RecallMemory: reopenedRecall,
		MemoryReviewState: reopenedReview, memoryReviewer: &memoryReviewController{},
	}
	if reviewErr := al.runMemoryReview(context.Background(), agent, caller); reviewErr != nil {
		t.Fatalf("post-restart review failed: %v", reviewErr)
	}
	entries, err := reopenedCurated.List(memory.CuratedTargetCurrentUser, caller)
	if err != nil || len(entries) != 1 || entries[0].PreferenceValue != "detailed" {
		t.Fatalf("post-restart curator did not preserve preference: %#v err=%v", entries, err)
	}
}

func TestRepeatedReviewWithoutNewRecallDoesNotDuplicateMemory(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "current_user",
		"content":          "Prefers concise progress updates",
		"type":             memory.CuratedTypeCommunicationPreference,
		"evidence_kind":    memory.CuratedEvidenceExplicit,
		"preference_key":   "communication.verbosity",
		"preference_value": "concise",
	}}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	caller.GroupID, caller.ChatID, caller.TopicID, caller.TopicName = "", "user-a", "", ""
	appendReviewerTurn(t, agent, caller, "turn-once")
	if _, err := agent.MemoryReviewState.RecordSuccessfulTurn(caller); err != nil {
		t.Fatal(err)
	}
	if reviewErr := al.runMemoryReview(context.Background(), agent, caller); reviewErr != nil {
		t.Fatal(reviewErr)
	}
	callsAfterFirst, _, _ := provider.snapshot()
	if reviewErr := al.runMemoryReview(context.Background(), agent, caller); reviewErr != nil {
		t.Fatal(reviewErr)
	}
	callsAfterSecond, _, _ := provider.snapshot()
	if callsAfterSecond != callsAfterFirst {
		t.Fatalf("reviewer called provider without new recall: first=%d second=%d", callsAfterFirst, callsAfterSecond)
	}
	entries, err := agent.CuratedMemory.List(memory.CuratedTargetCurrentUser, caller)
	if err != nil || len(entries) != 1 {
		t.Fatalf("repeat review duplicated memory: %#v err=%v", entries, err)
	}
}

func TestLifecycleFlushRunsWhenScheduledReviewerIsDisabled(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "current_user",
		"content":          "Prefers detailed setup explanations",
		"type":             memory.CuratedTypeCommunicationPreference,
		"evidence_kind":    memory.CuratedEvidenceExplicit,
		"preference_key":   "communication.verbosity",
		"preference_value": "detailed",
	}}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	al.cfg.Memory.BackgroundReview.Enabled = false
	appendReviewerTurn(t, agent, caller, "turn-before-compression")
	al.recordAndMaybeReviewMemory(agent, caller, 1, "I prefer detailed setup explanations")
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("disabled scheduled reviewer made %d calls before lifecycle flush", calls)
	}
	if err := al.flushMemoryReview(context.Background(), agent, caller); err != nil {
		t.Fatalf("lifecycle flush with scheduled reviewer disabled: %v", err)
	}
	entries, err := agent.CuratedMemory.List(memory.CuratedTargetCurrentUser, caller)
	if err != nil || len(entries) != 1 || entries[0].PreferenceValue != "detailed" {
		t.Fatalf("lifecycle flush result=%#v err=%v", entries, err)
	}
}

func TestRememberedMemoryCallerScopesAreBoundedAndRefreshRecentSessions(t *testing.T) {
	al := &AgentLoop{}
	for index := 0; index < maxRememberedMemoryCallerScopes; index++ {
		key := fmt.Sprintf("session-%03d", index)
		al.rememberMemoryCallerScope(memory.CallerScope{
			UserKey: "user-a", SessionKey: key, SessionRef: key,
		})
	}
	al.rememberMemoryCallerScope(memory.CallerScope{
		UserKey: "user-a", SessionKey: "session-000", SessionRef: "session-000",
	})
	al.rememberMemoryCallerScope(memory.CallerScope{
		UserKey: "user-a", SessionKey: "session-new", SessionRef: "session-new",
	})

	count := 0
	al.memoryCallerScopes.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != maxRememberedMemoryCallerScopes {
		t.Fatalf("remembered caller scopes = %d, want %d", count, maxRememberedMemoryCallerScopes)
	}
	if _, ok := al.memoryCallerScopes.Load("session-000"); !ok {
		t.Fatal("refreshed caller scope was evicted")
	}
	if _, ok := al.memoryCallerScopes.Load("session-001"); ok {
		t.Fatal("least-recently remembered caller scope was retained")
	}
	if _, ok := al.memoryCallerScopes.Load("session-new"); !ok {
		t.Fatal("new caller scope was not retained")
	}
}

func TestRememberedMemoryCallerScopesStayBoundedConcurrently(t *testing.T) {
	al := &AgentLoop{}
	var writers sync.WaitGroup
	for index := 0; index < maxRememberedMemoryCallerScopes*4; index++ {
		writers.Add(1)
		go func(index int) {
			defer writers.Done()
			key := fmt.Sprintf("concurrent-session-%03d", index)
			al.rememberMemoryCallerScope(memory.CallerScope{
				UserKey: "user-a", SessionKey: key, SessionRef: key,
			})
		}(index)
	}
	writers.Wait()

	count := 0
	al.memoryCallerScopes.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != maxRememberedMemoryCallerScopes {
		t.Fatalf("remembered caller scopes = %d, want %d", count, maxRememberedMemoryCallerScopes)
	}
}

func TestCompressionLifecycleFlushesBeforeCompaction(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "current_user",
		"content":          "Prefers concise progress updates",
		"type":             memory.CuratedTypeCommunicationPreference,
		"evidence_kind":    memory.CuratedEvidenceExplicit,
		"preference_key":   "communication.verbosity",
		"preference_value": "concise",
	}}
	al, agent, _ := newMemoryReviewerHarness(t, provider)
	al.cfg.Memory.BackgroundReview.Enabled = false
	tracker := &trackingContextManager{}
	al.contextManager = tracker
	opts := processOptions{
		EnableSummary: true,
		Dispatch: DispatchRequest{
			SessionKey: "compression-session",
			InboundContext: &bus.InboundContext{
				Channel: "telegram", Account: "personal", ChatID: "user-a",
				ChatType: "direct", SenderID: "user-a",
			},
		},
	}
	ts := newTurnState(agent, opts, turnEventScope{
		turnID: "turn-before-compression", context: newTurnContext(nil, nil, nil),
	})
	caller := callerScopeForTurn(agent.ID, al.cfg, opts)
	appendReviewerTurn(t, agent, caller, ts.turnID)
	al.recordAndMaybeReviewMemory(agent, caller, 1, "I prefer concise progress updates")
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("disabled scheduled reviewer made %d calls before compaction", calls)
	}

	pipeline := NewPipeline(al)
	exec := newTurnExecution(agent, opts, nil, "", nil)
	if _, err := pipeline.Finalize(
		context.Background(), context.Background(), ts, exec, TurnEndStatusCompleted, "Delivered response",
	); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if tracker.compactCalls.Load() != 1 {
		t.Fatalf("context compact calls = %d, want 1", tracker.compactCalls.Load())
	}
	entries, err := agent.CuratedMemory.List(memory.CuratedTargetCurrentUser, caller)
	if err != nil || len(entries) != 1 || entries[0].PreferenceValue != "concise" {
		t.Fatalf("pre-compaction lifecycle memory = %#v, %v", entries, err)
	}
}

func TestShutdownRegistryFlushesRememberedScopeOnce(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "workspace",
		"content": "The project validates releases in remote CI",
		"type":    memory.CuratedTypeProjectFact,
	}}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	al.registry = &AgentRegistry{
		cfg: al.cfg,
		agents: map[string]*AgentInstance{
			"main": agent,
		},
	}
	appendReviewerTurn(t, agent, caller, "turn-before-shutdown")
	if _, err := agent.MemoryReviewState.RecordSuccessfulTurn(caller); err != nil {
		t.Fatal(err)
	}
	al.rememberMemoryCallerScope(caller)
	al.flushMemoryReviewsForRegistry(context.Background(), al.registry, "shutdown_test")
	al.flushMemoryReviewsForRegistry(context.Background(), al.registry, "shutdown_test_repeat")

	entries, err := agent.CuratedMemory.List(memory.CuratedTargetWorkspace, caller)
	if err != nil || len(entries) != 1 {
		t.Fatalf("shutdown flush entries=%#v err=%v", entries, err)
	}
	if calls, _, _ := provider.snapshot(); calls != 2 {
		t.Fatalf("shutdown and repeat provider calls=%d, want one two-call review", calls)
	}
}

func TestRegistryReloadFlushesRememberedScopeOnce(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "workspace",
		"content": "The project validates releases in remote CI",
		"type":    memory.CuratedTypeProjectFact,
	}}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	al.registry = &AgentRegistry{
		cfg: al.cfg,
		agents: map[string]*AgentInstance{
			"main": agent,
		},
	}
	appendReviewerTurn(t, agent, caller, "turn-before-reload")
	if _, err := agent.MemoryReviewState.RecordSuccessfulTurn(caller); err != nil {
		t.Fatal(err)
	}
	al.rememberMemoryCallerScope(caller)
	al.flushMemoryReviewsForRegistry(context.Background(), al.registry, "registry_reload_test")
	al.flushMemoryReviewsForRegistry(context.Background(), al.registry, "registry_reload_test_repeat")

	entries, err := agent.CuratedMemory.List(memory.CuratedTargetWorkspace, caller)
	if err != nil || len(entries) != 1 {
		t.Fatalf("reload flush entries=%#v err=%v", entries, err)
	}
	if calls, _, _ := provider.snapshot(); calls != 2 {
		t.Fatalf("reload and repeat provider calls=%d, want one two-call review", calls)
	}
}

func TestRegistryFlushNeverFallsBackAcrossAgentRoots(t *testing.T) {
	provider := &scriptedMemoryReviewProvider{mutation: map[string]any{
		"action": "add", "target": "workspace",
		"content": "must not cross an agent boundary",
	}}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	caller.AgentID = "removed-agent"
	al.registry = &AgentRegistry{
		cfg: al.cfg,
		agents: map[string]*AgentInstance{
			"main": agent,
		},
	}
	appendReviewerTurn(t, agent, caller, "turn-for-removed-agent")
	if _, err := agent.MemoryReviewState.RecordSuccessfulTurn(caller); err != nil {
		t.Fatal(err)
	}
	al.rememberMemoryCallerScope(caller)
	al.flushMemoryReviewsForRegistry(context.Background(), al.registry, "removed_agent_test")

	entries, err := agent.CuratedMemory.List(memory.CuratedTargetWorkspace, caller)
	if err != nil || len(entries) != 0 {
		t.Fatalf("removed-agent scope reached default store: entries=%#v err=%v", entries, err)
	}
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("removed-agent scope made %d provider calls", calls)
	}
}
