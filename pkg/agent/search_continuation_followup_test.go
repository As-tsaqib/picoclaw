package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func TestSkillPickerFallbackIsCompleteAndDetailBackKeepsSearchState(t *testing.T) {
	names := []string{"alpha", "beta", "calendar", "delta", "epsilon", "web-fetch", "zeta"}
	fallback := buildSkillPickerFallback(names, "", "calendar")
	for _, name := range names {
		assert.Contains(t, fallback, "- "+name)
	}
	assert.Contains(t, fallback, "/use <skill>")
	assert.Contains(t, fallback, "/use <skill> <message>")
	assert.Contains(t, fallback, "/use clear")
	assert.Contains(t, fallback, "/use off")

	inbound := &bus.InboundContext{
		Channel: "telegram", Account: "telegram", ChatID: "42", ChatType: "direct", SenderID: "42",
	}
	scope := &session.SessionScope{Version: 1, AgentID: "main", Channel: "telegram", Account: "telegram"}
	detail := buildSkillDetailContent(
		&AgentInstance{ID: "main"}, "si_v1_bound", scope, inbound, "web-fetch", 2, "web",
	)
	require.NotNil(t, detail.Interaction)
	assert.Equal(t, 2, detail.Interaction.Page)
	assert.Equal(t, "web", detail.Interaction.Query)
	assert.Equal(t, "si_v1_bound", detail.Interaction.SessionKey)
	foundBack := false
	for _, entry := range detail.Interaction.Entries {
		if entry.Action == "back" {
			foundBack = true
		}
	}
	assert.True(t, foundBack)
}

type recordingCheckpointStore struct {
	checkpoint memory.TaskCheckpoint
	listCalls  int
	getCalls   int
	mutations  []memory.CheckpointMutation
}

func (s *recordingCheckpointStore) List(memory.CallerScope, bool) ([]memory.TaskCheckpoint, error) {
	s.listCalls++
	return []memory.TaskCheckpoint{s.checkpoint}, nil
}

func (s *recordingCheckpointStore) Get(memory.CallerScope, string) (memory.TaskCheckpoint, error) {
	s.getCalls++
	return s.checkpoint, nil
}

func (s *recordingCheckpointStore) Apply(
	_ memory.CallerScope,
	_ string,
	mutation memory.CheckpointMutation,
) (memory.TaskCheckpoint, error) {
	s.mutations = append(s.mutations, mutation)
	result := s.checkpoint
	switch mutation.Action {
	case memory.CheckpointActionResume:
		result.Status = memory.CheckpointStatusActive
	case memory.CheckpointActionArchive:
		result.Status = memory.CheckpointStatusArchived
	}
	return result, nil
}

func TestCheckpointSemanticServiceAndNavigationPreserveOriginPage(t *testing.T) {
	store := &recordingCheckpointStore{checkpoint: memory.TaskCheckpoint{
		ID: "cp-private", Title: "Release", Objective: "Ship safely", Status: memory.CheckpointStatusSuspended,
	}}
	service := newCheckpointCommandService(store, memory.CallerScope{})
	_, err := service.list()
	require.NoError(t, err)
	_, err = service.detail("cp-private")
	require.NoError(t, err)
	_, err = service.resume("cp-private")
	require.NoError(t, err)
	_, err = service.archive("cp-private")
	require.NoError(t, err)
	assert.Equal(t, 1, store.listCalls)
	assert.Equal(t, 1, store.getCalls)
	require.Len(t, store.mutations, 2)
	assert.Equal(t, memory.CheckpointActionResume, store.mutations[0].Action)
	assert.Equal(t, memory.CheckpointActionArchive, store.mutations[1].Action)

	inbound := &bus.InboundContext{
		Channel: "telegram", Account: "telegram", ChatID: "42", ChatType: "direct", SenderID: "42",
	}
	scope := &session.SessionScope{Version: 1, AgentID: "main", Channel: "telegram", Account: "telegram"}
	checkpoint := store.checkpoint
	detail := buildCheckpointDetail(&AgentInstance{ID: "main"}, checkpoint, "si_v1_bound", scope, inbound, 3)
	confirm := buildCheckpointArchiveConfirm(&AgentInstance{ID: "main"}, checkpoint, "si_v1_bound", scope, inbound, 3)
	require.NotNil(t, detail.Interaction)
	require.NotNil(t, confirm.Interaction)
	assert.Equal(t, 3, detail.Interaction.Page)
	assert.Equal(t, 3, confirm.Interaction.Page)
	assert.Equal(t, 2, len(store.mutations), "rendering confirmation/cancel state must not mutate the checkpoint")
	foundCancel := false
	for _, entry := range confirm.Interaction.Entries {
		if entry.Action == "detail" && entry.Value == checkpoint.ID {
			foundCancel = true
		}
	}
	assert.True(t, foundCancel)
}

