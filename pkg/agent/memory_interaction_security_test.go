package agent

import (
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
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
