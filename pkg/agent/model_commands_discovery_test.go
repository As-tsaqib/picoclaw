package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/config"
)

func TestDiscoveredModelCacheHitAndForceRefresh(t *testing.T) {
	resetModelDiscoveryCacheForTest()
	t.Cleanup(resetModelDiscoveryCacheForTest)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"live-model"}]}`))
	}))
	defer server.Close()

	cfg := &config.Config{}
	mc := &config.ModelConfig{
		ModelName: "primary",
		Provider:  "openai",
		Model:     "openai/seed",
		APIBase:   server.URL,
		Enabled:   true,
	}
	mc.SetAPIKey("test-key")
	cfg.ModelList = []*config.ModelConfig{mc}
	sources := discoverySources(cfg)
	require.Len(t, sources, 1)

	first, err := fetchDiscoveredModels(context.Background(), cfg, sources[0], false)
	require.NoError(t, err)
	require.Len(t, first, 1)
	second, err := fetchDiscoveredModels(context.Background(), cfg, sources[0], false)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, int32(1), calls.Load(), "second read must use the live-catalog cache")

	refreshed, err := fetchDiscoveredModels(context.Background(), cfg, sources[0], true)
	require.NoError(t, err)
	require.Len(t, refreshed, 1)
	assert.Equal(t, int32(2), calls.Load(), "force refresh must bypass the cache")
}

func TestDiscoveredModelResolutionFailsSoftAcrossProviders(t *testing.T) {
	resetModelDiscoveryCacheForTest()
	t.Cleanup(resetModelDiscoveryCacheForTest)

	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "catalog unavailable", http.StatusBadGateway)
	}))
	defer failed.Close()
	working := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"target-model"}]}`))
	}))
	defer working.Close()

	cfg := &config.Config{}
	cfg.ModelList = []*config.ModelConfig{
		{ModelName: "broken", Provider: "openai", Model: "openai/seed-a", APIBase: failed.URL, Enabled: true},
		{ModelName: "working", Provider: "openai", Model: "openai/seed-b", APIBase: working.URL, Enabled: true},
	}

	selection, ok, err := resolveDiscoveredModelSelection(context.Background(), cfg, "target-model")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "target-model", selection.Model)
	assert.Equal(t, "openai", selection.Provider)
	assert.Equal(t, stableModelConfigRef(cfg.ModelList[1]), selection.ConfigRef)
}
