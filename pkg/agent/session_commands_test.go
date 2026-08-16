package agent

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/providers"
	"github.com/As-tsaqib/picoclaw/pkg/routing"
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
		response, err := al.processMessageWithStructured(
			context.Background(),
			telegramSessionTestMessage(command),
			&structured,
		)
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
	msg.Context.ChatID = "-1001"
	msg.Context.ChatType = "group"
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
		Channel: "telegram", Account: "bot-a", ChatID: "-1001", AgentID: agent.ID,
		Scope: session.CanonicalScopeSignature(allocation.Scope), DashboardMode: "route", Inbound: msg.Context,
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

	frozen, _, _, ok := al.resolveSteeringTarget(msg)
	require.True(t, ok)
	require.Equal(t, first.Key, frozen)
	require.NoError(t, catalog.SetActiveScopedSession(&allocation.Scope, allocation.SessionAliases, second.Key))
	msg.SessionKey = frozen
	stillFrozen, _, _, ok := al.resolveSteeringTarget(msg)
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

func TestPersonalDashboardFrozenTargetIgnoresConcurrentMappingChange(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	agent := al.registry.GetDefaultAgent()
	catalog := agent.Sessions.(session.ScopedSessionStore)
	dashboard := agent.Sessions.(session.DashboardSessionStore)

	direct := telegramSessionTestMessage("")
	route, _, err := al.resolveMessageRoute(direct)
	require.NoError(t, err)
	directAllocation := al.allocateRouteSession(route, direct)
	ensureSessionMetadata(agent.Sessions, directAllocation.SessionKey, &directAllocation.Scope, directAllocation.SessionAliases)

	group := direct
	group.Context.ChatID = "-10077"
	group.Context.ChatType = "group"
	group.Context.TopicID = "13"
	groupAllocation := al.allocateRouteSession(route, group)
	first, err := catalog.CreateScopedSession(&groupAllocation.Scope, "First frozen dashboard session")
	require.NoError(t, err)
	second, err := catalog.CreateScopedSession(&groupAllocation.Scope, "Second next-turn dashboard session")
	require.NoError(t, err)

	_, query, ok := al.telegramSessionDashboard(&direct.Context, agent.ID, directAllocation.SessionKey, &directAllocation.Scope, directAllocation.SessionAliases)
	require.True(t, ok)
	require.NoError(t, dashboard.SetActiveDashboardSession(query, first.Key))

	frozen, _, dashboardAttached, ok := al.resolveSteeringTarget(direct)
	require.True(t, ok)
	require.True(t, dashboardAttached)
	require.Equal(t, first.Key, frozen)

	// This is exactly what Run does before handing the inbound to a worker.
	direct.SessionKey = frozen
	direct.Context.SessionDashboard = dashboardAttached
	require.NoError(t, dashboard.SetActiveDashboardSession(query, second.Key))

	stillFrozen, _, stillAttached, ok := al.resolveSteeringTarget(direct)
	require.True(t, ok)
	require.True(t, stillAttached)
	assert.Equal(t, first.Key, stillFrozen)

	target, err := al.buildContinuationTarget(direct)
	require.NoError(t, err)
	require.NotNil(t, target)
	assert.Equal(t, first.Key, target.SessionKey, "continuation must stay on the session frozen when the turn began")
	require.NotNil(t, target.InboundContext)
	assert.True(t, target.InboundContext.SessionDashboard)
	assert.Equal(t, second.Key, dashboard.ActiveDashboardSession(query), "mapping change is only for the next turn")
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

func TestPrivateTelegramSessionIsAutomaticPersonalDashboard(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	provider := &sessionCountingProvider{}
	agent := al.registry.GetDefaultAgent()
	agent.Provider = provider
	cfg.Agents.Defaults.ModelName = provider.GetDefaultModel()
	catalog := agent.Sessions.(session.ScopedSessionStore)

	mine := telegramSessionTestMessage("")
	route, _, err := al.resolveMessageRoute(mine)
	require.NoError(t, err)
	mineAllocation := al.allocateRouteSession(route, mine)
	mineRecord, err := catalog.CreateScopedSession(&mineAllocation.Scope, "Mine private")
	require.NoError(t, err)

	group := mine
	group.Context.ChatID = "-100100"
	group.Context.ChatType = "group"
	group.Context.TopicID = "7"
	groupAllocation := al.allocateRouteSession(route, group)
	groupRecord, err := catalog.CreateScopedSession(&groupAllocation.Scope, "Mine group topic")
	require.NoError(t, err)

	other := mine
	other.Context.ChatID = "99"
	other.Context.SenderID = "99"
	otherAllocation := al.allocateRouteSession(route, other)
	otherRecord, err := catalog.CreateScopedSession(&otherAllocation.Scope, "Other user")
	require.NoError(t, err)

	sharedAllocation := session.AllocateRouteSession(session.AllocationInput{
		AgentID:       agent.ID,
		Context:       group.Context,
		SessionPolicy: routing.SessionPolicy{Dimensions: []string{"chat"}},
	})
	sharedRecord, err := catalog.CreateScopedSession(&sharedAllocation.Scope, "Shared group")
	require.NoError(t, err)

	var structured *bus.StructuredContent
	response, err := al.processMessageWithStructured(context.Background(), telegramSessionTestMessage("/session"), &structured)
	require.NoError(t, err)
	require.NotNil(t, structured)
	assert.Contains(t, response, "Mine private")
	assert.Contains(t, response, "Mine group topic")
	assert.NotContains(t, response, "Other user")
	assert.NotContains(t, response, "Shared group")
	assert.Equal(t, "👤 Session Saya", structured.Title)
	assert.Zero(t, provider.calls.Load())

	for _, key := range []string{mineRecord.Key, groupRecord.Key, otherRecord.Key, sharedRecord.Key} {
		assert.NotContains(t, response, key, "full session keys must never be rendered")
	}
}

func TestConfiguredSuperadminOnlyGetsGlobalModeInPrivateChat(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	agent := al.registry.GetDefaultAgent()
	cfg.Dashboard.Superadmin = config.SessionSuperadminConfig{
		TelegramUserID: "42", BotAccount: "telegram", AgentID: agent.ID, Enabled: true,
	}
	catalog := agent.Sessions.(session.ScopedSessionStore)

	other := telegramSessionTestMessage("")
	other.Context.ChatID = "99"
	other.Context.SenderID = "99"
	route, _, err := al.resolveMessageRoute(other)
	require.NoError(t, err)
	otherAllocation := al.allocateRouteSession(route, other)
	_, err = catalog.CreateScopedSession(&otherAllocation.Scope, "User 99 session")
	require.NoError(t, err)

	var structured *bus.StructuredContent
	response, err := al.processMessageWithStructured(context.Background(), telegramSessionTestMessage("/session"), &structured)
	require.NoError(t, err)
	require.NotNil(t, structured)
	assert.Equal(t, "🌐 Mode Global Superadmin", structured.Title)
	assert.Contains(t, response, "User 99 session")
	require.Len(t, structured.Tables, 1)
	assert.Equal(t, []string{"No", "Nama Session", "Channel", "Account/Bot", "Agent", "Chat/Topic", "Owner", "Pesan", "Terakhir"}, structured.Tables[0].Columns)

	nonAdmin := telegramSessionTestMessage("/session")
	nonAdmin.Context.ChatID = "43"
	nonAdmin.Context.SenderID = "43"
	structured = nil
	_, err = al.processMessageWithStructured(context.Background(), nonAdmin, &structured)
	require.NoError(t, err)
	require.NotNil(t, structured)
	assert.Equal(t, "👤 Session Saya", structured.Title)

	group := telegramSessionTestMessage("/session")
	group.Context.ChatID = "-1001"
	group.Context.ChatType = "group"
	structured = nil
	_, err = al.processMessageWithStructured(context.Background(), group, &structured)
	require.NoError(t, err)
	require.NotNil(t, structured)
	assert.Equal(t, "Session", structured.Title, "superadmin in a group must remain route-scoped")
}

func TestPersonalDashboardSelectionDoesNotMutateOriginRouteAndRegularTurnUsesAttachment(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	provider := &sessionCountingProvider{}
	agent := al.registry.GetDefaultAgent()
	agent.Provider = provider
	cfg.Agents.Defaults.ModelName = provider.GetDefaultModel()
	catalog := agent.Sessions.(session.ScopedSessionStore)
	dashboard := agent.Sessions.(session.DashboardSessionStore)

	direct := telegramSessionTestMessage("")
	route, _, err := al.resolveMessageRoute(direct)
	require.NoError(t, err)
	directAllocation := al.allocateRouteSession(route, direct)
	ensureSessionMetadata(agent.Sessions, directAllocation.SessionKey, &directAllocation.Scope, directAllocation.SessionAliases)

	group := direct
	group.Context.ChatID = "-1001"
	group.Context.ChatType = "group"
	group.Context.TopicID = "9"
	groupAllocation := al.allocateRouteSession(route, group)
	origin, err := catalog.CreateScopedSession(&groupAllocation.Scope, "Deployment")
	require.NoError(t, err)
	otherOrigin, err := catalog.CreateScopedSession(&groupAllocation.Scope, "Origin local active")
	require.NoError(t, err)
	require.NoError(t, catalog.SetActiveScopedSession(&groupAllocation.Scope, groupAllocation.SessionAliases, otherOrigin.Key))

	_, query, ok := al.telegramSessionDashboard(&direct.Context, agent.ID, directAllocation.SessionKey, &directAllocation.Scope, directAllocation.SessionAliases)
	require.True(t, ok)
	require.NoError(t, dashboard.SetActiveDashboardSession(query, origin.Key))
	assert.Equal(t, otherOrigin.Key, catalog.ActiveScopedSession(&groupAllocation.Scope, groupAllocation.SessionAliases))

	turn := telegramSessionTestMessage("continue deployment")
	response, err := al.processMessage(context.Background(), turn)
	require.NoError(t, err)
	assert.Equal(t, "unexpected LLM response", response)
	assert.Equal(t, otherOrigin.Key, catalog.ActiveScopedSession(&groupAllocation.Scope, groupAllocation.SessionAliases), "origin route mapping must remain unchanged")

	history := agent.Sessions.GetHistory(origin.Key)
	require.NotEmpty(t, history)
	foundUser := false
	for _, message := range history {
		if message.Role == "user" && strings.Contains(message.Content, "continue deployment") {
			foundUser = true
		}
	}
	assert.True(t, foundUser, "regular private turn must append to dashboard-selected session")
	storedScope := agent.Sessions.(session.MetadataAwareSessionStore).GetSessionScope(origin.Key)
	require.NotNil(t, storedScope)
	assert.Equal(t, "-1001", storedScope.OriginChatID, "dashboard attachment must not rebind origin metadata to private chat")
	assert.Equal(t, "9", storedScope.OriginTopicID)
}

type blockingSessionProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockingSessionProvider) Chat(
	context.Context,
	[]providers.Message,
	[]providers.ToolDefinition,
	string,
	map[string]any,
) (*providers.LLMResponse, error) {
	p.once.Do(func() { close(p.started) })
	<-p.release
	return &providers.LLMResponse{Content: "frozen response"}, nil
}

