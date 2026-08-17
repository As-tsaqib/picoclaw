package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func TestConfiguredSelectionsHonorLegacyEnabledInference(t *testing.T) {
	cfg := &config.Config{}
	withKey := &config.ModelConfig{
		ModelName: "legacy-keyed",
		Provider:  "openai",
		Model:     "openai/keyed-model",
	}
	withKey.SetAPIKey("test-key")
	cfg.ModelList = append(cfg.ModelList,
		withKey,
		&config.ModelConfig{
			ModelName: "explicit",
			Provider:  "openai",
			Model:     "openai/explicit-model",
			Enabled:   true,
		},
		&config.ModelConfig{
			ModelName: "disabled",
			Provider:  "openai",
			Model:     "openai/disabled-model",
		},
	)

	selections := configuredSelections(cfg)
	require.Len(t, selections, 2)
	aliases := []string{selections[0].Alias, selections[1].Alias}
	assert.ElementsMatch(t, []string{"legacy-keyed", "explicit"}, aliases)
}

func TestModelConfigSelectableRepresentativeCredentialModes(t *testing.T) {
	tests := []struct {
		name string
		mc   *config.ModelConfig
		want bool
	}{
		{"disabled ordinary provider", &config.ModelConfig{Provider: "openai", Model: "openai/x"}, false},
		{"oauth openai", &config.ModelConfig{Provider: "openai", Model: "openai/x", AuthMethod: "oauth"}, true},
		{
			"oauth anthropic",
			&config.ModelConfig{Provider: "anthropic", Model: "anthropic/x", AuthMethod: "oauth"},
			true,
		},
		{
			"token anthropic",
			&config.ModelConfig{Provider: "anthropic", Model: "anthropic/x", AuthMethod: "token"},
			true,
		},
		{"antigravity oauth", &config.ModelConfig{Provider: "antigravity", Model: "antigravity/x"}, true},
		{"bedrock external credentials", &config.ModelConfig{Provider: "bedrock", Model: "bedrock/x"}, true},
		{"claude cli", &config.ModelConfig{Provider: "claude-cli", Model: "claude-cli/x"}, true},
		{
			"custom compatible endpoint",
			&config.ModelConfig{Provider: "openai", Model: "openai/x", APIBase: "http://localhost:11434/v1"},
			true,
		},
		{"explicit enabled", &config.ModelConfig{Provider: "openai", Model: "openai/x", Enabled: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assert.Equal(t, tt.want, modelConfigSelectable(tt.mc)) })
	}
}

func TestStableModelConfigRefSeparatesDuplicateAliasAccounts(t *testing.T) {
	a := &config.ModelConfig{
		ModelName: "Gemini", Provider: "openai", Model: "openai/shared",
		APIBase: "https://a.example/v1", Enabled: true,
	}
	b := &config.ModelConfig{
		ModelName: "Gemini", Provider: "openai", Model: "openai/shared",
		APIBase: "https://b.example/v1", Enabled: true,
	}
	a.SetAPIKey("credential-a-super-secret")
	b.SetAPIKey("credential-b-super-secret")
	cfg := &config.Config{ModelList: []*config.ModelConfig{a, b}}

	refA := stableModelConfigRef(a)
	refB := stableModelConfigRef(b)
	require.NotEmpty(t, refA)
	require.NotEmpty(t, refB)
	assert.NotEqual(t, refA, refB)
	assert.NotContains(t, refA, "credential-a")
	assert.NotContains(t, refB, "credential-b")
	assert.Same(t, a, lookupSessionModelConfigByRef(cfg, refA))
	assert.Same(t, b, lookupSessionModelConfigByRef(cfg, refB))
	assert.Nil(t, lookupSessionModelConfigByRef(cfg, "Gemini"), "legacy duplicate alias must fail closed")

	selections := configuredSelections(cfg)
	require.Len(t, selections, 2)
	assert.ElementsMatch(t, []string{refA, refB}, []string{selections[0].ConfigRef, selections[1].ConfigRef})
}

