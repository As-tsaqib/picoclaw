package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/As-tsaqib/picoclaw/pkg/providers"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

var ownedSessionModelProviders sync.Map // map[*turnExecution]providers.StatefulProvider

// applySessionModelOverride resolves a durable session override into turn-local
// provider/model state. AgentInstance is never mutated, so concurrent sessions
// remain isolated and an in-flight turn keeps the model frozen at setup time.
func (al *AgentLoop) applySessionModelOverride(ctx context.Context, ts *turnState, exec *turnExecution) error {
	if al == nil || ts == nil || exec == nil || ts.agent == nil || strings.TrimSpace(ts.sessionKey) == "" {
		return nil
	}
	store, ok := ts.agent.Sessions.(session.ModelOverrideStore)
	if !ok {
		return nil
	}
	override, found, err := store.GetModelOverride(ts.sessionKey)
	if err != nil {
		return fmt.Errorf("load session model override: %w", err)
	}
	if !found {
		return nil
	}
	cfg := al.GetConfig()
	if cfg == nil {
		return fmt.Errorf("config is unavailable for session model override")
	}
	source := validSessionModelOverrideSource(cfg, override)
	if source == nil {
		// Config edits can invalidate a previously valid durable preference.
		// Fail soft to the agent default so normal messages keep working and the
		// dashboard reports the same effective model as runtime execution.
		return nil
	}

	modelCfg := *source
	modelCfg.Model = strings.TrimSpace(override.Model)
	if modelCfg.Workspace == "" {
		modelCfg.Workspace = ts.agent.Workspace
	}
	factory := al.providerFactory
	if factory == nil {
		factory = providers.CreateProviderFromConfig
	}
	provider, _, err := factory(&modelCfg)
	if err != nil {
		return fmt.Errorf("initialize session model %q: %w", override.Model, err)
	}

	primary, ok := candidateFromModelConfig(cfg.Agents.Defaults.Provider, &modelCfg)
	if !ok {
		closeProviderIfStateful(provider)
		return fmt.Errorf("session model %q did not resolve to a provider candidate", override.Model)
	}
	primary.Model = strings.TrimSpace(override.Model)
	primary.Provider = providers.NormalizeProvider(override.Provider)
	if strings.TrimSpace(override.Alias) != "" {
		primary.DisplayName = strings.TrimSpace(override.Alias)
	} else {
		primary.DisplayName = strings.TrimSpace(override.Model)
	}

	candidates := []providers.FallbackCandidate{primary}
	seen := map[string]bool{primary.StableKey(): true}
	for _, fallback := range resolveModelCandidates(cfg, cfg.Agents.Defaults.Provider, "", source.Fallbacks) {
		if seen[fallback.StableKey()] {
			continue
		}
		seen[fallback.StableKey()] = true
		candidates = append(candidates, fallback)
	}

	exec.activeCandidates = candidates
	exec.activeModel = strings.TrimSpace(override.Model)
	exec.activeModelConfig = &modelCfg
	exec.activeProvider = provider
	exec.llmModelName = primary.DisplayName
	exec.usedLight = false
	if stateful, closeOK := provider.(providers.StatefulProvider); closeOK {
		ownedSessionModelProviders.Store(exec, stateful)
		// Finalize closes normal paths. The turn context is canceled by runTurn on
		// every return path, so this covers setup/call/hook/abort errors that bypass
		// Finalize. LoadAndDelete makes the two cleanup paths safely idempotent.
		context.AfterFunc(ctx, func() { closeOwnedSessionModelProvider(exec) })
	}
	return nil
}

func closeOwnedSessionModelProvider(exec *turnExecution) {
	if exec == nil {
		return
	}
	value, ok := ownedSessionModelProviders.LoadAndDelete(exec)
	if !ok {
		return
	}
	if provider, ok := value.(providers.StatefulProvider); ok {
		provider.Close()
	}
}