func configuredSearchModels() config.SecureModelList {
	models := make(config.SecureModelList, 0, 8)
	for i := 1; i <= 7; i++ {
		models = append(models, &config.ModelConfig{
			ModelName: fmt.Sprintf("gpt-alias-%d", i), Provider: "openai", Model: fmt.Sprintf("gpt-test-%d", i),
			Enabled: true, APIKeys: config.SimpleSecureStrings("test-key"),
		})
	}
	models = append(models, &config.ModelConfig{
		ModelName: "claude-alias", Provider: "anthropic", Model: "claude-test",
		Enabled: true, APIKeys: config.SimpleSecureStrings("test-key"),
	})
	return models
}

func TestModelSearchTextAndInteractivePathsShareSemanticSearchAndQueryState(t *testing.T) {
	al, cfg, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()
	cfg.Session.Dimensions = []string{"chat", "sender"}
	cfg.ModelList = configuredSearchModels()
	cfg.Agents.Defaults.Provider = "openai"

	msg := telegramSessionTestMessage("")
	route, agent, err := al.resolveMessageRoute(msg)
	require.NoError(t, err)
	allocation := al.allocateRouteSession(route, msg)
	ensureSessionMetadata(agent.Sessions, allocation.SessionKey, &allocation.Scope, allocation.SessionAliases)
	mcx := modelCommandContext{
		Agent: agent, SessionKey: allocation.SessionKey, Scope: &allocation.Scope, Inbound: &msg.Context,
	}

	textual, err := al.executeModelCommand(context.Background(), mcx, commands.ModelCommandRequest{
		Operation: "search", Argument: "gpt",
	})
	require.NoError(t, err)
	require.NotNil(t, textual.Interaction)
	assert.Equal(t, "gpt", textual.Interaction.Query)
	assert.Equal(t, 2, textual.Interaction.Pages)
	assert.Contains(t, textual.Title, "gpt")

	base := bus.InternalCallbackRequest{
		Kind: "model", Action: "search", Value: "gpt",
		OwnerID: msg.Context.SenderID, Channel: msg.Context.Channel, Account: msg.Context.Account,
		ChatID: msg.Context.ChatID, TopicID: msg.Context.TopicID, AgentID: agent.ID,
		SessionKey: allocation.SessionKey, Scope: session.CanonicalScopeSignature(allocation.Scope), Inbound: msg.Context,
	}
	interactive, err := al.handleInternalCallback(context.Background(), base)
	require.NoError(t, err)
	require.NotNil(t, interactive)
	require.NotNil(t, interactive.Content)
	require.NotNil(t, interactive.Content.Interaction)
	assert.Equal(t, bus.InteractionAppendContinuation, interactive.Transition)
	assert.Equal(t, textual.Title, interactive.Content.Title)
	assert.Equal(t, "gpt", interactive.Content.Interaction.Query)
	assert.Equal(t, allocation.SessionKey, interactive.Content.Interaction.SessionKey)
	assert.NotContains(t, base.Value, "/model search")

	var nextState modelMenuState
	foundNext := false
	for _, entry := range interactive.Content.Interaction.Entries {
		if entry.Action != "page" {
			continue
		}
		require.NoError(t, jsonUnmarshalModelState(entry.Value, &nextState))
		if nextState.Page == 1 {
			foundNext = true
			break
		}
	}
	require.True(t, foundNext)
	assert.Equal(t, "search", nextState.View)
	assert.Equal(t, "gpt", nextState.Query)

	pageReq := base
	pageReq.Action = "page"
	pageReq.Value = mustModelState(nextState)
	pageReq.Query = "gpt"
	paged, err := al.handleInternalCallback(context.Background(), pageReq)
	require.NoError(t, err)
	require.NotNil(t, paged.Content.Interaction)
	assert.Equal(t, 1, paged.Content.Interaction.Page)
	assert.Equal(t, "gpt", paged.Content.Interaction.Query)
	assert.Equal(t, bus.InteractionTransition(""), paged.Transition, "callback-only pagination should default to replace")

	tooLong := strings.Repeat("x", modelSearchQueryMaxRunes+1)
	_, err = al.executeModelCommand(context.Background(), mcx, commands.ModelCommandRequest{
		Operation: "search", Argument: tooLong,
	})
	require.EqualError(t, err, "model search query is too long")
	longReq := base
	longReq.Value = tooLong
	_, err = al.handleInternalCallback(context.Background(), longReq)
	require.EqualError(t, err, "model search query is too long")
}

func jsonUnmarshalModelState(raw string, state *modelMenuState) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("empty model state")
	}
	return json.Unmarshal([]byte(raw), state)
}
