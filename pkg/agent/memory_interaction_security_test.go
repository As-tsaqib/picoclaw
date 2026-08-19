package agent

import (
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
)

func TestNewMemoryInteractionMenuBindsTrustedRouteEnvelope(t *testing.T) {
	inbound := &bus.InboundContext{
		Channel:  "telegram",
		Account:  "default",
		ChatID:   "-10042/7",
		TopicID:  "7",
		SenderID: "42",
		Raw:      map[string]string{"telegram_ephemeral": "true"},
	}
	entries := []bus.InteractionEntry{{Action: "browse", Label: "Browse"}}

	menu, err := newMemoryInteractionMenu(inbound, "main", 2, 4, "search terms", entries)
	if err != nil {
		t.Fatalf("newMemoryInteractionMenu() error = %v", err)
	}
	if menu.Kind != "memory" || menu.OwnerID != "42" || menu.AgentID != "main" {
		t.Fatalf("identity envelope = %#v", menu)
	}
	if menu.Channel != "telegram" || menu.Account != "default" || menu.ChatID != "-10042/7" || menu.TopicID != "7" {
		t.Fatalf("route envelope = %#v", menu)
	}
	if menu.Inbound.SenderID != menu.OwnerID || menu.Inbound.Channel != menu.Channel ||
		menu.Inbound.Account != menu.Account || menu.Inbound.ChatID != menu.ChatID || menu.Inbound.TopicID != menu.TopicID {
		t.Fatalf("trusted inbound mismatch: menu=%#v inbound=%#v", menu, menu.Inbound)
	}
	if menu.Page != 2 || menu.Pages != 4 || menu.Current != "search terms" || len(menu.Entries) != 1 {
		t.Fatalf("view state = %#v", menu)
	}

	entries[0].Label = "mutated"
	inbound.Raw["telegram_ephemeral"] = "mutated"
	if menu.Entries[0].Label != "Browse" {
		t.Fatal("menu entries alias caller-owned slice")
	}
	if menu.Inbound.Raw["telegram_ephemeral"] != "true" {
		t.Fatal("menu inbound aliases caller-owned metadata")
	}
}

func TestNewMemoryInteractionMenuRejectsIncompleteTrustedRoute(t *testing.T) {
	base := bus.InboundContext{
		Channel:  "telegram",
		Account:  "default",
		ChatID:   "42",
		SenderID: "42",
	}
	tests := []struct {
		name   string
		mutate func(*bus.InboundContext)
	}{
		{name: "channel", mutate: func(v *bus.InboundContext) { v.Channel = "" }},
		{name: "account", mutate: func(v *bus.InboundContext) { v.Account = "" }},
		{name: "chat", mutate: func(v *bus.InboundContext) { v.ChatID = "" }},
		{name: "owner", mutate: func(v *bus.InboundContext) { v.SenderID = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbound := base
			tt.mutate(&inbound)
			if _, err := newMemoryInteractionMenu(&inbound, "main", 0, 1, "", nil); err == nil {
				t.Fatal("expected incomplete trusted route to be rejected")
			}
		})
	}
	if _, err := newMemoryInteractionMenu(&base, "", 0, 1, "", nil); err == nil {
		t.Fatal("expected empty agent to be rejected")
	}
}

func TestMemoryInteractionPrivateRouteRequiresTrustedCapability(t *testing.T) {
	publicGroup := &bus.InboundContext{
		Channel: "telegram", Account: "default", ChatID: "-10042/7", ChatType: "group",
		TopicID: "7", SenderID: "42",
	}
	if memoryInteractionRouteIsPrivate(publicGroup) {
		t.Fatal("public group route must not authorize personal-memory dashboard")
	}

	booleanOnly := *publicGroup
	booleanOnly.PrivateResponse = true
	if memoryInteractionRouteIsPrivate(&booleanOnly) {
		t.Fatal("private-response boolean without route capability must fail closed")
	}

	verified := booleanOnly
	verified.PrivateRouteToken = "opaque-channel-capability"
	if !memoryInteractionRouteIsPrivate(&verified) {
		t.Fatal("verified private route should authorize receiver-bound dashboard")
	}

	direct := *publicGroup
	direct.ChatType = "direct"
	direct.ChatID = "42"
	direct.TopicID = ""
	if !memoryInteractionRouteIsPrivate(&direct) {
		t.Fatal("direct chat should authorize personal-memory dashboard")
	}
}

func TestMemoryInteractionCallerScopeOnlyPrivatizesVerifiedRoute(t *testing.T) {
	caller := memory.CallerScope{UserKey: "telegram:default:42", GroupID: "-10042"}
	publicGroup := &bus.InboundContext{ChatType: "group"}
	if got := memoryInteractionCallerScope(caller, publicGroup); got.GroupID != caller.GroupID {
		t.Fatalf("public group scope widened: %#v", got)
	}

	booleanOnly := &bus.InboundContext{ChatType: "group", PrivateResponse: true}
	if got := memoryInteractionCallerScope(caller, booleanOnly); got.GroupID != caller.GroupID {
		t.Fatalf("unverified private scope widened: %#v", got)
	}

	verified := &bus.InboundContext{
		ChatType: "group", PrivateResponse: true, PrivateRouteToken: "opaque-channel-capability",
	}
	got := memoryInteractionCallerScope(caller, verified)
	if got.GroupID != "" || !memory.AllowsPrivateUserMemory(got) {
		t.Fatalf("verified private route did not enable receiver-scoped memory: %#v", got)
	}
}
