package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func TestModelSearchCallbackKeepsBoundSessionAfterActiveSessionChanges(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	cfg.ModelList = configuredSearchModels()
	cfg.Agents.Defaults.Provider = "openai"

	msg := telegramSessionTestMessage("")
	route, agent, err := al.resolveMessageRoute(msg)
	require.NoError(t, err)
	allocation := al.allocateRouteSession(route, msg)
	ensureSessionMetadata(
		agent.Sessions,
		allocation.SessionKey,
		&allocation.Scope,
		allocation.SessionAliases,
	)
	catalog := agent.Sessions.(session.ScopedSessionStore)
	sessionA, err := catalog.CreateScopedSession(&allocation.Scope, "Session A")
	require.NoError(t, err)
	sessionB, err := catalog.CreateScopedSession(&allocation.Scope, "Session B")
	require.NoError(t, err)
	require.NoError(
		t,
		catalog.SetActiveScopedSession(
			&allocation.Scope,
			allocation.SessionAliases,
			sessionA.Key,
		),
	)

	request := bus.InternalCallbackRequest{
		Kind:       "model",
		Action:     "search",
		Value:      "gpt",
		OwnerID:    msg.Context.SenderID,
		Channel:    msg.Context.Channel,
		Account:    msg.Context.Account,
		ChatID:     msg.Context.ChatID,
		TopicID:    msg.Context.TopicID,
		AgentID:    agent.ID,
		SessionKey: sessionA.Key,
		Scope:      session.CanonicalScopeSignature(allocation.Scope),
		Inbound:    msg.Context,
	}

	require.NoError(
		t,
		catalog.SetActiveScopedSession(
			&allocation.Scope,
			allocation.SessionAliases,
			sessionB.Key,
		),
	)
	response, err := al.handleInternalCallback(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, response)
	require.NotNil(t, response.Content)
	require.NotNil(t, response.Content.Interaction)
	assert.Equal(t, sessionA.Key, response.Content.Interaction.SessionKey)
	assert.Equal(t, bus.InteractionAppendContinuation, response.Transition)
	assert.Equal(
		t,
		sessionB.Key,
		catalog.ActiveScopedSession(&allocation.Scope, allocation.SessionAliases),
		"bound callback must not silently retarget to the newly active session",
	)
}
