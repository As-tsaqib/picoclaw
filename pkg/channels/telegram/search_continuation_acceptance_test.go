package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/channels"
)

func testSearchContinuationContent(kind, query, sessionKey string) *bus.StructuredContent {
	inbound := bus.InboundContext{
		Channel: "telegram", Account: "telegram", ChatID: "12345", ChatType: "direct", SenderID: "42",
	}
	return &bus.StructuredContent{
		Title:      "Search Results",
		Paragraphs: []string{"Result for " + query},
		Interaction: &bus.InteractionMenu{
			Kind: kind, OwnerID: "42", Channel: "telegram", Account: "telegram", ChatID: "12345",
			AgentID: "main", Scope: "scope-a", SessionKey: sessionKey, Query: query, Inbound: inbound,
			Page: 0, Pages: 1,
			Entries: []bus.InteractionEntry{
				{Label: "1", Action: "detail", Value: "raw-result-secret"},
				{Label: "🔎 Search", Action: "search"},
				{Label: "✖️ Close", Action: "close"},
			},
		},
	}
}

func TestSearchPromptReplyAppendsNewInteractionAndRetiresOldMenu(t *testing.T) {
	const (
		oldToken  = "old-search-menu"
		sessionID = "si_v1_session-a"
	)
	callOrder := make([]string, 0, 6)
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		callOrder = append(callOrder, url)
		switch {
		case strings.Contains(url, "sendRichMessage"):
			return successResponseWithMessageID(t, 101), nil
		case strings.Contains(url, "editMessageReplyMarkup"):
			return successResponseWithMessageID(t, 91), nil
		case strings.Contains(url, "answerCallbackQuery"):
			return callbackSuccessResponse(t), nil
		case strings.Contains(url, "editMessageText"):
			return successResponseWithMessageID(t, 101), nil
		default:
			return nil, errors.New("unexpected API call " + url)
		}
	}}
	ch := newTestChannel(t, caller)
	messageBus := bus.NewMessageBus()
	ch.BaseChannel = channels.NewBaseChannel("telegram", nil, messageBus, nil)

	oldContent := testSearchContinuationContent("skill", "", sessionID)
	oldMenu := telegramSessionMenu{
		token: oldToken, menu: *oldContent.Interaction, chatID: 12345, messageID: 91, createdAt: time.Now(),
	}
	ch.storeSessionMenu(oldMenu)
	ch.storeSessionRenamePrompt(
		telegramSessionRenamePromptKey{chatID: 12345, messageID: 92},
		telegramSessionRenamePrompt{token: oldToken, menu: oldMenu, action: "search", createdAt: time.Now()},
	)

	requests := make([]bus.InternalCallbackRequest, 0, 2)
	ch.SetInternalCallbackHandler(func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
		requests = append(requests, req)
		content := testSearchContinuationContent("skill", strings.TrimSpace(req.Value), sessionID)
		if req.Action == "search" {
			return &bus.InternalCallbackResponse{
				Content: content, Transition: bus.InteractionAppendContinuation,
			}, nil
		}
		return &bus.InternalCallbackResponse{Content: content}, nil
	})

	reply := &telego.Message{
		MessageID: 100, From: &telego.User{ID: 42}, Text: "web",
		Chat: telego.Chat{ID: 12345}, ReplyToMessage: &telego.Message{MessageID: 92},
	}
	require.NoError(t, ch.handleMessage(context.Background(), reply))
	require.Len(t, requests, 1)
	assert.Equal(t, "search", requests[0].Action)
	assert.Equal(t, "web", requests[0].Value)
	assert.Equal(t, sessionID, requests[0].SessionKey)

	require.Len(t, callOrder, 2)
	assert.Contains(t, callOrder[0], "sendRichMessage", "result must be appended before the old card is retired")
	assert.Contains(t, callOrder[1], "editMessageReplyMarkup", "old keyboard should be disabled only after append succeeds")
	for _, call := range callOrder {
		assert.NotContains(t, call, "editMessageText", "prompt-driven search must not edit the old result card")
	}
	var sent struct {
		ReplyParameters *telego.ReplyParameters `json:"reply_parameters"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &sent))
	require.NotNil(t, sent.ReplyParameters)
	assert.Equal(t, 100, sent.ReplyParameters.MessageID, "continuation should be anchored below the user's reply")

	ch.sessionMenuMu.Lock()
	_, oldStillActive := ch.sessionMenus[oldToken]
	var newToken string
	var newMenu telegramSessionMenu
	for token, menu := range ch.sessionMenus {
		newToken, newMenu = token, menu
	}
	activeCount := len(ch.sessionMenus)
	ch.sessionMenuMu.Unlock()
	assert.False(t, oldStillActive)
	require.Equal(t, 1, activeCount)
	require.Len(t, newToken, 12)
	assert.Equal(t, 101, newMenu.messageID)
	assert.Equal(t, sessionID, newMenu.menu.SessionKey)
	assert.Equal(t, "web", newMenu.menu.Query)

	// The newly registered capability is actionable while the retired token is gone.
	require.NoError(t, ch.handleCallbackQuery(context.Background(), &telego.CallbackQuery{
		ID: "new-result-click", From: telego.User{ID: 42}, Data: sessionCallbackPrefix + newToken + ":e0",
		Message: &telego.Message{MessageID: 101, Chat: telego.Chat{ID: 12345}},
	}))
	require.Len(t, requests, 2)
	assert.Equal(t, "detail", requests[1].Action)
	assert.Equal(t, sessionID, requests[1].SessionKey)

	select {
	case inbound := <-messageBus.InboundChan():
		t.Fatalf("search reply leaked into normal agent pipeline: %+v", inbound)
	default:
	}
}

func TestAppendContinuationFailureKeepsOldMenuActive(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		switch {
		case strings.Contains(url, "sendRichMessage"):
			return nil, errors.New("rich send failed")
		case strings.Contains(url, "sendMessage"):
			return nil, errors.New("fallback send failed")
		default:
			return nil, errors.New("unexpected API call " + url)
		}
	}}
	ch := newTestChannel(t, caller)
	oldContent := testSearchContinuationContent("model", "", "si_v1_session-a")
	old := telegramSessionMenu{
		token: "old-menu", menu: *oldContent.Interaction, chatID: 12345, messageID: 91, createdAt: time.Now(),
	}
	ch.storeSessionMenu(old)

	err := ch.applyInteractionResponse(
		context.Background(),
		&telego.Message{MessageID: 100, Chat: telego.Chat{ID: 12345}},
		old.token,
		old,
		&bus.InternalCallbackResponse{
			Content: testSearchContinuationContent("model", "gpt", "si_v1_session-a"),
			Transition: bus.InteractionAppendContinuation,
		},
	)
	require.Error(t, err)
	ch.sessionMenuMu.Lock()
	_, oldStillActive := ch.sessionMenus[old.token]
	activeCount := len(ch.sessionMenus)
	ch.sessionMenuMu.Unlock()
	assert.True(t, oldStillActive, "failed continuation must not retire the prior capability")
	assert.Equal(t, 1, activeCount, "failed continuation must not register a competing menu")
	for _, call := range caller.calls {
		assert.NotContains(t, call.URL, "editMessageReplyMarkup")
	}
}

func TestPrivateAppendContinuationFailsClosedWithoutReceiverCapability(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		return nil, errors.New("unexpected network call " + url)
	}}
	ch := newTestChannel(t, caller)
	content := testSearchContinuationContent("skill", "", "si_v1_private")
	content.Interaction.ChatID = "-10055/7"
	content.Interaction.TopicID = "7"
	content.Interaction.Inbound = bus.InboundContext{
		Channel: "telegram", Account: "telegram", ChatID: "-10055/7", TopicID: "7", ChatType: "group",
		SenderID: "42", PrivateResponse: true, PrivateRouteToken: "missing-private-capability",
	}
	old := telegramSessionMenu{
		token: "old-private", menu: *content.Interaction, chatID: -10055, threadID: 7,
		ephemeralID: 77, receiverUserID: 42, createdAt: time.Now(),
	}
	ch.storeSessionMenu(old)
	result := testSearchContinuationContent("skill", "private query", "si_v1_private")
	result.Interaction.ChatID = content.Interaction.ChatID
	result.Interaction.TopicID = content.Interaction.TopicID
	result.Interaction.Inbound = content.Interaction.Inbound

	err := ch.applyInteractionResponse(
		context.Background(),
		&telego.Message{EphemeralMessageID: 80, MessageThreadID: 7, Chat: telego.Chat{ID: -10055}},
		old.token,
		old,
		&bus.InternalCallbackResponse{Content: result, Transition: bus.InteractionAppendContinuation},
	)
	require.Error(t, err)
	assert.Empty(t, caller.calls, "private continuation must not fall back to a public send")
	ch.sessionMenuMu.Lock()
	_, oldStillActive := ch.sessionMenus[old.token]
	ch.sessionMenuMu.Unlock()
	assert.True(t, oldStillActive)
}

func TestModelSearchButtonUsesForceReplyAndBoundSession(t *testing.T) {
	const sessionID = "si_v1_model-session-a"
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		switch {
		case strings.Contains(url, "answerCallbackQuery"):
			return callbackSuccessResponse(t), nil
		case strings.Contains(url, "sendMessage"):
			return successResponseWithMessageID(t, 92), nil
		case strings.Contains(url, "sendRichMessage"):
			return successResponseWithMessageID(t, 101), nil
		case strings.Contains(url, "editMessageReplyMarkup"):
			return successResponseWithMessageID(t, 91), nil
		default:
			return nil, errors.New("unexpected API call " + url)
		}
	}}
	ch := newTestChannel(t, caller)
	modelContent := testSearchContinuationContent("model", "", sessionID)
	modelContent.Title = "Model Dashboard"
	modelContent.Interaction.Entries = []bus.InteractionEntry{
		{Label: "🔎 Search", Action: "search"},
		{Label: "✖️ Close", Action: "close"},
	}
	markup, pending, err := ch.structuredReplyMarkup(modelContent, 12345, 0)
	require.NoError(t, err)
	require.NotNil(t, pending)
	pending.messageID = 91
	ch.storeSessionMenu(*pending)

	requests := make([]bus.InternalCallbackRequest, 0, 2)
	ch.SetInternalCallbackHandler(func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
		requests = append(requests, req)
		if strings.TrimSpace(req.Value) == "" {
			return &bus.InternalCallbackResponse{Text: "Reply to this prompt with a model name to search:"}, nil
		}
		return &bus.InternalCallbackResponse{
			Content: testSearchContinuationContent("model", req.Value, sessionID),
			Transition: bus.InteractionAppendContinuation,
		}, nil
	})

	search := findInlineButton(t, markup, "🔎 Search")
	require.NoError(t, ch.handleCallbackQuery(context.Background(), &telego.CallbackQuery{
		ID: "model-search", From: telego.User{ID: 42}, Data: search.CallbackData,
		Message: &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}},
	}))
	require.Len(t, requests, 1)
	assert.Equal(t, "model", requests[0].Kind)
	assert.Equal(t, "search", requests[0].Action)
	assert.Equal(t, sessionID, requests[0].SessionKey)
	require.Len(t, caller.calls, 2)
	assert.Contains(t, caller.calls[0].URL, "answerCallbackQuery")
	assert.Contains(t, caller.calls[1].URL, "sendMessage")
	var prompt struct {
		Text        string            `json:"text"`
		ReplyMarkup telego.ForceReply `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[1].Data.BodyRaw, &prompt))
	assert.True(t, prompt.ReplyMarkup.ForceReply)
	assert.Contains(t, prompt.Text, "model name")

	reply := &telego.Message{
		MessageID: 100, From: &telego.User{ID: 42}, Text: "gpt",
		Chat: telego.Chat{ID: 12345}, ReplyToMessage: &telego.Message{MessageID: 92},
	}
	require.NoError(t, ch.handleMessage(context.Background(), reply))
	require.Len(t, requests, 2)
	assert.Equal(t, "model", requests[1].Kind)
	assert.Equal(t, "search", requests[1].Action)
	assert.Equal(t, "gpt", requests[1].Value)
	assert.Equal(t, sessionID, requests[1].SessionKey)
	assert.False(t, strings.HasPrefix(requests[1].Value, "/model search"), "prompt path must carry semantic input, not slash text")
	require.Len(t, caller.calls, 4)
	assert.Contains(t, caller.calls[2].URL, "sendRichMessage")
	assert.Contains(t, caller.calls[3].URL, "editMessageReplyMarkup")
}

