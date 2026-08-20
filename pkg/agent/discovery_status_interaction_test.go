package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func TestShowDashboardUsesSessionAwareModelSemantic(t *testing.T) {
	inbound := bus.InboundContext{
		Channel: "telegram", Account: "default", ChatID: "42", ChatType: "direct", SenderID: "42",
	}
	scope := &session.SessionScope{Version: 1, AgentID: "main", Channel: "telegram", Account: "default"}
	opts := &processOptions{Dispatch: DispatchRequest{
		SessionKey: "si_v1_bound", SessionScope: scope, InboundContext: &inbound,
	}}
	rt := &commands.Runtime{
		ModelCommand: func(_ context.Context, req commands.ModelCommandRequest) (*bus.StructuredContent, error) {
			require.Equal(t, "current", req.Operation)
			return &bus.StructuredContent{Tables: []bus.StructuredTable{{
				Columns: []string{"Properti", "Nilai"},
				Rows: [][]string{
					{"Provider", "provider-a"},
					{"Alias", "session-alias"},
					{"Model", "session-override"},
				},
			}}}, nil
		},
		GetModelInfo: func() (string, string) {
			t.Fatal("show dashboard must not prefer legacy model getter over session semantic")
			return "", ""
		},
	}
	content, err := (&AgentLoop{}).executeDiscoveryCommand(
		context.Background(), &AgentInstance{ID: "main"}, opts, rt,
		commands.DiscoveryCommandRequest{Domain: "show", Operation: "dashboard"},
	)
	require.NoError(t, err)
	require.NotNil(t, content)
	assert.Equal(t, "session-override", structuredProperty(content, "Model"))
	assert.Equal(t, "provider-a", structuredProperty(content, "Provider"))
	assert.Equal(t, "telegram", structuredProperty(content, "Channel"))
	require.NotNil(t, content.Interaction)
	assert.Equal(t, "discovery", content.Interaction.Kind)
	assert.Equal(t, "si_v1_bound", content.Interaction.SessionKey)
}

func TestDiscoveryDelegatesModelAndSkillCatalogDomains(t *testing.T) {
	modelCalled := false
	skillCalled := false
	rt := &commands.Runtime{
		ModelCommand: func(_ context.Context, req commands.ModelCommandRequest) (*bus.StructuredContent, error) {
			modelCalled = true
			require.Equal(t, "list", req.Operation)
			return &bus.StructuredContent{
				Fallback: "models",
				Interaction: &bus.InteractionMenu{Kind: "model", SessionKey: "si_v1_bound"},
			}, nil
		},
		SkillCommand: func(_ context.Context, req commands.SkillCommandRequest) (*bus.StructuredContent, error) {
			skillCalled = true
			require.Equal(t, "dashboard", req.Operation)
			return &bus.StructuredContent{
				Fallback: "skills",
				Interaction: &bus.InteractionMenu{Kind: "skill", SessionKey: "si_v1_bound"},
			}, nil
		},
	}
	models, err := delegateModelCatalog(context.Background(), rt)
	require.NoError(t, err)
	require.NotNil(t, models.Interaction)
	assert.Equal(t, "model", models.Interaction.Kind)

	skills, err := delegateSkillCatalog(context.Background(), rt)
	require.NoError(t, err)
	require.NotNil(t, skills.Interaction)
	assert.Equal(t, "skill", skills.Interaction.Kind)
	assert.True(t, modelCalled)
	assert.True(t, skillCalled)
}

func TestDiscoveryPaginationIsBoundedAndDeterministic(t *testing.T) {
	page, pages, start, end := discoveryPageWindow(13, 2)
	assert.Equal(t, 2, page)
	assert.Equal(t, 3, pages)
	assert.Equal(t, 10, start)
	assert.Equal(t, 13, end)

	page, pages, start, end = discoveryPageWindow(13, 999)
	assert.Equal(t, 2, page)
	assert.Equal(t, 3, pages)
	assert.Equal(t, 10, start)
	assert.Equal(t, 13, end)

	assert.Equal(t,
		[]string{"Alpha", "alpha", "beta", "Zulu"},
		sortedStrings([]string{"Zulu", "beta", "alpha", "Alpha"}),
	)
}

