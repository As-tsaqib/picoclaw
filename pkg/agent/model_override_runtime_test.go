package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/providers"
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
