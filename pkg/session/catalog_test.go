package session_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/memory"
	"github.com/As-tsaqib/picoclaw/pkg/providers"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func telegramScope(agent, account, chat, topic, sender string, private bool) *session.SessionScope {
	dimensions := []string{"chat"}
	values := map[string]string{"chat": "direct:" + chat}
	if topic != "" {
		dimensions = append(dimensions, "topic")
		values["topic"] = "topic:" + topic
	}
	if private {
		dimensions = append(dimensions, "sender")
		values["sender"] = sender
	}
	return &session.SessionScope{
		Version: session.ScopeVersionV1, AgentID: agent, Channel: "telegram", Account: account,
		Dimensions: dimensions, Values: values, PrivateResponse: private,
	}
}

func newCatalogBackend(t *testing.T, dir string) (*session.JSONLBackend, *memory.JSONLStore) {
	t.Helper()
	store, err := memory.NewJSONLStore(dir)
	require.NoError(t, err)
	return session.NewJSONLBackend(store), store
}

func TestNamedSessionsAndActiveMappingPersistAfterReopen(t *testing.T) {
	dir := t.TempDir()
	scope := telegramScope("main", "primary", "42", "", "42", false)
	backend, _ := newCatalogBackend(t, dir)
	first, err := backend.CreateScopedSession(scope, "Watchdog Gateway")
	require.NoError(t, err)
	second, err := backend.CreateScopedSession(scope, "Telegram Config")
	require.NoError(t, err)
	require.NotEqual(t, first.Key, second.Key)
	require.NoError(t, backend.SetActiveScopedSession(scope, nil, second.Key))
	require.NoError(t, backend.Close())

	reopened, _ := newCatalogBackend(t, dir)
	t.Cleanup(func() { _ = reopened.Close() })
	assert.Equal(t, second.Key, reopened.ActiveScopedSession(scope, nil))
	records, err := reopened.ListScopedSessions(scope, nil)
	require.NoError(t, err)
	names := map[string]string{}
	for _, record := range records {
		names[record.Key] = record.Name
	}
	assert.Equal(t, "Watchdog Gateway", names[first.Key])
	assert.Equal(t, "Telegram Config", names[second.Key])
}

func TestLegacyUnnamedSessionGetsFirstUserFallbackWithoutHistoryMutation(t *testing.T) {
	dir := t.TempDir()
	backend, store := newCatalogBackend(t, dir)
	t.Cleanup(func() { _ = backend.Close() })
	scope := telegramScope("main", "primary", "42", "", "42", false)
	key := session.BuildSessionKey(*scope)
	backend.EnsureSessionMetadata(key, scope, []string{"agent:main:telegram:direct:42"})
	backend.AddMessage(key, "user", "  Investigate | gateway crash  ")
	before := backend.GetHistory(key)

	records, err := backend.ListScopedSessions(scope, []string{"agent:main:telegram:direct:42"})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "Investigate | gateway crash", records[0].Name)
	assert.Equal(t, before, backend.GetHistory(key), "deriving a name must not rewrite history")
	meta, err := store.GetSessionMeta(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, "Investigate | gateway crash", meta.Name)
	assert.Equal(t, "auto", meta.NameSource)
}

func TestScopedSessionListFailsClosedAcrossTelegramBoundaries(t *testing.T) {
	backend, _ := newCatalogBackend(t, t.TempDir())
	t.Cleanup(func() { _ = backend.Close() })
	current := telegramScope("main", "primary", "-1001", "7", "51", true)
	inScope, err := backend.CreateScopedSession(current, "Allowed")
	require.NoError(t, err)

	variants := []*session.SessionScope{
		telegramScope("other", "primary", "-1001", "7", "51", true),
		telegramScope("main", "secondary", "-1001", "7", "51", true),
		telegramScope("main", "primary", "-1002", "7", "51", true),
		telegramScope("main", "primary", "-1001", "8", "51", true),
		telegramScope("main", "primary", "-1001", "7", "52", true),
	}
	for i, variant := range variants {
		_, createErr := backend.CreateScopedSession(variant, "Forbidden")
		require.NoError(t, createErr, "variant %d", i)
	}

	records, err := backend.ListScopedSessions(current, nil)
	require.NoError(t, err)
	keys := make(map[string]bool, len(records))
	for _, record := range records {
		keys[record.Key] = true
	}
	assert.True(t, keys[inScope.Key])
	assert.Len(t, records, 2, "the allowed instance plus its deterministic legacy-compatible default")
}