func (*blockingSessionProvider) GetDefaultModel() string { return "session-blocking-test" }

func TestConcurrentPersonalDashboardSwitchDoesNotMixInFlightHistory(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	provider := &blockingSessionProvider{started: make(chan struct{}), release: make(chan struct{})}
	agent := al.registry.GetDefaultAgent()
	agent.Provider = provider
	cfg.Agents.Defaults.ModelName = provider.GetDefaultModel()
	catalog := agent.Sessions.(session.ScopedSessionStore)
	dashboard := agent.Sessions.(session.DashboardSessionStore)

	direct := telegramSessionTestMessage("")
	route, _, err := al.resolveMessageRoute(direct)
	require.NoError(t, err)
	directAllocation := al.allocateRouteSession(route, direct)
	ensureSessionMetadata(agent.Sessions, directAllocation.SessionKey, &directAllocation.Scope, directAllocation.SessionAliases)

	group := direct
	group.Context.ChatID = "-1007"
	group.Context.ChatType = "group"
	group.Context.TopicID = "11"
	groupAllocation := al.allocateRouteSession(route, group)
	first, err := catalog.CreateScopedSession(&groupAllocation.Scope, "First attached")
	require.NoError(t, err)
	second, err := catalog.CreateScopedSession(&groupAllocation.Scope, "Second attached")
	require.NoError(t, err)

	_, query, ok := al.telegramSessionDashboard(&direct.Context, agent.ID, directAllocation.SessionKey, &directAllocation.Scope, directAllocation.SessionAliases)
	require.True(t, ok)
	require.NoError(t, dashboard.SetActiveDashboardSession(query, first.Key))

	type result struct {
		text string
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		text, processErr := al.processMessage(context.Background(), telegramSessionTestMessage("frozen user turn"))
		resultCh <- result{text: text, err: processErr}
	}()

	<-provider.started
	require.NoError(t, dashboard.SetActiveDashboardSession(query, second.Key))
	close(provider.release)
	got := <-resultCh
	require.NoError(t, got.err)
	assert.Equal(t, "frozen response", got.text)

	firstHistory := agent.Sessions.GetHistory(first.Key)
	secondHistory := agent.Sessions.GetHistory(second.Key)
	firstJoined := ""
	for _, message := range firstHistory {
		firstJoined += "\n" + message.Content
	}
	secondJoined := ""
	for _, message := range secondHistory {
		secondJoined += "\n" + message.Content
	}
	assert.Contains(t, firstJoined, "frozen user turn")
	assert.Contains(t, firstJoined, "frozen response")
	assert.NotContains(t, secondJoined, "frozen user turn")
	assert.NotContains(t, secondJoined, "frozen response")
	assert.Equal(t, second.Key, dashboard.ActiveDashboardSession(query), "next turn should use the newly selected dashboard mapping")
}

