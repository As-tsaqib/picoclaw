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
	exec := &turnExecution{}
	shared := &recordingStatefulProvider{}
	closeOwnedSessionModelProvider(exec)
	assert.Zero(t, shared.closes.Load())
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
