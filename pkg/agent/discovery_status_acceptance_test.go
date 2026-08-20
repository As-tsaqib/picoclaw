package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func discoveryBoundRequest(t *testing.T, al *AgentLoop) (bus.InternalCallbackRequest, *AgentInstance) {
	t.Helper()
	msg := telegramSessionTestMessage("")
	route, agent, err := al.resolveMessageRoute(msg)
	require.NoError(t, err)
	allocation := al.allocateRouteSession(route, msg)
	ensureSessionMetadata(agent.Sessions, allocation.SessionKey, &allocation.Scope, allocation.SessionAliases)
	return bus.InternalCallbackRequest{
		Kind:       "discovery",
		OwnerID:    msg.Context.SenderID,
		Channel:    msg.Context.Channel,
		Account:    msg.Context.Account,
		ChatID:     msg.Context.ChatID,
		TopicID:    msg.Context.TopicID,
		AgentID:    agent.ID,
		SessionKey: allocation.SessionKey,
		Scope:      session.CanonicalScopeSignature(allocation.Scope),
		Inbound:    msg.Context,
	}, agent
}

func TestDiscoveryRefreshCallbacksUseReplaceCurrent(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	base, _ := discoveryBoundRequest(t, al)

	for _, tc := range []struct {
		name   string
		action string
		value  string
		kind   string
	}{
		{name: "show dashboard refresh", action: "show_dashboard", kind: "current_state"},
		{name: "check channel refresh", action: "check_channel_refresh", value: "cli", kind: "channel_status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			req.Action = tc.action
			req.Value = tc.value
			response, err := al.handleInternalCallback(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.Content)
			assert.Equal(t, bus.InteractionReplaceCurrent, response.Transition)
			assert.Equal(t, tc.kind, response.Content.Kind)
			require.NotNil(t, response.Content.Interaction)
			assert.Equal(t, base.SessionKey, response.Content.Interaction.SessionKey)
			if tc.action == "check_channel_refresh" {
				assert.Equal(t, "cli", structuredProperty(response.Content, "Channel"))
				assert.Equal(t, "yes", structuredProperty(response.Content, "Enabled"))
				assert.Equal(t, "yes", structuredProperty(response.Content, "Available"))
			}
		})
	}
}

func TestDiscoveryCategoryHandoffKeepsBoundSessionAuthority(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	base, _ := discoveryBoundRequest(t, al)

	for _, tc := range []struct {
		name       string
		action     string
		wantedKind string
	}{
		{name: "models", action: "list_models", wantedKind: "model"},
		{name: "skills", action: "list_skills", wantedKind: "skill"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			req.Action = tc.action
			response, err := al.handleInternalCallback(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.Content)
			require.NotNil(t, response.Content.Interaction)
			assert.Equal(t, bus.InteractionReplaceCurrent, response.Transition)
			assert.Equal(t, tc.wantedKind, response.Content.Interaction.Kind)
			assert.Equal(t, base.SessionKey, response.Content.Interaction.SessionKey)
			assert.Equal(t, base.OwnerID, response.Content.Interaction.OwnerID)
			assert.Equal(t, base.Channel, response.Content.Interaction.Channel)
			assert.Equal(t, base.Account, response.Content.Interaction.Account)
			assert.Equal(t, base.ChatID, response.Content.Interaction.ChatID)
			assert.Equal(t, base.TopicID, response.Content.Interaction.TopicID)
			assert.Equal(t, base.AgentID, response.Content.Interaction.AgentID)
			assert.Equal(t, base.Scope, response.Content.Interaction.Scope)
		})
	}
}
