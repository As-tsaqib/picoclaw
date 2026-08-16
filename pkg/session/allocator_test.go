package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/routing"
)

func TestAllocateRouteSession_PerPeerDM(t *testing.T) {
	allocation := AllocateRouteSession(AllocationInput{
		AgentID: "main",
		Context: bus.InboundContext{
			Channel:  "telegram",
			Account:  "default",
			ChatID:   "dm-123",
			ChatType: "direct",
			SenderID: "User123",
		},
		SessionPolicy: routing.SessionPolicy{
			Dimensions: []string{"sender"},
		},
	})

	if allocation.SessionKey == "" || !IsOpaqueSessionKey(allocation.SessionKey) {
		t.Fatalf("SessionKey = %q, want opaque session key", allocation.SessionKey)
	}
	if !containsAlias(allocation.SessionAliases, "agent:main:direct:user123") {
		t.Fatalf("SessionAliases = %v, want to contain agent:main:direct:user123", allocation.SessionAliases)
	}
	if allocation.MainSessionKey == "" || !IsOpaqueSessionKey(allocation.MainSessionKey) {
		t.Fatalf("MainSessionKey = %q, want opaque session key", allocation.MainSessionKey)
	}
	if len(allocation.MainAliases) != 1 || allocation.MainAliases[0] != "agent:main:main" {
		t.Fatalf("MainAliases = %v, want [agent:main:main]", allocation.MainAliases)
	}
	if allocation.Scope.Version != ScopeVersionV1 {
		t.Fatalf("Scope.Version = %d, want %d", allocation.Scope.Version, ScopeVersionV1)
	}
	if len(allocation.Scope.Dimensions) != 1 || allocation.Scope.Dimensions[0] != "sender" {
		t.Fatalf("Scope.Dimensions = %v, want [sender]", allocation.Scope.Dimensions)
	}
	if allocation.Scope.Values["sender"] != "user123" {
		t.Fatalf("Scope.Values[sender] = %q, want user123", allocation.Scope.Values["sender"])
	}
}

func TestAllocateRouteSession_GroupPeer(t *testing.T) {
	allocation := AllocateRouteSession(AllocationInput{
		AgentID: "main",
		Context: bus.InboundContext{
			Channel:  "slack",
			Account:  "workspace-a",
			ChatID:   "C001",
			ChatType: "channel",
			SenderID: "U001",
		},
		SessionPolicy: routing.SessionPolicy{
			Dimensions: []string{"chat"},
		},
	})

	if allocation.SessionKey == "" || !IsOpaqueSessionKey(allocation.SessionKey) {
		t.Fatalf("SessionKey = %q, want opaque session key", allocation.SessionKey)
	}
	if !containsAlias(allocation.SessionAliases, "agent:main:slack:channel:c001") {
		t.Fatalf("SessionAliases = %v, want to contain agent:main:slack:channel:c001", allocation.SessionAliases)
	}
	if allocation.MainSessionKey == "" || !IsOpaqueSessionKey(allocation.MainSessionKey) {
		t.Fatalf("MainSessionKey = %q, want opaque session key", allocation.MainSessionKey)
	}
	if len(allocation.MainAliases) != 1 || allocation.MainAliases[0] != "agent:main:main" {
		t.Fatalf("MainAliases = %v, want [agent:main:main]", allocation.MainAliases)
	}
	if len(allocation.Scope.Dimensions) != 1 || allocation.Scope.Dimensions[0] != "chat" {
		t.Fatalf("Scope.Dimensions = %v, want [chat]", allocation.Scope.Dimensions)
	}
	if allocation.Scope.Values["chat"] != "channel:c001" {
		t.Fatalf("Scope.Values[chat] = %q, want channel:c001", allocation.Scope.Values["chat"])
	}
}

func TestAllocateRouteSession_TelegramForumTopicsRemainIsolatedByDefault(t *testing.T) {
	first := AllocateRouteSession(AllocationInput{
		AgentID: "main",
		Context: bus.InboundContext{
			Channel:  "telegram",
			ChatID:   "-1001234567890",
			ChatType: "group",
			TopicID:  "42",
			SenderID: "7",
		},
		SessionPolicy: routing.SessionPolicy{
			Dimensions: []string{"chat"},
		},
	})
	second := AllocateRouteSession(AllocationInput{
		AgentID: "main",
		Context: bus.InboundContext{
			Channel:  "telegram",
			ChatID:   "-1001234567890",
			ChatType: "group",
			TopicID:  "99",
			SenderID: "7",
		},
		SessionPolicy: routing.SessionPolicy{
			Dimensions: []string{"chat"},
		},
	})

	if first.SessionKey == second.SessionKey {
		t.Fatalf("forum topics should not share default session key: %q", first.SessionKey)
	}
	if got := first.Scope.Values["chat"]; got != "group:-1001234567890/42" {
		t.Fatalf("first.Scope.Values[chat] = %q, want %q", got, "group:-1001234567890/42")
	}
	if got := second.Scope.Values["chat"]; got != "group:-1001234567890/99" {
		t.Fatalf("second.Scope.Values[chat] = %q, want %q", got, "group:-1001234567890/99")
	}
}

