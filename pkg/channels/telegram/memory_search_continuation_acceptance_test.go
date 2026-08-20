package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/channels"
)

func TestMemorySearchReplyAppendsPrivateContinuationAndRetiresOldMenu(t *testing.T) {
	const (
		chatID             = int64(-10055)
		threadID           = 7
		ownerID            = int64(42)
		dashboardMessageID = 77
		promptMessageID    = 78
		resultMessageID    = 79
	)
	sendMessageCalls := 0
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		switch {
		case strings.Contains(url, "sendMessage"):
			sendMessageCalls++
			switch sendMessageCalls {
			case 1:
				return successEphemeralResponse(t, chatID, threadID, ownerID, dashboardMessageID), nil
			case 2:
				return successEphemeralResponse(t, chatID, threadID, ownerID, promptMessageID), nil
			case 3:
				return successEphemeralResponse(t, chatID, threadID, ownerID, resultMessageID), nil
			default:
				return nil, errors.New("unexpected extra sendMessage")
			}
		case strings.Contains(url, "answerCallbackQuery"), strings.Contains(url, "editEphemeralMessageReplyMarkup"):
			return callbackSuccessResponse(t), nil
		default:
			return nil, errors.New("unexpected API call " + url)
		}
	}}
	ch := newTestChannel(t, caller)
	messageBus := bus.NewMessageBus()
	ch.BaseChannel = channels.NewBaseChannel("telegram", nil, messageBus, nil)
	target := mustRegisterEphemeralTarget(t, ch, chatID, threadID, ownerID, 0, "origin-callback")
	content := testMemoryStructuredContent()
	content.Interaction.ChatID = strconvFormatChat(chatID, threadID)
	content.Interaction.TopicID = strconv.Itoa(threadID)
	content.Interaction.OwnerID = strconv.FormatInt(ownerID, 10)
	content.Interaction.Inbound = privateOutboundContext(target)
	content.Interaction.Inbound.PrivateSession = true

	_, err := ch.sendStructuredContent(
		context.Background(),
		bus.OutboundMessage{Content: content.FallbackText(), Structured: content},
		chatID,
		threadID,
		&target,
	)
	require.NoError(t, err)

	var oldToken string
	ch.sessionMenuMu.Lock()
	for token := range ch.sessionMenus {
		oldToken = token
	}
	ch.sessionMenuMu.Unlock()
	require.Len(t, oldToken, 12)

	ch.SetInternalCallbackHandler(
		func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
			if strings.TrimSpace(req.Value) == "" {
				return &bus.InternalCallbackResponse{Text: "Reply to this prompt with a memory query to search:"}, nil
			}
			updated := testMemoryStructuredContent()
			updated.Title = "Memory Search Results"
			updated.Interaction.ChatID = strconvFormatChat(chatID, threadID)
			updated.Interaction.TopicID = strconv.Itoa(threadID)
			updated.Interaction.OwnerID = strconv.FormatInt(ownerID, 10)
			updated.Interaction.Query = strings.TrimSpace(req.Value)
			updated.Interaction.Inbound = privateOutboundContext(target)
			updated.Interaction.Inbound.PrivateSession = true
			return &bus.InternalCallbackResponse{
				Content: updated, Transition: bus.InteractionAppendContinuation,
			}, nil
		},
	)

	var dashboard struct {
		ReplyMarkup telego.InlineKeyboardMarkup `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &dashboard))
	search := findInlineButton(t, &dashboard.ReplyMarkup, "🔎 Search")
	require.NoError(t, ch.handleCallbackQuery(context.Background(), &telego.CallbackQuery{
		ID: "memory-search", From: telego.User{ID: ownerID}, Data: search.CallbackData,
		Message: &telego.Message{
			EphemeralMessageID: dashboardMessageID, MessageThreadID: threadID,
			Chat: telego.Chat{ID: chatID, Type: telego.ChatTypeSupergroup, IsForum: true},
		},
	}))

	reply := &telego.Message{
		EphemeralMessageID: 80, MessageThreadID: threadID,
		From: &telego.User{ID: ownerID}, Text: "golang concurrency",
		Chat: telego.Chat{ID: chatID, Type: telego.ChatTypeSupergroup, IsForum: true},
		ReplyToMessage: &telego.Message{
			EphemeralMessageID: promptMessageID, MessageThreadID: threadID, Chat: telego.Chat{ID: chatID},
		},
	}
	require.NoError(t, ch.handleMessage(context.Background(), reply))
	assert.Equal(t, 3, sendMessageCalls)

	for _, call := range caller.calls {
		assert.NotContains(
			t,
			call.URL,
			"editEphemeralMessageText",
			"search result must append instead of editing the old card",
		)
	}
	var resultSend struct {
		ReceiverUserID int64 `json:"receiver_user_id"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[3].Data.BodyRaw, &resultSend))
	assert.Equal(
		t,
		ownerID,
		resultSend.ReceiverUserID,
		"continuation must preserve verified private receiver authority",
	)

	ch.sessionMenuMu.Lock()
	_, oldStillActive := ch.sessionMenus[oldToken]
	activeCount := len(ch.sessionMenus)
	var active telegramSessionMenu
	for _, menu := range ch.sessionMenus {
		active = menu
	}
	ch.sessionMenuMu.Unlock()
	assert.False(t, oldStillActive)
	assert.Equal(t, 1, activeCount)
	assert.Equal(t, resultMessageID, active.ephemeralID)
	assert.Equal(t, ownerID, active.receiverUserID)
	assert.Equal(t, "golang concurrency", active.menu.Query)

	select {
	case inbound := <-messageBus.InboundChan():
		t.Fatalf("memory search reply leaked to agent pipeline: %+v", inbound)
	default:
	}
}
