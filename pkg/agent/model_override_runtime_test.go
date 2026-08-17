package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/providers"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

type recordingStatefulProvider struct{ closes atomic.Int32 }

func (p *recordingStatefulProvider) Chat(
	context.Context,
	[]providers.Message,
	[]providers.ToolDefinition,
	string,
	map[string]any,
) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{}, nil
}

func (p *recordingStatefulProvider) GetDefaultModel() string { return "test-model" }
func (p *recordingStatefulProvider) Close()                  { p.closes.Add(1) }

func TestCloseOwnedSessionModelProviderIsIdempotent(t *testing.T) {
	exec := &turnExecution{}
	provider := &recordingStatefulProvider{}
	ownedSessionModelProviders.Store(exec, provider)
	t.Cleanup(func() { ownedSessionModelProviders.Delete(exec) })

	closeOwnedSessionModelProvider(exec)
	closeOwnedSessionModelProvider(exec)
	assert.Equal(t, int32(1), provider.closes.Load())
}

func TestOwnedSessionModelProviderClosesOnTurnContextCancellation(t *testing.T) {
	exec := &turnExecution{}
	provider := &recordingStatefulProvider{}
	ownedSessionModelProviders.Store(exec, provider)
	t.Cleanup(func() { ownedSessionModelProviders.Delete(exec) })

	ctx, cancel := context.WithCancel(context.Background())
	context.AfterFunc(ctx, func() { closeOwnedSessionModelProvider(exec) })
	cancel()
	require.Eventually(t, func() bool { return provider.closes.Load() == 1 }, time.Second, 10*time.Millisecond)
	closeOwnedSessionModelProvider(exec)
	assert.Equal(t, int32(1), provider.closes.Load())
}

func TestSharedProviderIsNotClosedBySessionCleanup(t *testing.T) {
	shared := &recordingStatefulProvider{}
	agent := &AgentInstance{
		Provider: shared,
		CandidateProviders: map[string]providers.LLMProvider{
			providers.ModelKey("openai", "shared"): shared,
		},
	}
	closeSessionModelProviderIfOwned(agent, shared)
	assert.Zero(t, shared.closes.Load())
}

func TestPrimarySessionCandidateUsesTurnLocalProviderForDuplicateModelSources(t *testing.T) {
	turnLocal := &recordingStatefulProvider{}
	global := &recordingStatefulProvider{}
	agent := &AgentInstance{CandidateProviders: map[string]providers.LLMProvider{
		providers.ModelKey("openai", "shared"): global,
	}}
	active := []providers.FallbackCandidate{{
		Provider: "openai", Model: "shared", IdentityKey: "cfg:v1:account-a",
	}}

	got, err := providerForFallbackCandidate(agent, turnLocal, active, active[0])
	require.NoError(t, err)
	assert.Same(t, turnLocal, got)
	otherSource := providers.FallbackCandidate{
		Provider: "openai", Model: "shared", IdentityKey: "cfg:v1:account-b",
	}
	got, err = providerForFallbackCandidate(agent, turnLocal, active, otherSource)
	require.NoError(t, err)
	assert.Same(t, global, got)
}

func TestModelSelectionValidationFailureClosesTemporaryProviderAndPreservesOverride(t *testing.T) {
	store := session.NewSessionManager("")
	old := session.ModelOverride{
		Provider: "openai", Model: "old-model", ConfigRef: "old-source", Source: "configured",
	}
	require.NoError(t, store.SetModelOverride("session-a", old))

	source := &config.ModelConfig{
		ModelName: "next", Provider: "openai", Model: "openai/next-model", Enabled: true,
	}
	cfg := &config.Config{ModelList: []*config.ModelConfig{source}}
	selection := configuredSelections(cfg)[0]
	temporary := &recordingStatefulProvider{}

	err := validateAndPersistModelSelectionWithFactory(
		modelCommandContext{SessionKey: "session-a"},
		store,
		cfg,
		selection,
		func(*config.ModelConfig) (providers.LLMProvider, string, error) {
			return temporary, "", errors.New("validation failed")
		},
	)
	require.Error(t, err)
	assert.Equal(t, int32(1), temporary.closes.Load())

	got, ok, readErr := store.GetModelOverride("session-a")
	require.NoError(t, readErr)
	require.True(t, ok)
	assert.Equal(t, old, got)
}

func TestRuntimeProviderInitializationFailureFallsBackWithoutMutatingTurn(t *testing.T) {
	store := session.NewSessionManager("")
	source := &config.ModelConfig{
		ModelName: "override", Provider: "openai", Model: "openai/override-model", Enabled: true,
	}
	cfg := &config.Config{ModelList: []*config.ModelConfig{source}}
	cfg.Agents.Defaults.Provider = "openai"
	require.NoError(t, store.SetModelOverride("session-a", session.ModelOverride{
		Provider: "openai", Model: "override-model", ConfigRef: stableModelConfigRef(source), Source: "configured",
	}))
	defaultProvider := &recordingStatefulProvider{}
	temporary := &recordingStatefulProvider{}
	agent := &AgentInstance{Provider: defaultProvider, Sessions: store}
	al := &AgentLoop{
		cfg: cfg,
		providerFactory: func(*config.ModelConfig) (providers.LLMProvider, string, error) {
			return temporary, "", errors.New("credential expired")
		},
	}
	exec := &turnExecution{activeProvider: defaultProvider, activeModel: "default-model"}
	err := al.applySessionModelOverride(
		context.Background(),
		&turnState{agent: agent, sessionKey: "session-a"},
		exec,
	)
	require.NoError(t, err)
	assert.Same(t, defaultProvider, exec.activeProvider)
	assert.Equal(t, "default-model", exec.activeModel)
	assert.Equal(t, int32(1), temporary.closes.Load())
}

func TestRuntimeValidationNeverClosesSharedProviderReturnedByFactory(t *testing.T) {
	store := session.NewSessionManager("")
	source := &config.ModelConfig{
		ModelName: "override", Provider: "openai", Model: "openai/override-model", Enabled: true,
	}
	cfg := &config.Config{ModelList: []*config.ModelConfig{source}}
	cfg.Agents.Defaults.Provider = "openai"
	require.NoError(t, store.SetModelOverride("session-a", session.ModelOverride{
		Provider: "openai", Model: "override-model", ConfigRef: stableModelConfigRef(source), Source: "configured",
	}))
	shared := &recordingStatefulProvider{}
	agent := &AgentInstance{Provider: shared, Sessions: store}
	al := &AgentLoop{
		cfg: cfg,
		providerFactory: func(*config.ModelConfig) (providers.LLMProvider, string, error) {
			return shared, "", errors.New("validation failed")
		},
	}
	require.NoError(t, al.applySessionModelOverride(
		context.Background(),
		&turnState{agent: agent, sessionKey: "session-a"},
		&turnExecution{activeProvider: shared},
	))
	assert.Zero(t, shared.closes.Load())
}