func TestAllocateRouteSession_TelegramEphemeralUsersAreIsolated(t *testing.T) {
	allocate := func(sender string) Allocation {
		return AllocateRouteSession(AllocationInput{
			AgentID: "main",
			Context: bus.InboundContext{
				Channel:         "telegram",
				ChatID:          "-1001234567890",
				ChatType:        "group",
				SenderID:        sender,
				PrivateResponse: true,
				PrivateSession:  true,
			},
			SessionPolicy: routing.SessionPolicy{Dimensions: []string{"chat"}},
		})
	}

	userA := allocate("101")
	userB := allocate("202")
	if userA.SessionKey == userB.SessionKey {
		t.Fatalf("two ephemeral users shared session %q", userA.SessionKey)
	}
	if got := userA.Scope.Dimensions; len(got) != 2 || got[0] != "chat" || got[1] != "sender" {
		t.Fatalf("ephemeral dimensions = %v, want [chat sender]", got)
	}
	if userA.Scope.Values["sender"] != "101" || userB.Scope.Values["sender"] != "202" {
		t.Fatalf("sender scope values = %q, %q", userA.Scope.Values["sender"], userB.Scope.Values["sender"])
	}
	if containsAlias(userA.SessionAliases, "agent:main:telegram:group:-1001234567890") {
		t.Fatalf("private aliases must not include shared group alias: %v", userA.SessionAliases)
	}
}

func TestAllocateRouteSession_TelegramEphemeralAlwaysIncludesGroupAndRawUser(t *testing.T) {
	allocation := AllocateRouteSession(AllocationInput{
		AgentID: "main",
		Context: bus.InboundContext{
			Channel:         "telegram",
			ChatID:          "-1009876543210",
			ChatType:        "group",
			SenderID:        "707",
			PrivateResponse: true,
			PrivateSession:  true,
		},
		SessionPolicy: routing.SessionPolicy{
			Dimensions:    []string{"Sender"},
			IdentityLinks: map[string][]string{"shared": {"telegram:707", "telegram:808"}},
		},
	})
	if got := allocation.Scope.Dimensions; len(got) != 2 || got[0] != "sender" || got[1] != "chat" {
		t.Fatalf("private dimensions = %v, want [sender chat]", got)
	}
	if allocation.Scope.Values["sender"] != "707" || allocation.Scope.Values["chat"] != "group:-1009876543210" {
		t.Fatalf("private scope values = %v", allocation.Scope.Values)
	}
	otherInput := AllocationInput{
		AgentID: "main",
		Context: bus.InboundContext{
			Channel:         "telegram",
			ChatID:          "-1009876543211",
			ChatType:        "group",
			SenderID:        "808",
			PrivateResponse: true,
			PrivateSession:  true,
		},
		SessionPolicy: routing.SessionPolicy{
			Dimensions:    []string{"Sender"},
			IdentityLinks: map[string][]string{"shared": {"telegram:707", "telegram:808"}},
		},
	}
	other := AllocateRouteSession(otherInput)
	if allocation.SessionKey == other.SessionKey {
		t.Fatal("linked Telegram users or separate groups shared a private session")
	}
}

func TestAllocateRouteSession_TelegramNormalGroupRemainsShared(t *testing.T) {
	allocate := func(sender string) Allocation {
		return AllocateRouteSession(AllocationInput{
			AgentID: "main",
			Context: bus.InboundContext{
				Channel:  "telegram",
				ChatID:   "-1001234567890",
				ChatType: "group",
				SenderID: sender,
			},
			SessionPolicy: routing.SessionPolicy{Dimensions: []string{"chat"}},
		})
	}
	if first, second := allocate("101"), allocate("202"); first.SessionKey != second.SessionKey {
		t.Fatalf("normal group session behavior changed: %q != %q", first.SessionKey, second.SessionKey)
	}
}

