package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestAppendContinuationInteractionValidationFailureKeepsOldMenuActive(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		return nil, errors.New("unexpected Telegram call " + url)
	}}
	ch := newTestChannel(t, caller)

	oldContent := testSearchContinuationContent("model", "", "si_v1_session-a")
	old := telegramSessionMenu{
		token:     "old-menu",
		menu:      *oldContent.Interaction,
		chatID:    12345,
		messageID: 91,
		createdAt: time.Now(),
	}
	ch.storeSessionMenu(old)

	invalid := testSearchContinuationContent("model", "gpt", "si_v1_session-a")
	invalid.Interaction.AgentID = ""
	err := ch.applyInteractionResponse(
		context.Background(),
		&telego.Message{MessageID: 100, Chat: telego.Chat{ID: 12345}},
		old.token,
		old,
		&bus.InternalCallbackResponse{
			Content:    invalid,
			Transition: bus.InteractionAppendContinuation,
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prepare interaction continuation")
	assert.Empty(t, caller.calls, "known interaction validation failure must happen before Telegram send")

	registeredOld, ok := ch.takeSessionMenu(old.token)
	require.True(t, ok, "failed continuation must preserve the old server-side capability")
	action, _, actionable := resolveSessionMenuAction(registeredOld.menu, "m0")
	assert.True(t, actionable, "old menu must remain actionable")
	assert.Equal(t, "detail", action)

	ch.sessionMenuMu.Lock()
	activeCount := len(ch.sessionMenus)
	ch.sessionMenuMu.Unlock()
	assert.Equal(t, 1, activeCount, "failed continuation must not create a competing callback capability")
	for _, call := range caller.calls {
		assert.False(
			t,
			strings.Contains(call.URL, "editMessageReplyMarkup"),
			"old keyboard must not be retired when continuation establishment fails",
		)
	}
}

func TestOrdinaryStructuredSendStillDegradesWhenInteractionValidationFails(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		if strings.Contains(url, "sendRichMessage") {
			return successResponseWithMessageID(t, 101), nil
		}
		return nil, errors.New("unexpected Telegram call " + url)
	}}
	ch := newTestChannel(t, caller)
	content := testSearchContinuationContent("model", "gpt", "si_v1_session-a")
	content.Interaction.AgentID = ""

	ids, err := ch.sendStructuredContent(
		context.Background(),
		bus.OutboundMessage{Content: content.FallbackText(), Structured: content},
		12345,
		0,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"101"}, ids)
	require.Len(t, caller.calls, 1)
	assert.Contains(t, caller.calls[0].URL, "sendRichMessage")

	ch.sessionMenuMu.Lock()
	activeCount := len(ch.sessionMenus)
	ch.sessionMenuMu.Unlock()
	assert.Zero(t, activeCount, "ordinary degradation may send noninteractive output but must not invent a menu")
}