func TestVisibleSessionStatsExcludeToolsAndThoughts(t *testing.T) {
	backend, _ := newCatalogBackend(t, t.TempDir())
	t.Cleanup(func() { _ = backend.Close() })
	scope := telegramScope("main", "primary", "42", "", "42", false)
	record, err := backend.CreateScopedSession(scope, "Count")
	require.NoError(t, err)
	now := time.Now()
	backend.AddFullMessage(record.Key, providers.Message{Role: "user", Content: "hello", CreatedAt: &now})
	backend.AddFullMessage(record.Key, providers.Message{
		Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call-1"}}, CreatedAt: &now,
	})
	backend.AddFullMessage(record.Key, providers.Message{Role: "tool", Content: "internal", ToolCallID: "call-1", CreatedAt: &now})
	backend.AddFullMessage(record.Key, providers.Message{Role: "assistant", Content: "visible reply", CreatedAt: &now})

	records, err := backend.ListScopedSessions(scope, nil)
	require.NoError(t, err)
	for _, got := range records {
		if got.Key == record.Key {
			assert.Equal(t, 2, got.MessageCount)
			return
		}
	}
	t.Fatal("created session not found")
}

func TestAutomaticFallbackNameIsDurableAndReplacedByFirstMessage(t *testing.T) {
	dir := t.TempDir()
	scope := telegramScope("main", "primary", "42", "", "42", false)
	backend, store := newCatalogBackend(t, dir)

	records, err := backend.ListScopedSessions(scope, nil)
	require.NoError(t, err)
	require.Len(t, records, 1)
	key := records[0].Key
	meta, err := store.GetSessionMeta(context.Background(), key)
	require.NoError(t, err)
	assert.NotEmpty(t, meta.Name)
	assert.Equal(t, "auto", meta.NameSource)
	assert.True(t, meta.AutoNamePending)
	require.NoError(t, backend.Close())

	reopened, reopenedStore := newCatalogBackend(t, dir)
	t.Cleanup(func() { _ = reopened.Close() })
	reopenedRecords, err := reopened.ListScopedSessions(scope, nil)
	require.NoError(t, err)
	require.Len(t, reopenedRecords, 1)
	assert.Equal(t, meta.Name, reopenedRecords[0].Name)

	require.NoError(t, reopened.SetAutomaticSessionName(key, "  First real message\nwith spacing  "))
	updated, err := reopenedStore.GetSessionMeta(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, "First real message with spacing", updated.Name)
	assert.False(t, updated.AutoNamePending)
}

func TestScopedSessionsRespectPrivateGroupTopicAndEphemeralPolicies(t *testing.T) {
	backend, _ := newCatalogBackend(t, t.TempDir())
	t.Cleanup(func() { _ = backend.Close() })

	private := telegramScope("main", "bot-a", "100", "", "100", true)
	privateRecord, err := backend.CreateScopedSession(private, "Private")
	require.NoError(t, err)
	for _, other := range []*session.SessionScope{
		telegramScope("main", "bot-a", "101", "", "100", true),
		telegramScope("main", "bot-a", "100", "", "101", true),
		telegramScope("main", "bot-b", "100", "", "100", true),
	} {
		records, listErr := backend.ListScopedSessions(other, nil)
		require.NoError(t, listErr)
		for _, record := range records {
			assert.NotEqual(t, privateRecord.Key, record.Key)
		}
	}

	groupTopic := telegramScope("main", "bot-a", "-1001", "7", "ignored", false)
	groupRecord, err := backend.CreateScopedSession(groupTopic, "Topic 7")
	require.NoError(t, err)
	for _, other := range []*session.SessionScope{
		telegramScope("main", "bot-a", "-1002", "7", "ignored", false),
		telegramScope("main", "bot-a", "-1001", "8", "ignored", false),
		telegramScope("other", "bot-a", "-1001", "7", "ignored", false),
	} {
		records, listErr := backend.ListScopedSessions(other, nil)
		require.NoError(t, listErr)
		for _, record := range records {
			assert.NotEqual(t, groupRecord.Key, record.Key)
		}
	}

	// A normal public group scope and a personal ephemeral scope must remain
	// distinct even for the same chat/topic.
	publicSameRoute := telegramScope("main", "bot-a", "-1001", "7", "51", false)
	ephemeralSameRoute := telegramScope("main", "bot-a", "-1001", "7", "51", true)
	ephemeralRecord, err := backend.CreateScopedSession(ephemeralSameRoute, "Personal ephemeral")
	require.NoError(t, err)
	publicRecords, err := backend.ListScopedSessions(publicSameRoute, nil)
	require.NoError(t, err)
	for _, record := range publicRecords {
		assert.NotEqual(t, ephemeralRecord.Key, record.Key)
	}
	otherEphemeral := telegramScope("main", "bot-a", "-1001", "7", "52", true)
	otherEphemeralRecords, err := backend.ListScopedSessions(otherEphemeral, nil)
	require.NoError(t, err)
	for _, record := range otherEphemeralRecords {
		assert.NotEqual(t, ephemeralRecord.Key, record.Key)
	}
}

