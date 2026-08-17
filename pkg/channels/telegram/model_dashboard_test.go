package telegram

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestModelInteractionKeyboardUsesNumericSelectorsWithoutModelLeak(t *testing.T) {
	longModel := "provider/this-is-a-very-long-model-identifier-that-must-stay-server-side-and-out-of-callback-data"
	entries := make([]bus.InteractionEntry, 0, 9)
	for i := 1; i <= 5; i++ {
		entries = append(entries, bus.InteractionEntry{
			Label:  string(rune('0' + i)),
			Action: "select",
			Value:  `{"provider":"openai","model":"` + longModel + `","config_ref":"main"}`,
		})
	}
	entries = append(entries,
		bus.InteractionEntry{Label: "Halaman 1/2", Action: "noop"},
		bus.InteractionEntry{Label: "▶️", Action: "page", Value: `{"view":"available","page":1}`},
		bus.InteractionEntry{Label: "🔃 Refresh", Action: "refresh", Value: `{"view":"available"}`},
		bus.InteractionEntry{Label: "✖️ Tutup", Action: "close"},
	)
	menu := bus.InteractionMenu{Kind: "model", Entries: entries, Page: 0, Pages: 2}
	keyboard := modelInteractionKeyboard(menu, func(code string) string {
		return "pcsm:abcdefghijkl:" + code
	})
	require.NotEmpty(t, keyboard)
	require.Len(t, keyboard[0], 5)
	for i, button := range keyboard[0] {
		assert.Equal(t, string(rune('1'+i)), button.Text)
		assert.LessOrEqual(t, len([]byte(button.CallbackData)), 64)
		assert.False(t, strings.Contains(button.CallbackData, longModel))
	}

	action, value, ok := resolveSessionMenuAction(menu, "m0")
	require.True(t, ok)
	assert.Equal(t, "select", action)
	assert.Contains(t, value, longModel)
}

func TestModelMenuExpiresBeforeSessionMenu(t *testing.T) {
	now := time.Now()
	ch := &TelegramChannel{sessionMenus: map[string]telegramSessionMenu{
		"model-token": {
			token:     "model-token",
			menu:      bus.InteractionMenu{Kind: "model"},
			createdAt: now.Add(-modelMenuTTL - time.Second),
		},
		"session-token": {
			token:     "session-token",
			menu:      bus.InteractionMenu{Kind: "session"},
			createdAt: now.Add(-modelMenuTTL - time.Second),
		},
	}}

	ch.pruneSessionMenusLocked(now)
	_, modelPresent := ch.sessionMenus["model-token"]
	_, sessionPresent := ch.sessionMenus["session-token"]
	assert.False(t, modelPresent)
	assert.True(t, sessionPresent)
}