func TestDiscoveryCallbackIsSessionBoundAndReplaceCurrent(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}

	msg := telegramSessionTestMessage("")
	route, agent, err := al.resolveMessageRoute(msg)
	require.NoError(t, err)
	allocation := al.allocateRouteSession(route, msg)
	ensureSessionMetadata(agent.Sessions, allocation.SessionKey, &allocation.Scope, allocation.SessionAliases)

	base := bus.InternalCallbackRequest{
		Kind:       "discovery",
		Action:     "list_dashboard",
		OwnerID:    msg.Context.SenderID,
		Channel:    msg.Context.Channel,
		Account:    msg.Context.Account,
		ChatID:     msg.Context.ChatID,
		TopicID:    msg.Context.TopicID,
		AgentID:    agent.ID,
		SessionKey: allocation.SessionKey,
		Scope:      session.CanonicalScopeSignature(allocation.Scope),
		Inbound:    msg.Context,
	}
	response, err := al.handleInternalCallback(context.Background(), base)
	require.NoError(t, err)
	require.NotNil(t, response)
	require.NotNil(t, response.Content)
	require.NotNil(t, response.Content.Interaction)
	assert.Equal(t, bus.InteractionReplaceCurrent, response.Transition)
	assert.Equal(t, "discovery", response.Content.Interaction.Kind)
	assert.Equal(t, allocation.SessionKey, response.Content.Interaction.SessionKey)

	tests := []struct {
		name   string
		mutate func(*bus.InternalCallbackRequest)
	}{
		{name: "wrong owner", mutate: func(req *bus.InternalCallbackRequest) { req.OwnerID = "99" }},
		{name: "wrong channel", mutate: func(req *bus.InternalCallbackRequest) { req.Channel = "discord" }},
		{name: "wrong chat", mutate: func(req *bus.InternalCallbackRequest) { req.ChatID = "99" }},
		{name: "wrong topic", mutate: func(req *bus.InternalCallbackRequest) { req.TopicID = "7" }},
		{name: "wrong account", mutate: func(req *bus.InternalCallbackRequest) { req.Account = "bot-b" }},
		{name: "wrong agent", mutate: func(req *bus.InternalCallbackRequest) { req.AgentID = "other" }},
		{name: "wrong scope", mutate: func(req *bus.InternalCallbackRequest) { req.Scope = "foreign-scope" }},
		{name: "wrong session", mutate: func(req *bus.InternalCallbackRequest) { req.SessionKey = "si_v1_foreign" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			tt.mutate(&req)
			_, callbackErr := al.handleInternalCallback(context.Background(), req)
			require.Error(t, callbackErr)
		})
	}
}

func TestDiscoveryCallbackRejectsUnknownActionAndKind(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	msg := telegramSessionTestMessage("")
	route, agent, err := al.resolveMessageRoute(msg)
	require.NoError(t, err)
	allocation := al.allocateRouteSession(route, msg)
	ensureSessionMetadata(agent.Sessions, allocation.SessionKey, &allocation.Scope, allocation.SessionAliases)

	req := bus.InternalCallbackRequest{
		Kind: "discovery", Action: "not_allowed", OwnerID: msg.Context.SenderID,
		Channel: msg.Context.Channel, Account: msg.Context.Account, ChatID: msg.Context.ChatID,
		TopicID: msg.Context.TopicID, AgentID: agent.ID, SessionKey: allocation.SessionKey,
		Scope: session.CanonicalScopeSignature(allocation.Scope), Inbound: msg.Context,
	}
	_, err = al.handleInternalCallback(context.Background(), req)
	require.Error(t, err)

	req.Kind = "unknown-domain"
	_, err = al.handleInternalCallback(context.Background(), req)
	require.Error(t, err)
}

func TestChannelCheckContentUsesReadOnlyStatusAndRefreshesWithReplaceCurrent(t *testing.T) {
	inbound := bus.InboundContext{
		Channel: "telegram", Account: "default", ChatID: "42", ChatType: "direct", SenderID: "42",
	}
	scope := &session.SessionScope{Version: 1, AgentID: "main", Channel: "telegram", Account: "default"}
	opts := &processOptions{Dispatch: DispatchRequest{
		SessionKey: "si_v1_bound", SessionScope: scope, InboundContext: &inbound,
	}}
	calls := 0
	rt := &commands.Runtime{CheckChannel: func(name string) (commands.ChannelStatus, error) {
		calls++
		return commands.ChannelStatus{Name: name, Enabled: true, Available: false, Reason: "not running"}, nil
	}}
	content, err := (&AgentLoop{}).buildChannelCheck(&AgentInstance{ID: "main"}, opts, rt, "telegram")
	require.NoError(t, err)
	require.NotNil(t, content)
	assert.Equal(t, "yes", structuredProperty(content, "Enabled"))
	assert.Equal(t, "no", structuredProperty(content, "Available"))
	require.NotNil(t, content.Interaction)
	assert.Equal(t, "discovery", content.Interaction.Kind)
	assert.Equal(t, "check_channel_refresh", content.Interaction.Entries[0].Action)
	assert.Equal(t, "telegram", content.Interaction.Entries[0].Value)
	assert.Equal(t, 1, calls)
}