func TestStableModelConfigRefSeparatesSameAliasAcrossProviders(t *testing.T) {
	a := &config.ModelConfig{ModelName: "Gemini", Provider: "openai", Model: "openai/shared", Enabled: true}
	b := &config.ModelConfig{ModelName: "Gemini", Provider: "openrouter", Model: "openrouter/shared", Enabled: true}
	assert.NotEqual(t, stableModelConfigRef(a), stableModelConfigRef(b))
}

func TestStableModelConfigRefDoesNotDependOnCredentialSliceOrder(t *testing.T) {
	a := &config.ModelConfig{
		ModelName: "shared", Provider: "openai", Model: "openai/model", Enabled: true,
	}
	b := &config.ModelConfig{
		ModelName: "shared", Provider: "openai", Model: "openai/model", Enabled: true,
	}
	a.APIKeys = config.SimpleSecureStrings("key-a", "key-b")
	b.APIKeys = config.SimpleSecureStrings("key-b", "key-a")
	assert.Equal(t, stableModelConfigRef(a), stableModelConfigRef(b))
}

func TestStableModelConfigRefIncludesStreamingMode(t *testing.T) {
	a := &config.ModelConfig{ModelName: "shared", Provider: "openai", Model: "openai/model", Enabled: true}
	b := *a
	b.Streaming.Enabled = true
	assert.NotEqual(t, stableModelConfigRef(a), stableModelConfigRef(&b))
}

func TestLegacyModelReferenceFailsClosedAcrossDuplicateSources(t *testing.T) {
	a := &config.ModelConfig{
		ModelName: "account-a", Provider: "openai", Model: "openai/shared", APIBase: "https://a.example", Enabled: true,
	}
	b := &config.ModelConfig{
		ModelName: "account-b", Provider: "openai", Model: "openai/shared", APIBase: "https://b.example", Enabled: true,
	}
	cfg := &config.Config{ModelList: []*config.ModelConfig{a, b}}
	assert.Nil(t, lookupSessionModelConfigByRef(cfg, "shared", "openai"))
	assert.Nil(t, lookupSessionModelConfigByRef(cfg, "openai/shared", "openai"))
}

func TestStableModelConfigRefPersistsAndResolvesAfterRestart(t *testing.T) {
	dir := t.TempDir()
	a := &config.ModelConfig{
		ModelName: "Gemini", Provider: "openai", Model: "openai/shared",
		APIBase: "https://a.example/v1", Enabled: true,
	}
	b := &config.ModelConfig{
		ModelName: "Gemini", Provider: "openai", Model: "openai/shared",
		APIBase: "https://b.example/v1", Enabled: true,
	}
	a.SetAPIKey("credential-a")
	b.SetAPIKey("credential-b")
	cfg := &config.Config{ModelList: []*config.ModelConfig{a, b}}
	refB := stableModelConfigRef(b)

	first := session.NewSessionManager(dir)
	require.NoError(t, first.SetModelOverride("session-a", session.ModelOverride{
		Provider: "openai", Model: "shared", Alias: "Gemini", ConfigRef: refB, Source: "configured",
	}))
	second := session.NewSessionManager(dir)
	override, ok, err := second.GetModelOverride("session-a")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, refB, override.ConfigRef)
	resolved := validSessionModelOverrideSource(cfg, override)
	require.Same(t, b, resolved)
	assert.Equal(t, "credential-b", resolved.APIKey())
	assert.NotEqual(t, "credential-a", resolved.APIKey())
}

