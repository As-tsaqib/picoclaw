package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/providers"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func TestNewCommandProductionWiringCreatesAndActivatesWithoutClearingHistory(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         t.TempDir(),
				ModelName:         "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
		Session: config.SessionConfig{Dimensions: []string{"chat"}},
	}
	provider := &countingMockProvider{response: "LLM reply"}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	helper := testHelper{al: al}

	base := bus.InboundMessage{Context: bus.InboundContext{
		Channel: "whatsapp", ChatID: "chat-new-safety", ChatType: "direct", SenderID: "user-new-safety",
	}}
	seed := base
	seed.Content = "keep this history"
	if got := helper.executeAndGetResponse(t, context.Background(), seed); got != "LLM reply" {
		t.Fatalf("seed reply = %q, want LLM reply", got)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls after seed = %d, want 1", provider.calls)
	}

	route, agent, err := al.resolveMessageRoute(testInboundMessage(base))
	if err != nil {
		t.Fatalf("resolve route: %v", err)
	}
	allocation := al.allocateRouteSession(route, testInboundMessage(base))
	catalog, ok := agent.Sessions.(session.ScopedSessionStore)
	if !ok {
		t.Fatal("production session store does not implement ScopedSessionStore")
	}
	oldKey := catalog.ActiveScopedSession(&allocation.Scope, allocation.SessionAliases)
	if strings.TrimSpace(oldKey) == "" {
		t.Fatal("old active session key is empty")
	}
	oldHistory := agent.Sessions.GetHistory(oldKey)
	if len(oldHistory) < 2 {
		t.Fatalf("old session history len = %d, want at least user+assistant", len(oldHistory))
	}

	newMsg := base
	newMsg.Content = "/new"
	newReply := helper.executeAndGetResponse(t, context.Background(), newMsg)
	if !strings.Contains(newReply, "Session aktif:") {
		t.Fatalf("/new reply = %q, want session activation reply", newReply)
	}
	if provider.calls != 1 {
		t.Fatalf("provider called for /new: calls=%d, want 1 total", provider.calls)
	}
	newKey := catalog.ActiveScopedSession(&allocation.Scope, allocation.SessionAliases)
	if newKey == "" || newKey == oldKey {
		t.Fatalf("active session after /new = %q, old = %q", newKey, oldKey)
	}
	assertHistoryPreserved(t, agent.Sessions.GetHistory(oldKey), oldHistory)
	if got := agent.Sessions.GetHistory(newKey); len(got) != 0 {
		t.Fatalf("new session history len = %d, want 0", len(got))
	}
	assertSessionKeysPresent(t, catalog, &allocation.Scope, allocation.SessionAliases, oldKey, newKey)

	namedMsg := base
	namedMsg.Content = "/new research"
	namedReply := helper.executeAndGetResponse(t, context.Background(), namedMsg)
	if !strings.Contains(namedReply, "Session aktif:") {
		t.Fatalf("/new research reply = %q, want session activation reply", namedReply)
	}
	if provider.calls != 1 {
		t.Fatalf("provider called for /new research: calls=%d, want 1 total", provider.calls)
	}
	namedKey := catalog.ActiveScopedSession(&allocation.Scope, allocation.SessionAliases)
	if namedKey == "" || namedKey == newKey || namedKey == oldKey {
		t.Fatalf("named active session = %q, previous=%q old=%q", namedKey, newKey, oldKey)
	}
	records, err := catalog.ListScopedSessions(&allocation.Scope, allocation.SessionAliases)
	if err != nil {
		t.Fatalf("list scoped sessions: %v", err)
	}
	foundNamed := false
	for _, record := range records {
		if record.Key == namedKey {
			foundNamed = true
			if record.Name != "research" {
				t.Fatalf("named session name = %q, want research", record.Name)
			}
		}
	}
	if !foundNamed {
		t.Fatalf("named active session %q not found in catalog", namedKey)
	}
	assertHistoryPreserved(t, agent.Sessions.GetHistory(oldKey), oldHistory)
}

func assertHistoryPreserved(t *testing.T, got, want []providers.Message) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("preserved history len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Role != want[i].Role || got[i].Content != want[i].Content {
			t.Fatalf("history[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func assertSessionKeysPresent(
	t *testing.T,
	catalog session.ScopedSessionStore,
	scope *session.SessionScope,
	aliases []string,
	keys ...string,
) {
	t.Helper()
	records, err := catalog.ListScopedSessions(scope, aliases)
	if err != nil {
		t.Fatalf("list scoped sessions: %v", err)
	}
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		seen[record.Key] = true
	}
	for _, key := range keys {
		if !seen[key] {
			t.Fatalf("session %q disappeared from catalog", key)
		}
	}
}

func TestInternalCallbackPresentationNormalizesLegacyModelAndStatusHeaders(t *testing.T) {
	cases := []struct {
		kind string
		want []string
	}{
		{kind: "model_current", want: bus.CardHeaderColumns(bus.CardHeaderDetail, true)},
		{kind: "current_state", want: bus.CardHeaderColumns(bus.CardHeaderDetail, true)},
		{kind: "channel_status", want: bus.CardHeaderColumns(bus.CardHeaderStatus, true)},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			response := &bus.InternalCallbackResponse{Content: &bus.StructuredContent{
				Kind: tc.kind,
				Tables: []bus.StructuredTable{{
					Columns: []string{"Properti", "Nilai"},
					Rows:    [][]string{{"key", "value"}},
				}},
			}}
			got := normalizeInternalCallbackPresentation(response)
			if got != response || got.Content == nil || len(got.Content.Tables) == 0 {
				t.Fatal("callback presentation normalization lost response content")
			}
			assertAgentColumns(t, got.Content.Tables[0].Columns, tc.want)
		})
	}
}

func assertAgentColumns(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("columns = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("columns = %v, want %v", got, want)
		}
	}
	for _, column := range got {
		upper := strings.ToUpper(column)
		if strings.Contains(upper, "PROPERTI") || strings.Contains(upper, "NILAI") {
			t.Fatalf("legacy typography leaked into structured header: %v", got)
		}
	}
}
