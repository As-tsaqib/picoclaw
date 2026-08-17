package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func TestResolveConfiguredModelByAliasAndModelID(t *testing.T) {
	mc := &config.ModelConfig{
		ModelName: "Primary",
		Provider:  "openai",
		Model:     "openai/gpt-test",
		Enabled:   true,
	}
	cfg := &config.Config{ModelList: []*config.ModelConfig{mc}}

	byAlias, err := (&AgentLoop{}).resolveModelSelection(context.Background(), cfg, "Primary")
	require.NoError(t, err)
	byID, err := (&AgentLoop{}).resolveModelSelection(context.Background(), cfg, "gpt-test")
	require.NoError(t, err)
	assert.Equal(t, "gpt-test", byAlias.Model)
	assert.Equal(t, byAlias, byID)
	assert.Equal(t, stableModelConfigRef(mc), byAlias.ConfigRef)
}

func TestResolveAndPersistDiscoveredModelWithoutChangingConfig(t *testing.T) {
	resetModelDiscoveryCacheForTest()
	t.Cleanup(resetModelDiscoveryCacheForTest)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"live-only-model"}]}`))
	}))
	defer server.Close()

	mc := &config.ModelConfig{
		ModelName: "Primary",
		Provider:  "openai",
		Model:     "openai/configured-model",
		APIBase:   server.URL,
		Enabled:   true,
	}
	mc.SetAPIKey("credential-that-must-not-persist")
	cfg := &config.Config{ModelList: []*config.ModelConfig{mc}}
	selection, err := (&AgentLoop{}).resolveModelSelection(context.Background(), cfg, "live-only-model")
	require.NoError(t, err)
	assert.Equal(t, "discovered", selection.Source)

	dir := t.TempDir()
	store := session.NewSessionManager(dir)
	require.NoError(t, validateAndPersistModelSelection(
		modelCommandContext{SessionKey: "session-a"},
		store,
		cfg,
		selection,
	))
	override, ok, err := store.GetModelOverride("session-a")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "live-only-model", override.Model)
	assert.Equal(t, stableModelConfigRef(mc), override.ConfigRef)
	assert.Equal(t, "configured-model", strings.TrimPrefix(mc.Model, "openai/"), "config must remain unchanged")

	persisted, err := os.ReadFile(filepath.Join(dir, ".model-overrides.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(persisted), "credential-that-must-not-persist")
}

func TestResolveModelSelectionRejectsUnknownModel(t *testing.T) {
	cfg := &config.Config{ModelList: []*config.ModelConfig{
		{ModelName: "Primary", Provider: "anthropic", Model: "anthropic/configured", Enabled: true},
	}}
	_, err := (&AgentLoop{}).resolveModelSelection(context.Background(), cfg, "not-configured")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tidak ditemukan")
}

func TestFailedProviderInitializationDoesNotReplaceExistingOverride(t *testing.T) {
	store := session.NewSessionManager("")
	old := session.ModelOverride{
		Provider: "openai", Model: "old-model", ConfigRef: "old-source", Source: "configured",
	}
	require.NoError(t, store.SetModelOverride("session-a", old))

	broken := &config.ModelConfig{
		ModelName: "Broken",
		Provider:  "definitely-unsupported-provider",
		Model:     "definitely-unsupported-provider/new-model",
		Enabled:   true,
	}
	cfg := &config.Config{ModelList: []*config.ModelConfig{broken}}
	selection := configuredSelections(cfg)[0]
	err := validateAndPersistModelSelection(
		modelCommandContext{SessionKey: "session-a"},
		store,
		cfg,
		selection,
	)
	require.Error(t, err)

	got, ok, readErr := store.GetModelOverride("session-a")
	require.NoError(t, readErr)
	require.True(t, ok)
	assert.Equal(t, old, got)
}
