package session

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelOverridesRemainIsolatedAcrossConcurrentSessions(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	overrides := map[string]ModelOverride{
		"session-a": {Provider: "openai", Model: "gpt-a", ConfigRef: "openai-main", Source: "configured"},
		"session-b": {Provider: "gemini", Model: "gemini-b", ConfigRef: "google-main", Source: "discovered"},
	}
	var wg sync.WaitGroup
	for key, override := range overrides {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, sm.SetModelOverride(key, override))
		}()
	}
	wg.Wait()

	for key, want := range overrides {
		got, ok, err := sm.GetModelOverride(key)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, want, got)
	}

	require.NoError(t, sm.ClearModelOverride("session-a"))
	_, ok, err := sm.GetModelOverride("session-a")
	require.NoError(t, err)
	assert.False(t, ok)
	gotB, ok, err := sm.GetModelOverride("session-b")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, overrides["session-b"], gotB)
}

func TestModelOverridesRemainIsolatedAcrossTopics(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	base := SessionScope{
		Version:    ScopeVersionV1,
		AgentID:    "main",
		Channel:    "telegram",
		Account:    "bot-a",
		Dimensions: []string{"chat", "topic"},
		Values:     map[string]string{"chat": "100", "topic": "1"},
	}
	topicA := base
	topicA.Values = map[string]string{"chat": "100", "topic": "1"}
	topicB := base
	topicB.Values = map[string]string{"chat": "100", "topic": "2"}
	keyA := BuildSessionKey(topicA)
	keyB := BuildSessionKey(topicB)
	require.NotEqual(t, keyA, keyB)

	require.NoError(t, sm.SetModelOverride(keyA, ModelOverride{
		Provider: "openai", Model: "gpt-a", ConfigRef: "cfg-a", Source: "configured",
	}))
	require.NoError(t, sm.SetModelOverride(keyB, ModelOverride{
		Provider: "openai", Model: "gpt-b", ConfigRef: "cfg-b", Source: "configured",
	}))
	a, ok, err := sm.GetModelOverride(keyA)
	require.NoError(t, err)
	require.True(t, ok)
	b, ok, err := sm.GetModelOverride(keyB)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "gpt-a", a.Model)
	assert.Equal(t, "gpt-b", b.Model)
}