func TestDashboardCallbackModeIsBoundToCurrentAuthorization(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	agent := al.registry.GetDefaultAgent()
	msg := telegramSessionTestMessage("")
	route, _, err := al.resolveMessageRoute(msg)
	require.NoError(t, err)
	allocation := al.allocateRouteSession(route, msg)
	ensureSessionMetadata(agent.Sessions, allocation.SessionKey, &allocation.Scope, allocation.SessionAliases)

	base := bus.InternalCallbackRequest{
		Kind:          "session",
		Action:        "noop",
		OwnerID:       "42",
		Channel:       "telegram",
		Account:       "bot-a",
		ChatID:        "42",
		AgentID:       agent.ID,
		Scope:         session.CanonicalScopeSignature(allocation.Scope),
		DashboardMode: string(session.DashboardModePersonal),
		Inbound:       msg.Context,
	}
	_, err = al.handleInternalCallback(context.Background(), base)
	require.NoError(t, err)

	cfg.Dashboard.Superadmin = config.SessionSuperadminConfig{
		TelegramUserID: "42", BotAccount: "telegram", AgentID: agent.ID, Enabled: true,
	}
	_, err = al.handleInternalCallback(context.Background(), base)
	require.Error(t, err, "a menu minted in personal mode must be rejected after authorization changes")

	base.DashboardMode = string(session.DashboardModeSuperadmin)
	_, err = al.handleInternalCallback(context.Background(), base)
	require.NoError(t, err)

	cfg.Dashboard.Superadmin = config.SessionSuperadminConfig{}
	_, err = al.handleInternalCallback(context.Background(), base)
	require.Error(t, err, "a stale superadmin menu must be rejected after the grant is removed")
}

