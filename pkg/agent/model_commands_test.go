package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func TestModelListUsesPageLocalNumericSelectors(t *testing.T) {
	models := []modelSelection{
		{Provider: "openai", Model: "model-1", ConfigRef: "one", Source: "configured"},
		{Provider: "openai", Model: "model-2", ConfigRef: "two", Source: "configured"},
		{Provider: "openai", Model: "model-3", ConfigRef: "three", Source: "configured"},
		{Provider: "openai", Model: "model-4", ConfigRef: "four", Source: "configured"},
		{Provider: "openai", Model: "model-5", ConfigRef: "five", Source: "configured"},
		{Provider: "openai", Model: "model-6", ConfigRef: "six", Source: "configured"},
		{Provider: "openai", Model: "model-7", ConfigRef: "seven", Source: "configured"},
	}
	content := buildModelListContent(
		modelCommandContext{Agent: &AgentInstance{ID: "main"}, SessionKey: "session-a"},
		"Configured Models",
		"configured",
		models,
		effectiveModelInfo{},
		1,
		&config.Config{},
		nil,
	)
	require.NotNil(t, content)
	require.Len(t, content.Tables, 1)
	require.Len(t, content.Tables[0].Rows, 2)
	assert.Equal(t, "1", content.Tables[0].Rows[0][0])
	assert.Equal(t, "2", content.Tables[0].Rows[1][0])
	require.NotNil(t, content.Interaction)

	labels := make([]string, 0, 2)
	for _, entry := range content.Interaction.Entries {
		if entry.Action == "select" {
			labels = append(labels, entry.Label)
		}
	}
	assert.Equal(t, []string{"1", "2"}, labels)
	assert.Equal(t, 1, content.Interaction.Page)
	assert.Equal(t, 2, content.Interaction.Pages)
}

func TestModelDashboardIncludesNativeSearchAndInformativeFallback(t *testing.T) {
	al := &AgentLoop{}
	store := session.NewSessionManager("")
	cfg := &config.Config{}
	cfg.Agents.Defaults.Provider = "openai"
	content := al.buildModelDashboard(
		modelCommandContext{
			Agent:      &AgentInstance{ID: "main", Model: "default-model"},
			SessionKey: "session-a",
		},
		store,
		cfg,
	)

	require.NotNil(t, content.Interaction)
	actions := make([]string, 0, len(content.Interaction.Entries))
	for _, entry := range content.Interaction.Entries {
		actions = append(actions, entry.Action)
	}
	assert.Contains(t, actions, "search")
	assert.Contains(t, content.Fallback, "/model use <alias-or-model>")
	assert.Contains(t, content.Fallback, "/model search <kata>")
}

func TestInternalModelCallbackRejectsCrossScopeCapabilities(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	msg := telegramSessionTestMessage("")
	route, agent, err := al.resolveMessageRoute(msg)
	require.NoError(t, err)
	allocation := al.allocateRouteSession(route, msg)
	ensureSessionMetadata(agent.Sessions, allocation.SessionKey, &allocation.Scope, allocation.SessionAliases)

	base := bus.InternalCallbackRequest{
		Kind:       "model",
		Action:     "dashboard",
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

	tests := []struct {
		name   string
		mutate func(*bus.InternalCallbackRequest)
	}{
		{name: "wrong sender", mutate: func(req *bus.InternalCallbackRequest) { req.OwnerID = "99" }},
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