func TestMetadataFreeLegacySessionRequiresProvenAlias(t *testing.T) {
	backend, _ := newCatalogBackend(t, t.TempDir())
	t.Cleanup(func() { _ = backend.Close() })
	scope := telegramScope("main", "primary", "42", "", "42", false)
	allowedAlias := "agent:main:telegram:primary:direct:42"
	backend.AddMessage(allowedAlias, "user", "legacy allowed")
	backend.AddMessage("agent:main:telegram:direct:999", "user", "legacy hidden")
	backend.AddMessage("agent:main:direct:42", "user", "generic alias is not proof")
	backend.AddMessage("agent:main:telegram:direct:42", "user", "account-less alias is not proof")

	records, err := backend.ListScopedSessions(scope, []string{allowedAlias})
	require.NoError(t, err)
	seen := make(map[string]bool)
	for _, record := range records {
		seen[record.Key] = true
	}
	assert.True(t, seen[allowedAlias])
	assert.False(t, seen["agent:main:telegram:direct:999"])
	assert.False(t, seen["agent:main:direct:42"])
	assert.False(t, seen["agent:main:telegram:direct:42"])
}

func TestUnprovenLegacyAliasHistoryIsNotPromotedIntoScopedDefault(t *testing.T) {
	backend, store := newCatalogBackend(t, t.TempDir())
	t.Cleanup(func() { _ = backend.Close() })
	scope := telegramScope("main", "primary", "42", "", "42", false)
	defaultKey := session.BuildSessionKey(*scope)
	genericAlias := "agent:main:direct:42"
	accountlessAlias := "agent:main:telegram:direct:42"
	backend.AddMessage(genericAlias, "user", "must stay hidden")
	backend.AddMessage(accountlessAlias, "user", "must also stay hidden")

	backend.EnsureSessionMetadata(defaultKey, scope, []string{genericAlias, accountlessAlias})
	assert.Empty(t, backend.GetHistory(defaultKey))
	meta, err := store.GetSessionMeta(context.Background(), defaultKey)
	require.NoError(t, err)
	assert.Empty(t, meta.Aliases)
}

func TestConcurrentActiveSwitchKeepsSessionHistoriesIsolated(t *testing.T) {
	backend, _ := newCatalogBackend(t, t.TempDir())
	t.Cleanup(func() { _ = backend.Close() })
	scope := telegramScope("main", "primary", "42", "", "42", false)
	first, err := backend.CreateScopedSession(scope, "First")
	require.NoError(t, err)
	second, err := backend.CreateScopedSession(scope, "Second")
	require.NoError(t, err)
	backend.AddMessage(first.Key, "user", "history-first")
	backend.AddMessage(second.Key, "user", "history-second")

	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		key := first.Key
		if i%2 == 1 {
			key = second.Key
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- backend.SetActiveScopedSession(scope, nil, key)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.NoError(t, backend.SetActiveScopedSession(scope, nil, second.Key))
	assert.Equal(t, second.Key, backend.ActiveScopedSession(scope, nil))
	require.Len(t, backend.GetHistory(first.Key), 1)
	require.Len(t, backend.GetHistory(second.Key), 1)
	assert.Equal(t, "history-first", backend.GetHistory(first.Key)[0].Content)
	assert.Equal(t, "history-second", backend.GetHistory(second.Key)[0].Content)
}