func TestAllocateRouteSession_TelegramEphemeralHistoryIsSeparateFromPublicHistory(t *testing.T) {
	baseContext := bus.InboundContext{
		Channel:  "telegram",
		ChatID:   "-1001234567890",
		ChatType: "group",
		SenderID: "101",
	}
	public := AllocateRouteSession(AllocationInput{
		AgentID:       "main",
		Context:       baseContext,
		SessionPolicy: routing.SessionPolicy{Dimensions: []string{"chat", "sender"}},
	})
	privateContext := baseContext
	privateContext.PrivateResponse = true
	privateContext.PrivateSession = true
	private := AllocateRouteSession(AllocationInput{
		AgentID:       "main",
		Context:       privateContext,
		SessionPolicy: routing.SessionPolicy{Dimensions: []string{"chat", "sender"}},
	})

	if public.SessionKey == private.SessionKey {
		t.Fatalf("public and private histories shared session %q", public.SessionKey)
	}
}

func TestSessionScope_PrivateMarkerPersistsWithoutRouteCapability(t *testing.T) {
	scope := SessionScope{
		Version:           ScopeVersionV1,
		AgentID:           "main",
		Channel:           "telegram",
		PrivateResponse:   true,
		PrivateRouteToken: "synthetic-process-local-capability",
	}
	data, err := json.Marshal(scope)
	if err != nil {
		t.Fatalf("marshal private scope: %v", err)
	}
	if string(data) == "" || !strings.Contains(string(data), `"private_response":true`) {
		t.Fatalf("private marker was not persisted: %s", data)
	}
	if strings.Contains(string(data), scope.PrivateRouteToken) {
		t.Fatalf("private route capability was persisted: %s", data)
	}

	var restored SessionScope
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal private scope: %v", err)
	}
	if !restored.PrivateResponse || restored.PrivateRouteToken != "" {
		t.Fatalf("restored private scope = %+v", restored)
	}
}

func TestAllocateRouteSession_PicoDirectAliasesIncludeLegacyChatKey(t *testing.T) {
	allocation := AllocateRouteSession(AllocationInput{
		AgentID: "main",
		Context: bus.InboundContext{
			Channel:  "pico",
			Account:  "default",
			ChatID:   "pico:session-123",
			ChatType: "direct",
			SenderID: "pico-user",
		},
		SessionPolicy: routing.SessionPolicy{
			Dimensions: []string{"sender"},
		},
	})

	if !containsAlias(allocation.SessionAliases, "agent:main:pico:direct:pico:session-123") {
		t.Fatalf("SessionAliases = %v, want pico legacy alias", allocation.SessionAliases)
	}
}

func TestBuildOpaqueSessionKey_IsStable(t *testing.T) {
	first := BuildOpaqueSessionKey("agent:main:direct:user123")
	second := BuildOpaqueSessionKey("agent:main:direct:user123")
	if first != second {
		t.Fatalf("BuildOpaqueSessionKey() mismatch: %q != %q", first, second)
	}
	if !IsOpaqueSessionKey(first) {
		t.Fatalf("expected opaque session key, got %q", first)
	}
}

func containsAlias(aliases []string, want string) bool {
	for _, alias := range aliases {
		if alias == want {
			return true
		}
	}
	return false
}

func TestTelegramOriginMetadataUsesNumericOwnershipFailClosed(t *testing.T) {
	base := bus.InboundContext{
		Channel: "telegram", ChatID: "42", ChatType: "direct", SenderID: "42",
		Raw: map[string]string{"platform": "telegram"},
	}
	allocation := AllocateRouteSession(AllocationInput{
		AgentID: "main", Context: base, SessionPolicy: routing.SessionPolicy{Dimensions: []string{"chat"}},
	})
	assert.Equal(t, "42", allocation.Scope.OwnerUserID)
	assert.Equal(t, "telegram", allocation.Scope.Platform)
	assert.Equal(t, "telegram", allocation.Scope.BotAccount)
	assert.Equal(t, "42", allocation.Scope.OriginChatID)

	group := base
	group.ChatID = "-1001"
	group.ChatType = "group"
	shared := AllocateRouteSession(AllocationInput{
		AgentID: "main", Context: group, SessionPolicy: routing.SessionPolicy{Dimensions: []string{"chat"}},
	})
	assert.Empty(t, shared.Scope.OwnerUserID, "shared group session must not be claimed by the sender")

	owned := AllocateRouteSession(AllocationInput{
		AgentID: "main", Context: group, SessionPolicy: routing.SessionPolicy{Dimensions: []string{"chat", "sender"}},
	})
	assert.Equal(
		t, "42", owned.Scope.OwnerUserID,
		"sender-scoped group session may be owned by that numeric Telegram user",
	)

	username := base
	username.SenderID = "@alice"
	untrusted := AllocateRouteSession(AllocationInput{
		AgentID:       "main",
		Context:       username,
		SessionPolicy: routing.SessionPolicy{Dimensions: []string{"chat", "sender"}},
	})
	assert.Empty(t, untrusted.Scope.OwnerUserID, "username must never be accepted as owner authorization")
}
