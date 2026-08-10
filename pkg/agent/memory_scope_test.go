package agent

import (
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
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
	if otherAccount.UserKey == first.UserKey {
		t.Fatalf("different Telegram accounts share memory key %q", first.UserKey)
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
		t.Fatalf("disabled memory initialized stores: curated=%v recall=%v checkpoints=%v review=%v", curated, recall, checkpoints, review)
	}
}
