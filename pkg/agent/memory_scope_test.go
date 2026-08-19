package agent

import (
	"path/filepath"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
)

func TestCallerScopeTelegramUserFollowsTopicsButNotAccountsOrUsers(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.IdentityLinks = map[string][]string{
		"alice": {"telegram:100", "telegram:alice-alt"},
	}
	base := bus.InboundContext{
		Channel: "telegram", Account: "personal", ChatID: "-100123/42",
		ChatType: "group", TopicID: "42", SenderID: "100", MessageID: "message-1",
		Raw: map[string]string{"topic_name": "OAuth"},
	}
	first := callerScopeFromInbound("main", "session-topic-42", &base, nil, cfg)
	secondInbound := base
	secondInbound.ChatID = "-100123/99"
	secondInbound.TopicID = "99"
	secondInbound.SenderID = "alice-alt"
	second := callerScopeFromInbound("main", "session-topic-99", &secondInbound, nil, cfg)
	if first.UserKey == "" || first.UserKey != second.UserKey {
		t.Fatalf("same canonical Telegram user keys = %q and %q", first.UserKey, second.UserKey)
	}
	if first.SessionRef == second.SessionRef {
		t.Fatalf("topic session refs unexpectedly equal: %q", first.SessionRef)
	}
	if first.GroupID != "-100123" || second.GroupID != "-100123" {
		t.Fatalf("group IDs = %q and %q, want -100123", first.GroupID, second.GroupID)
	}

	otherUserInbound := base
	otherUserInbound.SenderID = "200"
	otherUser := callerScopeFromInbound("main", "session-topic-42", &otherUserInbound, nil, cfg)
	if otherUser.UserKey == first.UserKey {
		t.Fatalf("different Telegram users share memory key %q", first.UserKey)
	}
	otherAccountInbound := base
	otherAccountInbound.Account = "work"
	otherAccount := callerScopeFromInbound("main", "session-topic-42", &otherAccountInbound, nil, cfg)
	if otherAccount.UserKey != first.UserKey {
		t.Fatalf(
			"different Telegram accounts SHOULD share person memory key %q, got %q",
			first.UserKey,
			otherAccount.UserKey,
		)
	}
	otherChannelInbound := base
	otherChannelInbound.Channel = "discord"
	otherChannel := callerScopeFromInbound("main", "session-topic-42", &otherChannelInbound, nil, cfg)
	if otherChannel.UserKey == first.UserKey {
		t.Fatalf("different channels share memory key %q", first.UserKey)
	}
}

func TestStructuredMemoryRootSeparatesAgentsAndLegacyMemory(t *testing.T) {
	workspace := t.TempDir()
	mainRoot := structuredMemoryRoot(workspace, "main")
	otherRoot := structuredMemoryRoot(workspace, "other")
	if mainRoot == otherRoot {
		t.Fatalf("agent memory roots are equal: %q", mainRoot)
	}
	legacyPath := filepath.Join(workspace, "memory", "MEMORY.md")
	if mainRoot == legacyPath || otherRoot == legacyPath {
		t.Fatal("structured storage overlaps legacy MEMORY.md")
	}
}

func TestDisabledMemoryFlagsPreservePreviousRuntimeShape(t *testing.T) {
	cfg := config.MemoryConfig{
		Enabled:     false,
		Recall:      config.MemoryRecallConfig{Mode: config.MemoryRecallIsolated},
		Checkpoints: config.MemoryCheckpointConfig{Enabled: false},
	}
	curated, recall, checkpoints, review := initializeAgentMemoryStores(
		t.TempDir(),
		"main",
		cfg,
	)
	if curated != nil || recall != nil || checkpoints != nil || review != nil {
		t.Fatalf(
			"disabled memory initialized stores: curated=%v recall=%v checkpoints=%v review=%v",
			curated,
			recall,
			checkpoints,
			review,
		)
	}
}

func TestPersonScopeRuntimeMigrationAndRestartIdempotency(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Memory.Enabled = true

	legacyTelegram := "channel:telegram|account:default|user:42"
	legacyPico := "channel:pico|account:default|user:pico-user"

	// 1. Seed legacy stores
	curatedStore, err := memory.NewCuratedStore(
		filepath.Join(structuredMemoryRoot(workspace, "main"), "curated"),
		memory.CuratedStoreOptions{
			WorkspaceCharLimit: 10_000,
			PerUserCharLimit:   10_000,
		},
	)
	if err != nil {
		t.Fatalf("failed to create curated store: %v", err)
	}

	_, _ = curatedStore.ApplyBatch(
		memory.CuratedTargetCurrentUser,
		memory.CallerScope{UserKey: legacyTelegram},
		[]memory.CuratedMutation{{
			Action:          memory.CuratedActionAdd,
			Content:         "Prefers native quizzes",
			Type:            memory.CuratedTypeWorkflowPreference,
			EvidenceKind:    memory.CuratedEvidenceExplicit,
			PreferenceKey:   "workflow.quiz_format",
			PreferenceValue: "telegram_native_quiz",
		}},
		false,
	)

	_, _ = curatedStore.ApplyBatch(
		memory.CuratedTargetCurrentUser,
		memory.CallerScope{UserKey: legacyPico},
		[]memory.CuratedMutation{{
			Action:          memory.CuratedActionAdd,
			Content:         "Prefers Indonesian language",
			Type:            memory.CuratedTypeCommunicationPreference,
			EvidenceKind:    memory.CuratedEvidenceExplicit,
			PreferenceKey:   "language.primary",
			PreferenceValue: "id",
		}},
		false,
	)

	// 2. Configure identity link
	cfg.Session.IdentityLinks = map[string][]string{
		"alice": {"telegram:default:42", "pico:default:pico-user"},
	}

	// 3. Initialize runtime (instance creation triggers migration)
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, &mockProvider{})
	if agent == nil || agent.CuratedMemory == nil {
		t.Fatal("failed to initialize agent instance")
	}

	// 4. Verify canonical person scope is active and old memory is visible
	personCaller := memory.CallerScope{UserKey: "person:alice"}
	entries, err := agent.CuratedMemory.List(memory.CuratedTargetCurrentUser, personCaller)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf(
			"expected 2 migrated entries in person:alice, got %d: %#v",
			len(entries),
			entries,
		)
	}

	// 5. Restart runtime again
	restartedAgent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, &mockProvider{})
	restartedEntries, err := restartedAgent.CuratedMemory.List(
		memory.CuratedTargetCurrentUser,
		personCaller,
	)
	if err != nil {
		t.Fatalf("List error after restart: %v", err)
	}
	if len(restartedEntries) != 2 {
		t.Fatalf(
			"expected exactly 2 entries (zero duplicates) after restart, got %d: %#v",
			len(restartedEntries),
			restartedEntries,
		)
	}
}
