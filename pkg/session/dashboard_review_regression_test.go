package session_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func recordKeys(records []session.SessionRecord) map[string]bool {
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		seen[record.Key] = true
	}
	return seen
}

func TestPersonalDashboardRejectsIdentityLinkedCanonicalLegacySender(t *testing.T) {
	backend, _ := newCatalogBackend(t, t.TempDir())
	t.Cleanup(func() { _ = backend.Close() })

	// Reproduce a legacy sender-scoped Telegram session where raw user 99 was
	// canonicalized through identity_links to sender value 42. There is no raw
	// durable owner metadata, so Telegram user 42 must not be allowed to claim it.
	legacyScope := &session.SessionScope{
		Version:    session.ScopeVersionV1,
		AgentID:    "main",
		Channel:    "telegram",
		Account:    "bot-a",
		Dimensions: []string{"chat", "sender"},
		Values: map[string]string{
			"chat":   "group:-1001",
			"sender": "42",
		},
		Platform:   "telegram",
		BotAccount: "telegram",
	}
	key := session.BuildSessionKey(*legacyScope)
	backend.EnsureSessionMetadata(key, legacyScope, nil)
	backend.AddMessage(key, "user", "private history owned by raw Telegram user 99")

	query := dashboardQuery(session.DashboardModePersonal, "42")
	records, err := backend.ListDashboardSessions(query)
	require.NoError(t, err)
	assert.False(t, recordKeys(records)[key])
	assert.ErrorIs(t, backend.SetActiveDashboardSession(query, key), session.ErrSessionNotInScope)
}

func TestPersonalDashboardLegacyDirectChatUsesRawChatProofNotCanonicalSender(t *testing.T) {
	backend, _ := newCatalogBackend(t, t.TempDir())
	t.Cleanup(func() { _ = backend.Close() })

	legacyScope := &session.SessionScope{
		Version:    session.ScopeVersionV1,
		AgentID:    "main",
		Channel:    "telegram",
		Account:    "bot-a",
		Dimensions: []string{"chat", "sender"},
		Values: map[string]string{
			"chat":   "direct:99",
			"sender": "42",
		},
		Platform:   "telegram",
		BotAccount: "telegram",
	}
	key := session.BuildSessionKey(*legacyScope)
	backend.EnsureSessionMetadata(key, legacyScope, nil)
	backend.AddMessage(key, "user", "raw 99 direct history")

	records, err := backend.ListDashboardSessions(dashboardQuery(session.DashboardModePersonal, "42"))
	require.NoError(t, err)
	assert.False(t, recordKeys(records)[key], "canonical sender 42 must not override raw direct chat 99")

	records, err = backend.ListDashboardSessions(dashboardQuery(session.DashboardModePersonal, "99"))
	require.NoError(t, err)
	assert.True(t, recordKeys(records)[key], "raw direct chat ID remains valid legacy owner proof")
}

func TestDashboardFiltersHiddenMetadataBeforeReadingHistory(t *testing.T) {
	backend, store := newCatalogBackend(t, t.TempDir())
	t.Cleanup(func() { _ = backend.Close() })

	foreignScope := dashboardScope("main", "bot-a", "telegram", "99", "", "99", true)
	key := session.BuildSessionKey(*foreignScope)
	backend.EnsureSessionMetadata(key, foreignScope, nil)
	backend.AddMessage(key, "user", "this would become the fallback session name if history were read")

	before, err := store.GetSessionMeta(context.Background(), key)
	require.NoError(t, err)
	require.Empty(t, before.Name)

	records, err := backend.ListDashboardSessions(dashboardQuery(session.DashboardModePersonal, "42"))
	require.NoError(t, err)
	assert.False(t, recordKeys(records)[key])

	after, err := store.GetSessionMeta(context.Background(), key)
	require.NoError(t, err)
	assert.Empty(t, after.Name, "hidden session metadata must not be mutated by /session listing")
	assert.Empty(t, after.NameSource)
}

func TestLegacyUnknownDashboardOptInStillEnforcesAgentAccountAndBot(t *testing.T) {
	backend, _ := newCatalogBackend(t, t.TempDir())
	t.Cleanup(func() { _ = backend.Close() })

	allowed := "agent:main:telegram:bot-a:direct:99"
	wrongAgent := "agent:other:telegram:bot-a:direct:99"
	wrongAccount := "agent:main:telegram:bot-b:direct:99"
	wrongBot := "agent:main:telegram-secondary:bot-a:direct:99"
	ambiguous := "agent:main:telegram:direct:99"
	for _, key := range []string{allowed, wrongAgent, wrongAccount, wrongBot, ambiguous} {
		backend.AddMessage(key, "user", "legacy")
	}

	query := dashboardQuery(session.DashboardModeSuperadmin, "42")
	query.IncludeLegacyUnknown = true
	records, err := backend.ListDashboardSessions(query)
	require.NoError(t, err)
	seen := recordKeys(records)
	assert.True(t, seen[allowed])
	assert.False(t, seen[wrongAgent])
	assert.False(t, seen[wrongAccount])
	assert.False(t, seen[wrongBot])
	assert.False(t, seen[ambiguous])
}

func TestSessionManagerSupportsPrivateDashboardFallback(t *testing.T) {
	dir := t.TempDir()
	manager := session.NewSessionManager(dir)

	own, err := manager.CreateScopedSession(
		dashboardScope("main", "bot-a", "telegram", "42", "", "42", true),
		"Own fallback session",
	)
	require.NoError(t, err)
	other, err := manager.CreateScopedSession(
		dashboardScope("main", "bot-a", "telegram", "99", "", "99", true),
		"Other fallback session",
	)
	require.NoError(t, err)

	query := dashboardQuery(session.DashboardModePersonal, "42")
	records, err := manager.ListDashboardSessions(query)
	require.NoError(t, err)
	seen := recordKeys(records)
	assert.True(t, seen[own.Key])
	assert.False(t, seen[other.Key])

	require.NoError(t, manager.SetActiveDashboardSession(query, own.Key))
	assert.Equal(t, own.Key, manager.ActiveDashboardSession(query))
	require.NoError(t, manager.RenameDashboardSession(query, own.Key, "Renamed fallback"))
	require.NoError(t, manager.Close())

	reopened := session.NewSessionManager(dir)
	t.Cleanup(func() { _ = reopened.Close() })
	assert.Equal(t, own.Key, reopened.ActiveDashboardSession(query), "dashboard mapping must survive fallback-store restart")
	records, err = reopened.ListDashboardSessions(query)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "Renamed fallback", records[0].Name)
}