func TestResolveModelSelectionRejectsConfiguredAmbiguity(t *testing.T) {
	cfg := &config.Config{}
	cfg.ModelList = append(cfg.ModelList,
		&config.ModelConfig{ModelName: "dup", Provider: "openai", Model: "openai/model-a", Enabled: true},
		&config.ModelConfig{ModelName: "dup", Provider: "openrouter", Model: "openrouter/model-b", Enabled: true},
	)

	_, err := (&AgentLoop{}).resolveModelSelection(context.Background(), cfg, "dup")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "ambigu")
}

func TestResolveModelSelectionRejectsBareModelAmbiguityAndAcceptsQualified(t *testing.T) {
	cfg := &config.Config{}
	first := &config.ModelConfig{ModelName: "one", Provider: "openai", Model: "openai/shared", Enabled: true}
	cfg.ModelList = append(cfg.ModelList,
		first,
		&config.ModelConfig{ModelName: "two", Provider: "openrouter", Model: "openrouter/shared", Enabled: true},
	)

	_, err := (&AgentLoop{}).resolveModelSelection(context.Background(), cfg, "shared")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "ambigu")

	selection, err := (&AgentLoop{}).resolveModelSelection(context.Background(), cfg, "openai/shared")
	require.NoError(t, err)
	assert.Equal(t, "openai", selection.Provider)
	assert.Equal(t, "shared", selection.Model)
	assert.Equal(t, stableModelConfigRef(first), selection.ConfigRef)
}

func TestChooseDiscoverySourceDistinguishesSameProviderAccounts(t *testing.T) {
	a := modelSelection{Provider: "openai", Alias: "same", ConfigRef: "cfg:v1:aaaaaaaa"}
	b := modelSelection{Provider: "openai", Alias: "same", ConfigRef: "cfg:v1:bbbbbbbb"}
	sources := []modelSelection{a, b}
	assert.Equal(t, b.ConfigRef, chooseDiscoverySource(sources, "openai", b.ConfigRef).ConfigRef)
	assert.Contains(t, discoverySourceLabel(a, sources), "aaaaaaaa")
	assert.Contains(t, discoverySourceLabel(b, sources), "bbbbbbbb")
}

func TestActiveIndicatorIncludesProviderSourceIdentity(t *testing.T) {
	active := effectiveModelInfo{Provider: "openai", Model: "shared", ConfigRef: "cfg:v1:a"}
	assert.True(t, sameEffectiveModel(
		active,
		modelSelection{Provider: "openai", Model: "shared", ConfigRef: "cfg:v1:a"},
	))
	assert.False(t, sameEffectiveModel(
		active,
		modelSelection{Provider: "openai", Model: "shared", ConfigRef: "cfg:v1:b"},
	))
}

func TestDiscoveryCacheSeparatesProviderConfigurationIdentity(t *testing.T) {
	resetModelDiscoveryCacheForTest()
	t.Cleanup(resetModelDiscoveryCacheForTest)

	serverOne := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"from-one"}]}`))
	}))
	defer serverOne.Close()
	serverTwo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"from-two"}]}`))
	}))
	defer serverTwo.Close()

	newConfig := func(apiBase, apiKey string) (*config.Config, modelSelection) {
		cfg := &config.Config{}
		mc := &config.ModelConfig{
			ModelName: "shared-ref",
			Provider:  "openai",
			Model:     "openai/seed",
			APIBase:   apiBase,
			Enabled:   true,
		}
		mc.SetAPIKey(apiKey)
		cfg.ModelList = append(cfg.ModelList, mc)
		return cfg, modelSelection{Provider: "openai", ConfigRef: stableModelConfigRef(mc)}
	}
	cfgOne, sourceOne := newConfig(serverOne.URL, "key-one")
	cfgTwo, sourceTwo := newConfig(serverTwo.URL, "key-two")

	modelsOne, err := fetchDiscoveredModels(context.Background(), cfgOne, sourceOne, false)
	require.NoError(t, err)
	require.Len(t, modelsOne, 1)
	assert.Equal(t, "from-one", modelsOne[0].Model)

	modelsTwo, err := fetchDiscoveredModels(context.Background(), cfgTwo, sourceTwo, false)
	require.NoError(t, err)
	require.Len(t, modelsTwo, 1)
	assert.Equal(t, "from-two", modelsTwo[0].Model)
	assert.NotEqual(t, sourceOne.ConfigRef, sourceTwo.ConfigRef)
}

