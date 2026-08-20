package telegram

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestSkillAndCheckpointMenusUseOpaqueEntryCallbacks(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})
	for _, tc := range []struct {
		kind  string
		value string
	}{
		{kind: "skill", value: "private-skill-name"},
		{kind: "checkpoint", value: "cp_private_raw_identifier"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			inbound := bus.InboundContext{
				Channel: "telegram", Account: "telegram", ChatID: "12345", ChatType: "direct", SenderID: "42",
			}
			content := &bus.StructuredContent{Interaction: &bus.InteractionMenu{
				Kind: tc.kind, OwnerID: "42", Channel: "telegram", Account: "telegram", ChatID: "12345",
				AgentID: "main", Scope: "safe-scope", SessionKey: "si_v1_private_session_key", Inbound: inbound,
				Page: 0, Pages: 1, Entries: []bus.InteractionEntry{
					{Label: "1", Action: "detail", Value: tc.value},
					{Label: "Close", Action: "close"},
				},
			}}
			markup, pending, err := ch.structuredReplyMarkup(content, 12345, 0)
			require.NoError(t, err)
			require.NotNil(t, pending)
			require.NotNil(t, markup)
			assert.Equal(t, tc.value, pending.menu.Entries[0].Value)
			assert.Equal(t, "si_v1_private_session_key", pending.menu.SessionKey)
			for _, row := range markup.InlineKeyboard {
				for _, button := range row {
					assert.LessOrEqual(t, len([]byte(button.CallbackData)), 64)
					assert.NotContains(t, button.CallbackData, tc.value)
					assert.NotContains(t, button.CallbackData, "si_v1_private_session_key")
				}
			}
		})
	}
}

func TestUnknownInteractionKindFailsClosed(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})
	_, _, err := ch.structuredReplyMarkup(&bus.StructuredContent{Interaction: &bus.InteractionMenu{
		Kind: "command", OwnerID: "42", AgentID: "main",
	}}, 12345, 0)
	require.Error(t, err)
}

func TestInteractionMutationClaimIsExactlyOnceUnderRace(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})
	ch.storeSessionMenu(telegramSessionMenu{token: "race-token", createdAt: time.Now()})
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ch.claimSessionMenuMutation("race-token", "e0") {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), wins.Load())
}

func TestSkillSearchPromptIsOwnerBoundAndSingleUse(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{callFn: func(context.Context, string, *ta.RequestData) (*ta.Response, error) {
		return callbackSuccessResponse(t), nil
	}})
	menu := telegramSessionMenu{chatID: 12345, messageID: 91, menu: bus.InteractionMenu{
		Kind: "skill", OwnerID: "42", Channel: "telegram", Account: "telegram", ChatID: "12345",
		AgentID: "main", Scope: "safe-scope", SessionKey: "si_v1_bound", Inbound: bus.InboundContext{
			Channel: "telegram", Account: "telegram", ChatID: "12345", ChatType: "direct", SenderID: "42",
		},
	}}
	key := telegramSessionRenamePromptKey{chatID: 12345, messageID: 92}
	ch.storeSessionRenamePrompt(key, telegramSessionRenamePrompt{
		token: "skill-search", menu: menu, action: "search", createdAt: time.Now(),
	})
	wrong := &telego.Message{
		MessageID: 100,
		From:      &telego.User{ID: 99},
		Text:      "secret",
		Chat:      telego.Chat{ID: 12345},
		ReplyToMessage: &telego.Message{
			MessageID: 92,
		},
	}
	_, status := ch.claimSessionRenamePrompt(wrong)
	assert.Equal(t, sessionRenameClaimRejected, status)

	valid := *wrong
	valid.From = &telego.User{ID: 42}
	valid.Text = strings.Repeat("a", 12)
	_, status = ch.claimSessionRenamePrompt(&valid)
	assert.Equal(t, sessionRenameClaimed, status)
	_, status = ch.claimSessionRenamePrompt(&valid)
	assert.Equal(t, sessionRenameClaimReplay, status)
}
