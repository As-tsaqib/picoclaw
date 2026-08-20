package telegram

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestDiscoveryStaleMenuTokenIsRejected(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})
	ch.storeSessionMenu(telegramSessionMenu{
		token: "stale-token",
		menu: bus.InteractionMenu{
			Kind: "discovery", OwnerID: "42", Channel: "telegram", Account: "telegram",
			ChatID: "12345", AgentID: "main", Scope: "safe-scope", SessionKey: "si_v1_bound",
			Inbound: bus.InboundContext{
				Channel: "telegram", Account: "telegram", ChatID: "12345", ChatType: "direct", SenderID: "42",
			},
		},
		chatID: 12345, createdAt: time.Now().Add(-sessionMenuTTL - time.Second),
	})
	_, ok := ch.takeSessionMenu("stale-token")
	assert.False(t, ok)
}