func TestModelCallbackPayloadIsOpaqueAndBounded(t *testing.T) {
	const (
		query     = "confidential model query"
		sessionID = "si_v1_secret-model-session"
		rawModel  = "provider/private-model-id"
	)
	ch := newTestChannel(t, &stubCaller{})
	content := testSearchContinuationContent("model", query, sessionID)
	content.Interaction.Entries = []bus.InteractionEntry{
		{Label: "1", Action: "select", Value: rawModel},
		{Label: "🔎 Search", Action: "search"},
		{Label: "✖️ Close", Action: "close"},
	}
	markup, pending, err := ch.structuredReplyMarkup(content, 12345, 0)
	require.NoError(t, err)
	require.NotNil(t, pending)
	assert.Equal(t, query, pending.menu.Query)
	assert.Equal(t, sessionID, pending.menu.SessionKey)
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			assert.LessOrEqual(t, len([]byte(button.CallbackData)), 64)
			assert.NotContains(t, button.CallbackData, query)
			assert.NotContains(t, button.CallbackData, sessionID)
			assert.NotContains(t, button.CallbackData, rawModel)
		}
	}
}

func TestModelSearchPromptRejectsCrossScopeExpiredAndReplay(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})
	menu := telegramSessionMenu{
		chatID: 12345, threadID: 7, messageID: 91,
		menu: bus.InteractionMenu{
			Kind: "model", OwnerID: "42", Channel: "telegram", Account: "telegram", ChatID: "12345/7",
			TopicID: "7", AgentID: "main", Scope: "scope-a", SessionKey: "si_v1_bound",
			Inbound: bus.InboundContext{
				Channel: "telegram", Account: "telegram", ChatID: "12345/7", TopicID: "7", ChatType: "group", SenderID: "42",
			},
		},
	}
	key := telegramSessionRenamePromptKey{chatID: 12345, threadID: 7, messageID: 92}
	ch.storeSessionRenamePrompt(key, telegramSessionRenamePrompt{
		token: "model-search", menu: menu, action: "search", createdAt: time.Now(),
	})

	base := telego.Message{
		MessageID: 100, From: &telego.User{ID: 42}, Text: "gpt", Chat: telego.Chat{ID: 12345}, MessageThreadID: 7,
		ReplyToMessage: &telego.Message{MessageID: 92},
	}
	wrongUser := base
	wrongUser.From = &telego.User{ID: 99}
	_, status := ch.claimSessionRenamePrompt(&wrongUser)
	assert.Equal(t, sessionRenameClaimRejected, status)
	wrongChat := base
	wrongChat.Chat.ID = 54321
	_, status = ch.claimSessionRenamePrompt(&wrongChat)
	assert.Equal(t, sessionRenameClaimNone, status)
	wrongTopic := base
	wrongTopic.MessageThreadID = 8
	_, status = ch.claimSessionRenamePrompt(&wrongTopic)
	assert.Equal(t, sessionRenameClaimNone, status)
	wrongPrompt := base
	wrongPrompt.ReplyToMessage = &telego.Message{MessageID: 93}
	_, status = ch.claimSessionRenamePrompt(&wrongPrompt)
	assert.Equal(t, sessionRenameClaimNone, status)
	empty := base
	empty.Text = "  "
	_, status = ch.claimSessionRenamePrompt(&empty)
	assert.Equal(t, sessionRenameClaimInvalid, status)
	_, status = ch.claimSessionRenamePrompt(&base)
	assert.Equal(t, sessionRenameClaimed, status)
	_, status = ch.claimSessionRenamePrompt(&base)
	assert.Equal(t, sessionRenameClaimReplay, status)

	expiredKey := telegramSessionRenamePromptKey{chatID: 12345, threadID: 7, messageID: 94}
	ch.storeSessionRenamePrompt(expiredKey, telegramSessionRenamePrompt{
		token: "expired-model-search", menu: menu, action: "search",
		createdAt: time.Now().Add(-sessionRenameTTL - time.Second),
	})
	expired := base
	expired.ReplyToMessage = &telego.Message{MessageID: 94}
	_, status = ch.claimSessionRenamePrompt(&expired)
	assert.Equal(t, sessionRenameClaimExpired, status)
}
