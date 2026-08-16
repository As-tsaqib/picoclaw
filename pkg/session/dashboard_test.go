package session_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func dashboardScope(agent, routingAccount, bot, chat, topic, owner string, senderScoped bool) *session.SessionScope {
	dimensions := []string{"chat"}
	values := map[string]string{"chat": "group:" + chat}
	chatType := "group"
	if chat == owner && topic == "" {
		values["chat"] = "direct:" + chat
		chatType = "direct"
	}
	if topic != "" {
		dimensions = append(dimensions, "topic")
		values["topic"] = "topic:" + topic
	}
	if senderScoped {
		dimensions = append(dimensions, "sender")
		values["sender"] = owner
	}
	return &session.SessionScope{
		Version:        session.ScopeVersionV1,
		AgentID:        agent,
		Channel:        bot,
		Account:        routingAccount,
		Dimensions:     dimensions,
		Values:         values,
		OwnerUserID:    owner,
		OriginChannel:  "telegram",
		OriginAccount:  routingAccount,
		OriginAgentID:  agent,
		OriginChatID:   chat,
		OriginTopicID:  topic,
		OriginSenderID: owner,
		OriginChatType: chatType,
		Platform:       "telegram",
		BotAccount:     bot,
	}
}

func dashboardQuery(mode session.DashboardMode, owner string) session.DashboardQuery {
	return session.DashboardQuery{
		Mode:        mode,
		OwnerUserID: owner,
		ChatID:      owner,
		BotAccount:  "telegram",
		Account:     "bot-a",
		AgentID:     "main",
	}
}

func TestPersonalDashboardOnlyListsVerifiedOwnerSessions(t *testing.T) {
	backend, _ := newCatalogBackend(t, t.TempDir())
	t.Cleanup(func() { _ = backend.Close() })

	private, err := backend.CreateScopedSession(
		dashboardScope("main", "bot-a", "telegram", "42", "", "42", true),
		"Private",
	)
	require.NoError(t, err)
	group, err := backend.CreateScopedSession(
		dashboardScope("main", "bot-a", "telegram", "-1001", "7", "42", true),
		"Group topic",
	)
	require.NoError(t, err)
	other, err := backend.CreateScopedSession(
		dashboardScope("main", "bot-a", "telegram", "99", "", "99", true),
		"Other user",
	)
	require.NoError(t, err)
	otherBot, err := backend.CreateScopedSession(
		dashboardScope("main", "bot-a", "telegram-secondary", "42", "", "42", true),
		"Other bot",
	)
	require.NoError(t, err)
	otherAgent, err := backend.CreateScopedSession(
		dashboardScope("other", "bot-a", "telegram", "42", "", "42", true),
		"Other agent",
	)
	require.NoError(t, err)

	sharedScope := dashboardScope("main", "bot-a", "telegram", "-1002", "", "", false)
	sharedScope.OwnerUserID = ""
	sharedScope.OriginSenderID = "42"
	shared, err := backend.CreateScopedSession(sharedScope, "Shared group")
	require.NoError(t, err)

	records, err := backend.ListDashboardSessions(dashboardQuery(session.DashboardModePersonal, "42"))
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, record := range records {
		seen[record.Key] = true
	}
	assert.True(t, seen[private.Key])
	assert.True(t, seen[group.Key], "sender-owned group/topic session should be discoverable")
	assert.False(t, seen[other.Key], "other user's session must never appear")
	assert.False(t, seen[otherBot.Key], "other bot account must remain isolated")
	assert.False(t, seen[otherAgent.Key], "other agent must remain isolated")
	assert.False(t, seen[shared.Key], "shared group must not be claimed from sender metadata alone")
}

func TestDashboardMappingsAreSeparateAndPersist(t *testing.T) {
	dir := t.TempDir()
	backend, _ := newCatalogBackend(t, dir)
	originScope := dashboardScope("main", "bot-a", "telegram", "-1001", "7", "42", true)
	origin, err := backend.CreateScopedSession(originScope, "Origin")
	require.NoError(t, err)
	other, err := backend.CreateScopedSession(originScope, "Other")
	require.NoError(t, err)
	require.NoError(t, backend.SetActiveScopedSession(originScope, nil, origin.Key))

	personal := dashboardQuery(session.DashboardModePersonal, "42")
	superadmin := dashboardQuery(session.DashboardModeSuperadmin, "42")
	require.NoError(t, backend.SetActiveDashboardSession(personal, other.Key))
	require.NoError(t, backend.SetActiveDashboardSession(superadmin, other.Key))
	assert.Equal(
		t,
		origin.Key,
		backend.ActiveScopedSession(originScope, nil),
		"personal/superadmin selection must not mutate origin route mapping",
	)
	require.NoError(t, backend.Close())

	reopened, _ := newCatalogBackend(t, dir)
	t.Cleanup(func() { _ = reopened.Close() })
	assert.Equal(t, other.Key, reopened.ActiveDashboardSession(personal))
	assert.Equal(t, other.Key, reopened.ActiveDashboardSession(superadmin))
	assert.Equal(t, origin.Key, reopened.ActiveScopedSession(originScope, nil))
}

func TestSuperadminCatalogRespectsAgentBotAndExplicitLegacyOptIn(t *testing.T) {
	backend, _ := newCatalogBackend(t, t.TempDir())
	t.Cleanup(func() { _ = backend.Close() })

	allowed, err := backend.CreateScopedSession(
		dashboardScope("main", "bot-a", "telegram", "99", "", "99", true),
		"Allowed",
	)
	require.NoError(t, err)
	foreignAgent, err := backend.CreateScopedSession(
		dashboardScope("other", "bot-a", "telegram", "99", "", "99", true),
		"Foreign agent",
	)
	require.NoError(t, err)
	foreignBot, err := backend.CreateScopedSession(
		dashboardScope("main", "bot-a", "telegram-secondary", "99", "", "99", true),
		"Foreign bot",
	)
	require.NoError(t, err)
	backend.AddMessage("agent:main:legacy-without-scope", "user", "legacy")

	query := dashboardQuery(session.DashboardModeSuperadmin, "42")
	records, err := backend.ListDashboardSessions(query)
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, record := range records {
		seen[record.Key] = true
	}
	assert.True(t, seen[allowed.Key])
	assert.False(t, seen[foreignAgent.Key])
	assert.False(t, seen[foreignBot.Key])
	assert.False(t, seen["agent:main:legacy-without-scope"])

	query.IncludeLegacyUnknown = true
	records, err = backend.ListDashboardSessions(query)
	require.NoError(t, err)
	legacyFound := false
	for _, record := range records {
		if record.Key == "agent:main:legacy-without-scope" {
			legacyFound = true
			assert.True(t, record.LegacyUnknown)
		}
	}
	assert.True(t, legacyFound, "legacy/unknown must require an explicit opt-in")
}
