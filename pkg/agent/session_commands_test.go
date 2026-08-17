package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/providers"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

type sessionCountingProvider struct{ calls atomic.Int32 }

func (p *sessionCountingProvider) Chat(
	context.Context,
	[]providers.Message,
	[]providers.ToolDefinition,
	string,
	map[string]any,
) (*providers.LLMResponse, error) {
	p.calls.Add(1)
	return &providers.LLMResponse{Content: "unexpected LLM response"}, nil
}

func (*sessionCountingProvider) GetDefaultModel() string { return "session-test" }

func telegramSessionTestMessage(content string) bus.InboundMessage {
	return bus.InboundMessage{Context: bus.InboundContext{
		Channel: "telegram", Account: "bot-a", ChatID: "42", ChatType: "direct", SenderID: "42",
	}, Content: content}
}

func TestSessionAndHelpCommandsReturnStructuredContentWithoutLLMOrHistory(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	provider := &sessionCountingProvider{}
	al.registry.GetDefaultAgent().Provider = provider
	cfg.Agents.Defaults.ModelName = provider.GetDefaultModel()

	for _, command := range []string{"/help", "/session"} {
		var structured *bus.StructuredContent
		response, err := al.processMessageWithStructured(context.Background(), telegramSessionTestMessage(command), &structured)
		require.NoError(t, err)
		require.NotNil(t, structured)
		assert.NotEmpty(t, response)
		assert.NotEmpty(t, structured.Tables)
	}
	assert.Zero(t, provider.calls.Load(), "internal commands must not reach the LLM")
	agent := al.registry.GetDefaultAgent()
	for _, key := range agent.Sessions.ListSessions() {
		assert.Empty(t, agent.Sessions.GetHistory(key), "commands and callbacks must not enter history")
	}
}

func TestInternalSessionCallbackRevalidatesOwnerScopeAndActions(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	msg := telegramSessionTestMessage("")
	route, agent, err := al.resolveMessageRoute(msg)
	require.NoError(t, err)
	allocation := al.allocateRouteSession(route, msg)
	ensureSessionMetadata(agent.Sessions, allocation.SessionKey, &allocation.Scope, allocation.SessionAliases)
	catalog, ok := agent.Sessions.(session.ScopedSessionStore)
	require.True(t, ok)
	first, err := catalog.CreateScopedSession(&allocation.Scope, "First")
	require.NoError(t, err)
	second, err := catalog.CreateScopedSession(&allocation.Scope, "Second")
	require.NoError(t, err)
	require.NoError(t, catalog.SetActiveScopedSession(&allocation.Scope, allocation.SessionAliases, first.Key))

	base := bus.InternalCallbackRequest{
		Kind: "session", Action: "select", Value: second.Key, OwnerID: "42",
		Channel: "telegram", Account: "bot-a", ChatID: "42", AgentID: agent.ID,
		Scope: session.CanonicalScopeSignature(allocation.Scope), Inbound: msg.Context,
	}
	beforeFirst := agent.Sessions.GetHistory(first.Key)
	beforeSecond := agent.Sessions.GetHistory(second.Key)
	response, err := al.handleInternalCallback(context.Background(), base)
	require.NoError(t, err)
	require.NotNil(t, response)
	require.NotNil(t, response.Content)
	assert.Equal(t, second.Key, catalog.ActiveScopedSession(&allocation.Scope, allocation.SessionAliases))
	assert.Equal(t, beforeFirst, agent.Sessions.GetHistory(first.Key))
	assert.Equal(t, beforeSecond, agent.Sessions.GetHistory(second.Key))

	wrongOwner := base
	wrongOwner.OwnerID = "99"
	_, err = al.handleInternalCallback(context.Background(), wrongOwner)
	require.Error(t, err)

	otherMessage := telegramSessionTestMessage("")
	otherMessage.Context.ChatID = "99"
	otherMessage.Context.SenderID = "99"
	otherAllocation := al.allocateRouteSession(route, otherMessage)
	foreign, err := catalog.CreateScopedSession(&otherAllocation.Scope, "Foreign")
	require.NoError(t, err)
	foreignSelect := base
	foreignSelect.Value = foreign.Key
	_, err = al.handleInternalCallback(context.Background(), foreignSelect)
	require.Error(t, err)
	assert.NotEqual(t, foreign.Key, catalog.ActiveScopedSession(&allocation.Scope, allocation.SessionAliases))

	newRequest := base
	newRequest.Action = "new"
	newRequest.Value = ""
	response, err = al.handleInternalCallback(context.Background(), newRequest)
	require.NoError(t, err)
	require.NotNil(t, response.Content)
	newActive := catalog.ActiveScopedSession(&allocation.Scope, allocation.SessionAliases)
	assert.NotEqual(t, first.Key, newActive)
	assert.NotEqual(t, second.Key, newActive)

	renameRequest := base
	renameRequest.Action = "rename"
	response, err = al.handleInternalCallback(context.Background(), renameRequest)
	require.NoError(t, err)
	assert.Contains(t, response.Text, "/session rename")
	closeRequest := base
	closeRequest.Action = "close"
	response, err = al.handleInternalCallback(context.Background(), closeRequest)
	require.NoError(t, err)
	assert.True(t, response.Close)
}

func TestActiveSessionSelectionIsFrozenPerTurnAndRejectsForeignInstance(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	msg := telegramSessionTestMessage("hello")
	route, agent, err := al.resolveMessageRoute(msg)
	require.NoError(t, err)
	allocation := al.allocateRouteSession(route, msg)
	ensureSessionMetadata(agent.Sessions, allocation.SessionKey, &allocation.Scope, allocation.SessionAliases)
	catalog := agent.Sessions.(session.ScopedSessionStore)
	first, err := catalog.CreateScopedSession(&allocation.Scope, "First")
	require.NoError(t, err)
	second, err := catalog.CreateScopedSession(&allocation.Scope, "Second")
	require.NoError(t, err)
	require.NoError(t, catalog.SetActiveScopedSession(&allocation.Scope, allocation.SessionAliases, first.Key))

	frozen, _, ok := al.resolveSteeringTarget(msg)
	require.True(t, ok)
	require.Equal(t, first.Key, frozen)
	require.NoError(t, catalog.SetActiveScopedSession(&allocation.Scope, allocation.SessionAliases, second.Key))
	msg.SessionKey = frozen
	stillFrozen, _, ok := al.resolveSteeringTarget(msg)
	require.True(t, ok)
	assert.Equal(t, first.Key, stillFrozen, "an in-flight turn must retain its original session")

	foreignMessage := msg
	foreignMessage.SessionKey = ""
	foreignMessage.Context.ChatID = "99"
	foreignMessage.Context.SenderID = "99"
	foreignAllocation := al.allocateRouteSession(route, foreignMessage)
	foreign, err := catalog.CreateScopedSession(&foreignAllocation.Scope, "Foreign")
	require.NoError(t, err)
	assert.Equal(t, second.Key, resolveAllocatedSession(agent, allocation, foreign.Key), "foreign instance keys must be ignored")
}

func TestSessionFallbackEscapesMarkdownAndHTMLNames(t *testing.T) {
	fallback := sessionTableFallback([]session.SessionRecord{{
		Key: "one", Name: "<b>*unsafe*</b> | [link]", MessageCount: 1,
	}}, "one", 0)
	assert.NotContains(t, fallback, "<b>")
	assert.Contains(t, fallback, "&lt;b&gt;")
	assert.Contains(t, fallback, `\*unsafe\*`)
	assert.Contains(t, fallback, `\|`)
	assert.Contains(t, fallback, `\[link\]`)
}
