package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
	"github.com/As-tsaqib/picoclaw/pkg/providers"
	"github.com/As-tsaqib/picoclaw/pkg/tools"
)

type slowMemoryNotificationReviewer struct {
	mu       sync.Mutex
	calls    int
	delay    time.Duration
	finished chan struct{}
	once     sync.Once
}

func (p *slowMemoryNotificationReviewer) Chat(
	ctx context.Context,
	_ []providers.Message,
	_ []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()

	if call == 1 {
		timer := time.NewTimer(p.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
		return &providers.LLMResponse{ToolCalls: []providers.ToolCall{{
			ID:   "slow-review-memory",
			Type: "function",
			Name: tools.MemoryManageToolName,
			Arguments: map[string]any{
				"action":        "add",
				"target":        "workspace",
				"content":       "Slow reviewer fact",
				"evidence_kind": memory.CuratedEvidenceObserved,
			},
		}}}, nil
	}

	p.once.Do(func() { close(p.finished) })
	return &providers.LLMResponse{Content: "review complete"}, nil
}

func (p *slowMemoryNotificationReviewer) GetDefaultModel() string { return "slow-review-test" }

func TestLogicalTurnNotificationWaitsForSlowReviewerLifecycle(t *testing.T) {
	provider := &slowMemoryNotificationReviewer{
		delay:    150 * time.Millisecond,
		finished: make(chan struct{}),
	}
	al, agent, caller := newMemoryReviewerHarness(t, provider)
	al.cfg.Memory.Notifications = config.MemoryNotificationOn
	al.cfg.Memory.BackgroundReview.Interval = 1
	messageBus := bus.NewMessageBus()
	al.bus = messageBus

	turnID := "slow-reviewer-logical-turn"
	toolCtx := tools.WithToolCallerScope(context.Background(), caller)
	toolCtx = tools.WithToolTurnID(toolCtx, turnID)
	memoryTool := tools.NewMemoryManageToolWithApprovalMode(
		agent.CuratedMemory,
		config.MemoryApprovalOff,
		al.memoryChangeNotification,
	)
	result := memoryTool.Execute(toolCtx, map[string]any{
		"action":           "add",
		"target":           "current_user",
		"content":          "Prefers concise answers",
		"type":             memory.CuratedTypeCommunicationPreference,
		"evidence_kind":    memory.CuratedEvidenceExplicit,
		"preference_key":   "communication.verbosity",
		"preference_value": "concise",
	})
	if result.IsError {
		t.Fatalf("foreground mutation failed: %v", result.Err)
	}

	appendReviewerTurn(t, agent, caller, turnID)
	al.recordAndMaybeReviewMemory(agent, caller, 1, "Remember this preference", turnID)

	// The old 50ms debounce implementation emitted here before the reviewer
	// could contribute its mutation. A logical-turn lifecycle must not.
	select {
	case outbound := <-messageBus.OutboundChan():
		t.Fatalf("notification flushed before slow reviewer completed: %q", outbound.Content)
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case <-provider.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("slow reviewer did not complete")
	}

	select {
	case outbound := <-messageBus.OutboundChan():
		if outbound.Content != "💾 Memory updated" {
			t.Fatalf("unexpected notification content: %q", outbound.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("logical-turn notification was not emitted after reviewer completion")
	}

	select {
	case extra := <-messageBus.OutboundChan():
		t.Fatalf("second notification emitted for one logical turn: %q", extra.Content)
	case <-time.After(100 * time.Millisecond):
	}

	userEntries, userErr := agent.CuratedMemory.List(memory.CuratedTargetCurrentUser, caller)
	workspaceEntries, workspaceErr := agent.CuratedMemory.List(memory.CuratedTargetWorkspace, caller)
	if userErr != nil || workspaceErr != nil || len(userEntries) != 1 || len(workspaceEntries) != 1 {
		t.Fatalf(
			"coalesced mutations missing: user=%d/%v workspace=%d/%v",
			len(userEntries), userErr, len(workspaceEntries), workspaceErr,
		)
	}
}