func TestSessionCurrentShowsDashboardModeOriginOwnerAndShortID(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	agent := al.registry.GetDefaultAgent()
	catalog := agent.Sessions.(session.ScopedSessionStore)
	dashboard := agent.Sessions.(session.DashboardSessionStore)

	direct := telegramSessionTestMessage("")
	route, _, err := al.resolveMessageRoute(direct)
	require.NoError(t, err)
	directAllocation := al.allocateRouteSession(route, direct)
	ensureSessionMetadata(agent.Sessions, directAllocation.SessionKey, &directAllocation.Scope, directAllocation.SessionAliases)

	group := direct
	group.Context.ChatID = "-100500"
	group.Context.ChatType = "group"
	group.Context.TopicID = "88"
	groupAllocation := al.allocateRouteSession(route, group)
	record, err := catalog.CreateScopedSession(&groupAllocation.Scope, "Current target")
	require.NoError(t, err)
	_, query, ok := al.telegramSessionDashboard(&direct.Context, agent.ID, directAllocation.SessionKey, &directAllocation.Scope, directAllocation.SessionAliases)
	require.True(t, ok)
	require.NoError(t, dashboard.SetActiveDashboardSession(query, record.Key))

	var structured *bus.StructuredContent
	response, err := al.processMessageWithStructured(context.Background(), telegramSessionTestMessage("/session current"), &structured)
	require.NoError(t, err)
	require.NotNil(t, structured)
	assert.Contains(t, response, "Current target")
	assert.Contains(t, response, record.ShortID)
	assert.Contains(t, response, "-100500 / 88")
	assert.Contains(t, response, "Owner: 42")
	assert.Contains(t, response, "Mode: Personal")
	assert.NotContains(t, response, record.Key)
}

func TestNonTelegramSessionCommandKeepsRouteScopedBehavior(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	msg := bus.InboundMessage{Context: bus.InboundContext{
		Channel: "slack", Account: "workspace-a", ChatID: "D42", ChatType: "direct", SenderID: "42",
	}, Content: "/session"}
	var structured *bus.StructuredContent
	_, err := al.processMessageWithStructured(context.Background(), msg, &structured)
	require.NoError(t, err)
	require.NotNil(t, structured)
	assert.Equal(t, "Session", structured.Title)
	assert.Equal(t, []string{"No", "Nama Session", "Pesan", "Terakhir"}, structured.Tables[0].Columns)
}