func TestEffectiveSessionModelIgnoresStaleOverride(t *testing.T) {
	store := session.NewSessionManager("")
	require.NoError(t, store.SetModelOverride("session-a", session.ModelOverride{
		Provider: "openai", Model: "removed-model", ConfigRef: "removed", Source: "configured",
	}))
	cfg := &config.Config{}
	cfg.Agents.Defaults.Provider = "openai"

	info := effectiveSessionModel(nil, store, cfg, "session-a")
	assert.Equal(t, "Default Agent", info.Scope)
	assert.NotEqual(t, "removed-model", info.Model)
}

func TestValidateDiscoveredSelectionRejectsStaleCatalogEntry(t *testing.T) {
	resetModelDiscoveryCacheForTest()
	t.Cleanup(resetModelDiscoveryCacheForTest)

	cfg := &config.Config{}
	mc := &config.ModelConfig{
		ModelName: "primary",
		Provider:  "openai",
		Model:     "openai/seed",
		Enabled:   true,
	}
	mc.SetAPIKey("test-key")
	cfg.ModelList = append(cfg.ModelList, mc)
	source := modelSelection{Provider: "openai", ConfigRef: stableModelConfigRef(mc)}
	key, err := modelDiscoveryCacheKey(source, mc)
	require.NoError(t, err)
	modelDiscoveryCache.Lock()
	modelDiscoveryCache.entries[key] = discoveryCacheEntry{
		Models:    []discoveredModel{{Provider: "openai", Model: "current-model", ConfigRef: source.ConfigRef}},
		ExpiresAt: time.Now().Add(time.Minute),
	}
	modelDiscoveryCache.Unlock()

	store := session.NewSessionManager("")
	err = validateAndPersistModelSelection(
		modelCommandContext{SessionKey: "session-a"},
		store,
		cfg,
		modelSelection{
			Provider: "openai", Model: "stale-model", ConfigRef: source.ConfigRef, Source: "discovered",
		},
	)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "stale")
	_, ok, readErr := store.GetModelOverride("session-a")
	require.NoError(t, readErr)
	assert.False(t, ok)
}

func TestDiscoveryCacheIsBoundedAndPublicErrorsHideProviderURL(t *testing.T) {
	resetModelDiscoveryCacheForTest()
	t.Cleanup(resetModelDiscoveryCacheForTest)
	now := time.Now()
	modelDiscoveryCache.Lock()
	for i := 0; i < modelDiscoveryMaxCacheEntries; i++ {
		key := fmt.Sprintf("source-%03d", i)
		modelDiscoveryCache.entries[key] = discoveryCacheEntry{ExpiresAt: now.Add(time.Duration(i+1) * time.Second)}
	}
	pruneModelDiscoveryCacheLocked(now, "incoming")
	modelDiscoveryCache.entries["incoming"] = discoveryCacheEntry{ExpiresAt: now.Add(time.Minute)}
	count := len(modelDiscoveryCache.entries)
	modelDiscoveryCache.Unlock()
	assert.LessOrEqual(t, count, modelDiscoveryMaxCacheEntries)

	err := publicModelDiscoveryError(errors.New(
		`Get "https://user:secret@example.invalid/models?token=credential": connection refused`,
	))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
	assert.NotContains(t, err.Error(), "credential")
	assert.NotContains(t, err.Error(), "example.invalid")
}

func resetModelDiscoveryCacheForTest() {
	modelDiscoveryCache.Lock()
	modelDiscoveryCache.entries = make(map[string]discoveryCacheEntry)
	modelDiscoveryCache.Unlock()
}
