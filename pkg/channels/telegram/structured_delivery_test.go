package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/channels"
)

func testSessionStructuredContent() *bus.StructuredContent {
	inbound := bus.InboundContext{Channel: "telegram", ChatID: "12345", ChatType: "direct", SenderID: "42"}
	return &bus.StructuredContent{
		Kind:  "session_list",
		Title: "Session",
		Tables: []bus.StructuredTable{{
			Columns: []string{"No", "Nama Session", "Pesan", "Terakhir"},
			Rows:    [][]string{{"✅1", "<b>Main</b> * safe | data", "2", "15:00"}, {"2", "Other", "1", "Kemarin"}},
			Border:  true, Striped: true, Header: true,
		}},
		Fallback: "| No | Nama Session | Pesan | Terakhir |\n|---|---|---|---|\n| ✅1 | Main | 2 | 15:00 |",
		Interaction: &bus.InteractionMenu{
			Kind:    "session",
			OwnerID: "42",
			Channel: "telegram",
			ChatID:  "12345",
			AgentID: "main",
			Scope:   "scope-signature",
			Inbound: inbound,
			Page:    0,
			Pages:   1,
			Current: "si_v1_secret-session-key-that-must-not-leak",
			Entries: []bus.InteractionEntry{
				{Label: "1", Action: "select", Value: "si_v1_secret-session-key-that-must-not-leak"},
				{Label: "2", Action: "select", Value: "si_v1_other-secret"},
				{Label: "Halaman 1/1", Action: "noop"},
				{Label: "➕ New", Action: "new"},
				{Label: "🗑️ Remove", Action: "remove"},
				{Label: "✏️ Rename", Action: "rename"},
				{Label: "✖️ Tutup", Action: "close"},
			},
		},
	}
}

func callbackSuccessResponse(t *testing.T) *ta.Response {
	t.Helper()
	return &ta.Response{Ok: true, Result: []byte("true")}
}

func TestStructuredSessionSendUsesNativeTableAndKeyboardTogether(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		require.Contains(t, url, "sendRichMessage")
		return successResponseWithMessageID(t, 91), nil
	}}
	ch := newTestChannel(t, caller)
	ids, err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID: "12345", Context: bus.InboundContext{Channel: "telegram", ChatID: "12345", SenderID: "42"},
		Content: "fallback", Structured: testSessionStructuredContent(),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"91"}, ids)
	require.Len(t, caller.calls, 1)
	var payload struct {
		RichMessage struct {
			Blocks []json.RawMessage `json:"blocks"`
		} `json:"rich_message"`
		ReplyMarkup struct {
			InlineKeyboard [][]telego.InlineKeyboardButton `json:"inline_keyboard"`
		} `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &payload))
	assert.NotContains(t, string(caller.calls[0].Data.BodyRaw), `"markdown"`)
	assert.NotContains(t, string(caller.calls[0].Data.BodyRaw), `"html"`)
	require.GreaterOrEqual(t, len(payload.RichMessage.Blocks), 2)
	var table struct {
		Type       string `json:"type"`
		IsBordered bool   `json:"is_bordered"`
		IsStriped  bool   `json:"is_striped"`
		Cells      [][]struct {
			IsHeader bool `json:"is_header"`
		} `json:"cells"`
	}
	for _, raw := range payload.RichMessage.Blocks {
		var candidate struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &candidate)
		if candidate.Type == telego.BlockTypeTable {
			require.NoError(t, json.Unmarshal(raw, &table))
		}
	}
	assert.Equal(t, telego.BlockTypeTable, table.Type)
	assert.True(t, table.IsBordered)
	assert.True(t, table.IsStriped)
	assert.True(t, table.Cells[0][0].IsHeader)
	require.NotEmpty(t, payload.ReplyMarkup.InlineKeyboard)
	for _, row := range payload.ReplyMarkup.InlineKeyboard {
		for _, button := range row {
			assert.LessOrEqual(t, len([]byte(button.CallbackData)), 64)
			assert.NotContains(t, button.CallbackData, "secret-session-key")
		}
	}
}

func TestStructuredSessionFallbackKeepsKeyboard(t *testing.T) {
	call := 0
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		call++
		if strings.Contains(url, "sendRichMessage") {
			return nil, errors.New("Bad Request: rich messages unsupported")
		}
		return successResponseWithMessageID(t, 92), nil
	}}
	ch := newTestChannel(t, caller)
	_, err := ch.Send(
		context.Background(),
		bus.OutboundMessage{ChatID: "12345", Content: "fallback", Structured: testSessionStructuredContent()},
	)
	require.NoError(t, err)
	require.Len(t, caller.calls, 2)
	assert.Contains(t, caller.calls[0].URL, "sendRichMessage")
	assert.Contains(t, caller.calls[1].URL, "sendMessage")
	var payload struct {
		ReplyMarkup *telego.InlineKeyboardMarkup `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[1].Data.BodyRaw, &payload))
	require.NotNil(t, payload.ReplyMarkup)
}

func TestStructuredInformationalResponseUsesNativeRichTable(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		require.Contains(t, url, "sendRichMessage")
		return successResponseWithMessageID(t, 93), nil
	}}
	ch := newTestChannel(t, caller)
	content := &bus.StructuredContent{Kind: "table", Title: "Context usage", Tables: []bus.StructuredTable{{
		Columns: []string{"Metrik", "Nilai"}, Rows: [][]string{{"Messages", "12"}, {"Used", "1024"}},
		Border: true, Striped: true, Header: true,
	}}, Fallback: "Messages: 12\nUsed: 1024"}
	_, err := ch.Send(
		context.Background(),
		bus.OutboundMessage{ChatID: "12345", Content: content.Fallback, Structured: content},
	)
	require.NoError(t, err)
	require.Len(t, caller.calls, 1)
	var payload struct {
		RichMessage struct {
			Blocks []struct {
				Type string `json:"type"`
			} `json:"blocks"`
		} `json:"rich_message"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &payload))
	foundTable := false
	for _, block := range payload.RichMessage.Blocks {
		foundTable = foundTable || block.Type == telego.BlockTypeTable
	}
	assert.True(t, foundTable)
}

func TestSessionCallbackAnswersBeforeEditingAndNeverPublishesInbound(t *testing.T) {
	callOrder := make([]string, 0)
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		callOrder = append(callOrder, url)
		switch {
		case strings.Contains(url, "sendRichMessage"):
			return successResponseWithMessageID(t, 91), nil
		case strings.Contains(url, "answerCallbackQuery"):
			return callbackSuccessResponse(t), nil
		case strings.Contains(url, "editMessageText"):
			return successResponseWithMessageID(t, 91), nil
		default:
			return nil, errors.New("unexpected API call " + url)
		}
	}}
	ch := newTestChannel(t, caller)
	var got bus.InternalCallbackRequest
	ch.SetInternalCallbackHandler(
		func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
			got = req
			updated := testSessionStructuredContent()
			updated.Interaction.Page = 0
			return &bus.InternalCallbackResponse{Content: updated}, nil
		},
	)
	_, err := ch.Send(
		context.Background(),
		bus.OutboundMessage{ChatID: "12345", Content: "fallback", Structured: testSessionStructuredContent()},
	)
	require.NoError(t, err)
	require.Len(t, caller.calls, 1)
	var sent struct {
		ReplyMarkup telego.InlineKeyboardMarkup `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &sent))
	button := sent.ReplyMarkup.InlineKeyboard[0][0]
	query := &telego.CallbackQuery{
		ID:      "callback-1",
		From:    telego.User{ID: 42},
		Data:    button.CallbackData,
		Message: &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}},
	}
	require.NoError(t, ch.handleCallbackQuery(context.Background(), query))
	require.Len(t, callOrder, 3)
	assert.Contains(t, callOrder[1], "answerCallbackQuery")
	assert.Contains(t, callOrder[2], "editMessageText")
	assert.Equal(t, "select", got.Action)
	assert.Equal(t, "42", got.OwnerID)
	assert.NotEmpty(t, got.Value)
}

func TestSessionCallbackRejectsOtherUserAndExpiredMenu(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		if strings.Contains(url, "sendRichMessage") {
			return successResponseWithMessageID(t, 91), nil
		}
		return callbackSuccessResponse(t), nil
	}}
	ch := newTestChannel(t, caller)
	called := false
	ch.SetInternalCallbackHandler(
		func(context.Context, bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
			called = true
			return nil, nil
		},
	)
	_, err := ch.Send(
		context.Background(),
		bus.OutboundMessage{ChatID: "12345", Content: "fallback", Structured: testSessionStructuredContent()},
	)
	require.NoError(t, err)
	var sent struct {
		ReplyMarkup telego.InlineKeyboardMarkup `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &sent))
	data := sent.ReplyMarkup.InlineKeyboard[0][0].CallbackData
	other := &telego.CallbackQuery{
		ID:      "other",
		From:    telego.User{ID: 99},
		Data:    data,
		Message: &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}},
	}
	require.NoError(t, ch.handleCallbackQuery(context.Background(), other))
	assert.False(t, called)

	// Expire the server-side capability and verify a fresh alert is sent.
	parts := strings.Split(data, ":")
	require.Len(t, parts, 3)
	token := parts[1]
	ch.sessionMenuMu.Lock()
	menu := ch.sessionMenus[token]
	menu.createdAt = time.Now().Add(-sessionMenuTTL - time.Second)
	ch.sessionMenus[token] = menu
	ch.sessionMenuMu.Unlock()
	expired := &telego.CallbackQuery{
		ID:      "expired",
		From:    telego.User{ID: 42},
		Data:    data,
		Message: &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}},
	}
	require.NoError(t, ch.handleCallbackQuery(context.Background(), expired))
	assert.True(t, len(caller.calls) >= 3)
}

func TestSessionCallbackDataParserAndActions(t *testing.T) {
	menu := *testSessionStructuredContent().Interaction
	for _, code := range []string{"s0", "s1", "n", "d", "r", "x", "o", "p0"} {
		action, _, ok := resolveSessionMenuAction(menu, code)
		if code == "p0" {
			assert.False(t, ok)
			continue
		}
		assert.True(t, ok, code)
		assert.NotEmpty(t, action)
	}
	_, _, ok := parseInternalSessionCallback("pcsm:bad:malformed")
	assert.False(t, ok)
	assert.LessOrEqual(t, len([]byte(sessionCallbackPrefix+strings.Repeat("a", 12)+":s0")), 64)
	assert.Equal(t, "91", strconv.Itoa(91))
}

func findInlineButton(t *testing.T, markup *telego.InlineKeyboardMarkup, label string) telego.InlineKeyboardButton {
	t.Helper()
	require.NotNil(t, markup)
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			if button.Text == label {
				return button
			}
		}
	}
	t.Fatalf("button %q not found", label)
	return telego.InlineKeyboardButton{}
}

func TestSessionKeyboardLayoutPaginationAndStyles(t *testing.T) {
	content := testSessionStructuredContent()
	content.Interaction.Page = 1
	content.Interaction.Pages = 3
	content.Interaction.Entries = nil
	for i := 0; i < 5; i++ {
		content.Interaction.Entries = append(content.Interaction.Entries, bus.InteractionEntry{
			Label: strconv.Itoa(i + 6), Action: "select", Value: "si_v1_" + strconv.Itoa(i),
		})
	}
	content.Interaction.Current = "si_v1_0"
	content.Interaction.Entries = append(content.Interaction.Entries,
		bus.InteractionEntry{Label: "◀️", Action: "page", Value: "0"},
		bus.InteractionEntry{Label: "Halaman 2/3", Action: "noop"},
		bus.InteractionEntry{Label: "▶️", Action: "page", Value: "2"},
		bus.InteractionEntry{Label: "➕ New", Action: "new"},
		bus.InteractionEntry{Label: "🗑️ Remove", Action: "remove"},
		bus.InteractionEntry{Label: "✏️ Rename", Action: "rename"},
		bus.InteractionEntry{Label: "✖️ Tutup", Action: "close"},
	)
	ch := newTestChannel(t, &stubCaller{callFn: func(context.Context, string, *ta.RequestData) (*ta.Response, error) {
		return callbackSuccessResponse(t), nil
	}})
	markup, _, err := ch.structuredReplyMarkup(content, 12345, 0)
	require.NoError(t, err)
	require.Len(t, markup.InlineKeyboard, 4)
	assert.Len(t, markup.InlineKeyboard[0], 5)
	assert.Len(t, markup.InlineKeyboard[1], 3, "navigation must use exactly three buttons")
	assert.Len(t, markup.InlineKeyboard[2], 3, "session actions must share one New | Remove | Rename row")
	assert.Len(t, markup.InlineKeyboard[3], 1)
	assert.Equal(t, telego.ButtonStyleSuccess, findInlineButton(t, markup, "6").Style)
	assert.Equal(t, telego.ButtonStylePrimary, findInlineButton(t, markup, "➕ New").Style)
	assert.Equal(t, telego.ButtonStyleDanger, findInlineButton(t, markup, "🗑️ Remove").Style)
	assert.Equal(t, telego.ButtonStyleDanger, findInlineButton(t, markup, "✖️ Tutup").Style)
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			assert.LessOrEqual(t, len([]byte(button.CallbackData)), 64)
		}
	}
	_, code, parsed := parseInternalSessionCallback(findInlineButton(t, markup, "◀️").CallbackData)
	require.True(t, parsed)
	action, value, ok := resolveSessionMenuAction(*content.Interaction, code)
	assert.True(t, ok)
	assert.Equal(t, "page", action)
	assert.Equal(t, "0", value)
}

func TestSessionCallbackNewRenameClosePaginationAndMalformed(t *testing.T) {
	tests := []struct {
		name       string
		label      string
		expect     string
		close      bool
		withPages  bool
		expectEdit string
	}{
		{name: "new", label: "➕ New", expect: "new", expectEdit: "editMessageText"},
		{name: "remove", label: "🗑️ Remove", expect: "remove", expectEdit: "editMessageText"},
		{name: "rename", label: "✏️ Rename", expect: "rename", expectEdit: "sendMessage"},
		{name: "close", label: "✖️ Tutup", expect: "close", close: true, expectEdit: "editMessageReplyMarkup"},
		{name: "pagination", label: "▶️", expect: "page", withPages: true, expectEdit: "editMessageText"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
				switch {
				case strings.Contains(url, "answerCallbackQuery"):
					return callbackSuccessResponse(t), nil
				case strings.Contains(url, "sendMessage"):
					return successResponseWithMessageID(t, 92), nil
				case strings.Contains(url, "editMessageText"), strings.Contains(url, "editMessageReplyMarkup"):
					return successResponseWithMessageID(t, 91), nil
				default:
					return nil, errors.New("unexpected API call " + url)
				}
			}}
			ch := newTestChannel(t, caller)
			content := testSessionStructuredContent()
			if tt.withPages {
				content.Interaction.Pages = 2
				content.Interaction.Entries = append(
					content.Interaction.Entries,
					bus.InteractionEntry{Label: "▶️", Action: "page", Value: "1"},
				)
			}
			markup, pending, err := ch.structuredReplyMarkup(content, 12345, 0)
			require.NoError(t, err)
			require.NotNil(t, pending)
			pending.messageID = 91
			ch.storeSessionMenu(*pending)
			var got bus.InternalCallbackRequest
			ch.SetInternalCallbackHandler(
				func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
					got = req
					if tt.close {
						return &bus.InternalCallbackResponse{Close: true}, nil
					}
					if tt.expect == "rename" {
						return &bus.InternalCallbackResponse{Text: "rename help"}, nil
					}
					return &bus.InternalCallbackResponse{Content: testSessionStructuredContent()}, nil
				},
			)
			button := findInlineButton(t, markup, tt.label)
			query := &telego.CallbackQuery{
				ID:      "callback-" + tt.name,
				From:    telego.User{ID: 42},
				Data:    button.CallbackData,
				Message: &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}},
			}
			require.NoError(t, ch.handleCallbackQuery(context.Background(), query))
			assert.Equal(t, tt.expect, got.Action)
			require.NotEmpty(t, caller.calls)
			assert.Contains(t, caller.calls[0].URL, "answerCallbackQuery")
			if tt.expectEdit != "" {
				require.Len(t, caller.calls, 2)
				assert.Contains(t, caller.calls[1].URL, tt.expectEdit)
			}
		})
	}

	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		require.Contains(t, url, "answerCallbackQuery")
		return callbackSuccessResponse(t), nil
	}}
	ch := newTestChannel(t, caller)
	require.NoError(t, ch.handleCallbackQuery(context.Background(), &telego.CallbackQuery{
		ID: "malformed", From: telego.User{ID: 42}, Data: "pcsm:not-valid:???",
	}))
	require.Len(t, caller.calls, 1)
}

func TestSessionRenameButtonUsesScopedForceReplyAndBypassesInbound(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		switch {
		case strings.Contains(url, "answerCallbackQuery"):
			return callbackSuccessResponse(t), nil
		case strings.Contains(url, "sendMessage"):
			return successResponseWithMessageID(t, 92), nil
		case strings.Contains(url, "editMessageText"):
			return successResponseWithMessageID(t, 91), nil
		default:
			return nil, errors.New("unexpected API call " + url)
		}
	}}
	ch := newTestChannel(t, caller)
	messageBus := bus.NewMessageBus()
	ch.BaseChannel = channels.NewBaseChannel("telegram", nil, messageBus, nil)
	content := testSessionStructuredContent()
	markup, menu, err := ch.structuredReplyMarkup(content, 12345, 0)
	require.NoError(t, err)
	menu.messageID = 91
	ch.storeSessionMenu(*menu)

	requests := make([]bus.InternalCallbackRequest, 0, 2)
	ch.SetInternalCallbackHandler(
		func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
			requests = append(requests, req)
			if strings.TrimSpace(req.Value) == "" {
				return &bus.InternalCallbackResponse{
					Text: "Balas pesan ini dengan nama baru untuk session aktif.",
				}, nil
			}
			updated := testSessionStructuredContent()
			updated.Tables[0].Rows[0][1] = req.Value
			return &bus.InternalCallbackResponse{Content: updated}, nil
		},
	)

	rename := findInlineButton(t, markup, "✏️ Rename")
	require.NoError(t, ch.handleCallbackQuery(context.Background(), &telego.CallbackQuery{
		ID: "rename-click", From: telego.User{ID: 42}, Data: rename.CallbackData,
		Message: &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}},
	}))
	require.Len(t, caller.calls, 2)
	assert.Contains(t, caller.calls[0].URL, "answerCallbackQuery")
	assert.Contains(t, caller.calls[1].URL, "sendMessage")
	var promptPayload struct {
		ReplyMarkup telego.ForceReply `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[1].Data.BodyRaw, &promptPayload))
	assert.True(t, promptPayload.ReplyMarkup.ForceReply)
	assert.Equal(t, "Nama session baru", promptPayload.ReplyMarkup.InputFieldPlaceholder)

	reply := &telego.Message{
		MessageID: 100, From: &telego.User{ID: 42}, Text: "Nama Baru",
		Chat:           telego.Chat{ID: 12345},
		ReplyToMessage: &telego.Message{MessageID: 92},
	}
	require.NoError(t, ch.handleMessage(context.Background(), reply))
	require.Len(t, requests, 2)
	assert.Equal(t, "rename", requests[1].Action)
	assert.Equal(t, "Nama Baru", requests[1].Value)
	assert.Equal(t, content.Interaction.Current, requests[1].SessionKey)
	require.Len(t, caller.calls, 3)
	assert.Contains(t, caller.calls[2].URL, "editMessageText")
	select {
	case inbound := <-messageBus.InboundChan():
		t.Fatalf("rename reply leaked into inbound bus: %+v", inbound)
	default:
	}
}

func TestSessionRenamePromptRejectsCrossScopeExpiredAndReplay(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		require.Contains(t, url, "sendMessage")
		return successResponseWithMessageID(t, 101), nil
	}}
	ch := newTestChannel(t, caller)
	menu := telegramSessionMenu{
		chatID: 12345, threadID: 7, messageID: 91, receiverUserID: 42,
		menu: *testSessionStructuredContent().Interaction,
	}
	menu.menu.TopicID = "7"
	menu.menu.ChatID = "12345/7"
	key := telegramSessionRenamePromptKey{chatID: 12345, threadID: 7, messageID: 92}
	ch.storeSessionRenamePrompt(key, telegramSessionRenamePrompt{
		token: "menu-token", menu: menu, createdAt: time.Now(),
	})

	wrongUser := &telego.Message{
		MessageID: 100, From: &telego.User{ID: 99}, Text: "Wrong user",
		Chat: telego.Chat{ID: 12345}, MessageThreadID: 7,
		ReplyToMessage: &telego.Message{MessageID: 92},
	}
	_, status := ch.claimSessionRenamePrompt(wrongUser)
	assert.Equal(t, sessionRenameClaimRejected, status)
	wrongChat := *wrongUser
	wrongChat.From = &telego.User{ID: 42}
	wrongChat.Chat.ID = 54321
	_, status = ch.claimSessionRenamePrompt(&wrongChat)
	assert.Equal(t, sessionRenameClaimNone, status)
	wrongTopic := *wrongUser
	wrongTopic.From = &telego.User{ID: 42}
	wrongTopic.MessageThreadID = 8
	_, status = ch.claimSessionRenamePrompt(&wrongTopic)
	assert.Equal(t, sessionRenameClaimNone, status)
	wrongPrompt := *wrongUser
	wrongPrompt.From = &telego.User{ID: 42}
	wrongPrompt.ReplyToMessage = &telego.Message{MessageID: 93}
	_, status = ch.claimSessionRenamePrompt(&wrongPrompt)
	assert.Equal(t, sessionRenameClaimNone, status)

	valid := *wrongUser
	valid.From = &telego.User{ID: 42}
	valid.Text = "Valid name"
	_, status = ch.claimSessionRenamePrompt(&valid)
	assert.Equal(t, sessionRenameClaimed, status)
	_, status = ch.claimSessionRenamePrompt(&valid)
	assert.Equal(t, sessionRenameClaimReplay, status)

	expiredKey := telegramSessionRenamePromptKey{chatID: 12345, threadID: 7, messageID: 94}
	ch.storeSessionRenamePrompt(expiredKey, telegramSessionRenamePrompt{
		token: "expired-token", menu: menu, createdAt: time.Now().Add(-sessionRenameTTL - time.Second),
	})
	expiredReply := &telego.Message{
		MessageID: 102, From: &telego.User{ID: 42}, Text: "Too late",
		Chat: telego.Chat{ID: 12345}, MessageThreadID: 7,
		ReplyToMessage: &telego.Message{MessageID: 94},
	}
	handled, err := ch.handlePendingSessionRenameReply(context.Background(), expiredReply)
	require.NoError(t, err)
	assert.True(t, handled)
	require.Len(t, caller.calls, 1)
}

func TestSessionRenamePromptIsConsumedExactlyOnceConcurrently(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{callFn: func(context.Context, string, *ta.RequestData) (*ta.Response, error) {
		return callbackSuccessResponse(t), nil
	}})
	menu := telegramSessionMenu{
		chatID: 12345, messageID: 91,
		menu: *testSessionStructuredContent().Interaction,
	}
	key := telegramSessionRenamePromptKey{chatID: 12345, messageID: 92}
	ch.storeSessionRenamePrompt(key, telegramSessionRenamePrompt{
		token: "one-shot", menu: menu, createdAt: time.Now(),
	})
	message := &telego.Message{
		MessageID: 100, From: &telego.User{ID: 42}, Text: "Exactly once",
		Chat: telego.Chat{ID: 12345}, ReplyToMessage: &telego.Message{MessageID: 92},
	}

	const attempts = 24
	statuses := make(chan sessionRenameClaimStatus, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, status := ch.claimSessionRenamePrompt(message)
			statuses <- status
		}()
	}
	wg.Wait()
	close(statuses)
	claimed := 0
	for status := range statuses {
		if status == sessionRenameClaimed {
			claimed++
		} else {
			assert.Equal(t, sessionRenameClaimReplay, status)
		}
	}
	assert.Equal(t, 1, claimed)
}

func TestSessionCallbackEnvelopeSupportsPrivateGroupSupergroupAndForum(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{callFn: func(context.Context, string, *ta.RequestData) (*ta.Response, error) {
		return callbackSuccessResponse(t), nil
	}})
	for _, tt := range []struct {
		name     string
		chatID   int64
		chatType string
		threadID int
	}{
		{name: "private", chatID: 42, chatType: telego.ChatTypePrivate},
		{name: "group", chatID: -1001, chatType: telego.ChatTypeGroup},
		{name: "supergroup", chatID: -1002, chatType: telego.ChatTypeSupergroup},
		{name: "forum", chatID: -1003, chatType: telego.ChatTypeSupergroup, threadID: 7},
	} {
		t.Run(tt.name, func(t *testing.T) {
			menu := telegramSessionMenu{
				chatID:    tt.chatID,
				threadID:  tt.threadID,
				messageID: 91,
				menu:      bus.InteractionMenu{OwnerID: "42", Channel: "telegram"},
			}
			message := &telego.Message{
				MessageID:       91,
				MessageThreadID: tt.threadID,
				Chat:            telego.Chat{ID: tt.chatID, Type: tt.chatType, IsForum: tt.threadID != 0},
			}
			query := &telego.CallbackQuery{ID: "q", From: telego.User{ID: 42}, Message: message}
			assert.True(t, ch.sessionCallbackEnvelopeValid(query, message, menu))
			message.MessageThreadID++
			assert.False(t, ch.sessionCallbackEnvelopeValid(query, message, menu))
		})
	}
}

func TestEphemeralSessionMenuUsesPrivateFallbackAndInternalCallback(t *testing.T) {
	const (
		chatID      = int64(-10055)
		threadID    = 7
		ownerID     = int64(42)
		ephemeralID = 77
	)
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		switch {
		case strings.Contains(url, "sendMessage"):
			return successEphemeralResponse(t, chatID, threadID, ownerID, ephemeralID), nil
		case strings.Contains(url, "answerCallbackQuery"), strings.Contains(url, "editEphemeralMessageText"):
			return callbackSuccessResponse(t), nil
		default:
			return nil, errors.New("unexpected API call " + url)
		}
	}}
	ch := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, ch, chatID, threadID, ownerID, 0, "origin-callback")
	content := testSessionStructuredContent()
	content.Interaction.ChatID = strconvFormatChat(chatID, threadID)
	content.Interaction.TopicID = strconv.Itoa(threadID)
	content.Interaction.OwnerID = strconv.FormatInt(ownerID, 10)
	content.Interaction.Inbound = privateOutboundContext(target)
	content.Interaction.Inbound.PrivateSession = true
	content.Interaction.Scope = "ephemeral-scope"

	ids, err := ch.sendStructuredContent(
		context.Background(),
		bus.OutboundMessage{Content: content.FallbackText(), Structured: content},
		chatID,
		threadID,
		&target,
	)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	require.Len(t, caller.calls, 1)
	assert.Contains(t, caller.calls[0].URL, "sendMessage")
	assert.NotContains(t, caller.calls[0].URL, "sendRichMessage")
	var sent struct {
		ReplyMarkup telego.InlineKeyboardMarkup `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &sent))

	ch.SetInternalCallbackHandler(
		func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
			assert.Equal(t, "select", req.Action)
			return &bus.InternalCallbackResponse{Content: content}, nil
		},
	)
	button := sent.ReplyMarkup.InlineKeyboard[0][0]
	query := &telego.CallbackQuery{
		ID:   "ephemeral-callback",
		From: telego.User{ID: ownerID},
		Data: button.CallbackData,
		Message: &telego.Message{
			MessageID: 0, EphemeralMessageID: ephemeralID, MessageThreadID: threadID,
			Chat: telego.Chat{ID: chatID, Type: telego.ChatTypeSupergroup, IsForum: true},
		},
	}
	require.NoError(t, ch.handleCallbackQuery(context.Background(), query))
	require.Len(t, caller.calls, 3)
	assert.Contains(t, caller.calls[1].URL, "answerCallbackQuery")
	assert.Contains(t, caller.calls[2].URL, "editEphemeralMessageText")
}

func TestEphemeralSessionRenameUsesPrivateForceReplyAndRefresh(t *testing.T) {
	const (
		chatID             = int64(-10055)
		threadID           = 7
		ownerID            = int64(42)
		dashboardMessageID = 77
		promptMessageID    = 78
	)
	sendMessageCalls := 0
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		switch {
		case strings.Contains(url, "sendMessage"):
			sendMessageCalls++
			if sendMessageCalls == 1 {
				return successEphemeralResponse(t, chatID, threadID, ownerID, dashboardMessageID), nil
			}
			return successEphemeralResponse(t, chatID, threadID, ownerID, promptMessageID), nil
		case strings.Contains(url, "answerCallbackQuery"), strings.Contains(url, "editEphemeralMessageText"):
			return callbackSuccessResponse(t), nil
		default:
			return nil, errors.New("unexpected API call " + url)
		}
	}}
	ch := newTestChannel(t, caller)
	messageBus := bus.NewMessageBus()
	ch.BaseChannel = channels.NewBaseChannel("telegram", nil, messageBus, nil)
	target := mustRegisterEphemeralTarget(t, ch, chatID, threadID, ownerID, 0, "origin-callback")
	content := testSessionStructuredContent()
	content.Interaction.ChatID = strconvFormatChat(chatID, threadID)
	content.Interaction.TopicID = strconv.Itoa(threadID)
	content.Interaction.OwnerID = strconv.FormatInt(ownerID, 10)
	content.Interaction.Inbound = privateOutboundContext(target)
	content.Interaction.Inbound.PrivateSession = true
	content.Interaction.Scope = "ephemeral-rename-scope"

	_, err := ch.sendStructuredContent(
		context.Background(),
		bus.OutboundMessage{Content: content.FallbackText(), Structured: content},
		chatID,
		threadID,
		&target,
	)
	require.NoError(t, err)
	require.Len(t, caller.calls, 1)
	var sent struct {
		ReplyMarkup telego.InlineKeyboardMarkup `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &sent))

	requests := make([]bus.InternalCallbackRequest, 0, 2)
	ch.SetInternalCallbackHandler(
		func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
			requests = append(requests, req)
			if strings.TrimSpace(req.Value) == "" {
				return &bus.InternalCallbackResponse{Text: "Balas prompt untuk rename."}, nil
			}
			updated := testSessionStructuredContent()
			updated.Interaction = content.Interaction
			updated.Tables[0].Rows[0][1] = req.Value
			return &bus.InternalCallbackResponse{Content: updated}, nil
		},
	)
	rename := findInlineButton(t, &sent.ReplyMarkup, "✏️ Rename")
	require.NoError(t, ch.handleCallbackQuery(context.Background(), &telego.CallbackQuery{
		ID: "ephemeral-rename-click", From: telego.User{ID: ownerID}, Data: rename.CallbackData,
		Message: &telego.Message{
			EphemeralMessageID: dashboardMessageID, MessageThreadID: threadID,
			Chat: telego.Chat{ID: chatID, Type: telego.ChatTypeSupergroup, IsForum: true},
		},
	}))
	require.Len(t, caller.calls, 3)
	assert.Contains(t, caller.calls[1].URL, "answerCallbackQuery")
	assert.Contains(t, caller.calls[2].URL, "sendMessage")
	var prompt struct {
		ReceiverUserID int64             `json:"receiver_user_id"`
		CallbackQuery  string            `json:"callback_query_id"`
		ReplyMarkup    telego.ForceReply `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[2].Data.BodyRaw, &prompt))
	assert.Equal(t, ownerID, prompt.ReceiverUserID)
	assert.Equal(t, "ephemeral-rename-click", prompt.CallbackQuery)
	assert.True(t, prompt.ReplyMarkup.ForceReply)

	reply := &telego.Message{
		EphemeralMessageID: 79, MessageThreadID: threadID,
		From: &telego.User{ID: ownerID}, Text: "Nama Ephemeral Baru",
		Chat: telego.Chat{ID: chatID, Type: telego.ChatTypeSupergroup, IsForum: true},
		ReplyToMessage: &telego.Message{
			EphemeralMessageID: promptMessageID, MessageThreadID: threadID,
			Chat: telego.Chat{ID: chatID},
		},
	}
	require.NoError(t, ch.handleMessage(context.Background(), reply))
	require.Len(t, requests, 2)
	assert.Equal(t, "Nama Ephemeral Baru", requests[1].Value)
	assert.Equal(t, content.Interaction.Current, requests[1].SessionKey)
	require.Len(t, caller.calls, 4)
	assert.Contains(t, caller.calls[3].URL, "editEphemeralMessageText")
	var edited struct {
		ReceiverUserID     int64 `json:"receiver_user_id"`
		EphemeralMessageID int   `json:"ephemeral_message_id"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[3].Data.BodyRaw, &edited))
	assert.Equal(t, ownerID, edited.ReceiverUserID)
	assert.Equal(t, dashboardMessageID, edited.EphemeralMessageID)
	select {
	case inbound := <-messageBus.InboundChan():
		t.Fatalf("ephemeral rename reply leaked into inbound bus: %+v", inbound)
	default:
	}
}

func TestInternalSessionCallbackNeverPublishesInboundMessage(t *testing.T) {
	messageBus := bus.NewMessageBus()
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		switch {
		case strings.Contains(url, "answerCallbackQuery"):
			return callbackSuccessResponse(t), nil
		case strings.Contains(url, "editMessageText"):
			return successResponseWithMessageID(t, 91), nil
		default:
			return nil, errors.New("unexpected call " + url)
		}
	}}
	ch := newTestChannel(t, caller)
	ch.BaseChannel = channels.NewBaseChannel("telegram", nil, messageBus, nil)
	content := testSessionStructuredContent()
	markup, pending, err := ch.structuredReplyMarkup(content, 12345, 0)
	require.NoError(t, err)
	pending.messageID = 91
	ch.storeSessionMenu(*pending)
	ch.SetInternalCallbackHandler(
		func(context.Context, bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
			return &bus.InternalCallbackResponse{Content: content}, nil
		},
	)
	button := markup.InlineKeyboard[0][0]
	require.NoError(t, ch.handleCallbackQuery(context.Background(), &telego.CallbackQuery{
		ID: "internal-only", From: telego.User{ID: 42}, Data: button.CallbackData,
		Message: &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}},
	}))
	select {
	case inbound := <-messageBus.InboundChan():
		t.Fatalf("internal callback leaked into inbound bus: %+v", inbound)
	default:
	}
}

func TestNativeStructuredLimitsFallBackSafely(t *testing.T) {
	tooManyColumns := &bus.StructuredContent{
		Tables: []bus.StructuredTable{{Columns: make([]string, richMessageMaxColumns+1)}},
	}
	_, ok := buildNativeRichMessage(tooManyColumns)
	assert.False(t, ok)
	tooManyBlocks := &bus.StructuredContent{
		Tables: []bus.StructuredTable{
			{Columns: []string{"A"}, Header: true, Rows: make([][]string, richMessageMaxBlocks)},
		},
	}
	_, ok = buildNativeRichMessage(tooManyBlocks)
	assert.False(t, ok)
	tooManyBytes := &bus.StructuredContent{Paragraphs: []string{strings.Repeat("x", richMessageMaxBytes+1)}}
	_, ok = buildNativeRichMessage(tooManyBytes)
	assert.False(t, ok)
}

func testMemoryStructuredContent() *bus.StructuredContent {
	inbound := bus.InboundContext{Channel: "telegram", ChatID: "12345", ChatType: "direct", SenderID: "42"}
	return &bus.StructuredContent{
		Kind:  "memory_dashboard",
		Title: "Personal Memory",
		Paragraphs: []string{
			"Workspace entries: 3",
			"User entries: 12",
			"Pending review: 1",
		},
		Fallback: "Personal Memory\nWorkspace entries: 3\nUser entries: 12\nPending review: 1",
		Interaction: &bus.InteractionMenu{
			Kind:    "memory",
			OwnerID: "42",
			Channel: "telegram",
			ChatID:  "12345",
			AgentID: "main",
			Inbound: inbound,
			Entries: []bus.InteractionEntry{
				{Label: "👤 My Profile", Action: "profile"},
				{Label: "📚 Browse", Action: "browse"},
				{Label: "🔎 Search", Action: "search"},
				{Label: "📝 Pending", Action: "pending"},
				{Label: "✖️ Tutup", Action: "close"},
			},
		},
	}
}

func TestMemoryStructuredContentAndKeyboard(t *testing.T) {
	content := testMemoryStructuredContent()
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		switch {
		case strings.Contains(url, "answerCallbackQuery"):
			return callbackSuccessResponse(t), nil
		case strings.Contains(url, "editMessageText"):
			return successResponseWithMessageID(t, 91), nil
		default:
			return successResponseWithMessageID(t, 91), nil
		}
	}}
	ch := newTestChannel(t, caller)
	markup, pending, err := ch.structuredReplyMarkup(content, 12345, 0)
	require.NoError(t, err)
	require.NotNil(t, markup)
	require.NotNil(t, pending)
	assert.Equal(t, "memory", pending.menu.Kind)

	var capturedReq *bus.InternalCallbackRequest
	ch.SetInternalCallbackHandler(
		func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
			capturedReq = &req
			return &bus.InternalCallbackResponse{Content: content}, nil
		},
	)

	pending.messageID = 91
	ch.storeSessionMenu(*pending)

	button := markup.InlineKeyboard[0][0] // profile button
	require.True(t, len([]byte(button.CallbackData)) <= 64)
	query := &telego.CallbackQuery{
		ID: "mem-callback-1", From: telego.User{ID: 42}, Data: button.CallbackData,
		Message: &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}},
	}
	require.NoError(t, ch.handleCallbackQuery(context.Background(), query))
	require.NotNil(t, capturedReq)
	assert.Equal(t, "memory", capturedReq.Kind)
	assert.Equal(t, "profile", capturedReq.Action)
	assert.Equal(t, "42", capturedReq.OwnerID)
}

func TestEphemeralFormattedStructuredFallback_ParseRejectionFallback(t *testing.T) {
	const (
		chatID      = int64(-10055)
		threadID    = 7
		ownerID     = int64(42)
		ephemeralID = 77
	)
	sendCalls := 0
	caller := &stubCaller{callFn: func(_ context.Context, url string, req *ta.RequestData) (*ta.Response, error) {
		if strings.Contains(url, "sendMessage") {
			sendCalls++
			if sendCalls == 1 {
				// Reject formatting first
				return nil, errors.New("Bad Request: can't parse entities in message")
			}
			return successEphemeralResponse(t, chatID, threadID, ownerID, ephemeralID), nil
		}
		return nil, errors.New("unexpected API call " + url)
	}}
	ch := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, ch, chatID, threadID, ownerID, 0, "origin-callback")
	content := testMemoryStructuredContent()
	content.Interaction.ChatID = strconvFormatChat(chatID, threadID)
	content.Interaction.TopicID = strconv.Itoa(threadID)
	content.Interaction.OwnerID = strconv.FormatInt(ownerID, 10)
	content.Interaction.Inbound = privateOutboundContext(target)
	content.Interaction.Inbound.PrivateSession = true

	ids, err := ch.sendStructuredContent(
		context.Background(),
		bus.OutboundMessage{Content: content.FallbackText(), Structured: content},
		chatID,
		threadID,
		&target,
	)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	assert.Equal(t, 2, sendCalls)
	// Verify sendRichMessage was never called
	for _, call := range caller.calls {
		assert.NotContains(t, call.URL, "sendRichMessage")
	}
}

func TestMemoryForceReply_SearchAndEditFlows(t *testing.T) {
	const (
		chatID             = int64(-10055)
		threadID           = 7
		ownerID            = int64(42)
		dashboardMessageID = 77
		promptMessageID    = 78
	)
	sendMessageCalls := 0
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		switch {
		case strings.Contains(url, "sendMessage"):
			sendMessageCalls++
			if sendMessageCalls == 1 {
				return successEphemeralResponse(t, chatID, threadID, ownerID, dashboardMessageID), nil
			}
			return successEphemeralResponse(t, chatID, threadID, ownerID, promptMessageID), nil
		case strings.Contains(url, "answerCallbackQuery"), strings.Contains(url, "editEphemeralMessageText"):
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

	var capturedReq *bus.InternalCallbackRequest
	ch.SetInternalCallbackHandler(
		func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
			capturedReq = &req
			if strings.TrimSpace(req.Value) == "" {
				return &bus.InternalCallbackResponse{Text: "Balas prompt pencarian."}, nil
			}
			updated := testMemoryStructuredContent()
			updated.Title = "Hasil Pencarian: " + req.Value
			return &bus.InternalCallbackResponse{Content: updated}, nil
		},
	)

	var sent struct {
		ReplyMarkup telego.InlineKeyboardMarkup `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &sent))
	searchBtn := findInlineButton(t, &sent.ReplyMarkup, "🔎 Search")

	// 1. User clicks Search
	require.NoError(t, ch.handleCallbackQuery(context.Background(), &telego.CallbackQuery{
		ID: "search-click", From: telego.User{ID: ownerID}, Data: searchBtn.CallbackData,
		Message: &telego.Message{
			EphemeralMessageID: dashboardMessageID, MessageThreadID: threadID,
			Chat: telego.Chat{ID: chatID, Type: telego.ChatTypeSupergroup, IsForum: true},
		},
	}))

	// 2. User replies to ForceReply
	reply := &telego.Message{
		EphemeralMessageID: 80, MessageThreadID: threadID,
		From: &telego.User{ID: ownerID}, Text: "golang concurrency",
		Chat: telego.Chat{ID: chatID, Type: telego.ChatTypeSupergroup, IsForum: true},
		ReplyToMessage: &telego.Message{
			EphemeralMessageID: promptMessageID, MessageThreadID: threadID,
			Chat: telego.Chat{ID: chatID},
		},
	}
	require.NoError(t, ch.handleMessage(context.Background(), reply))
	require.NotNil(t, capturedReq)
	assert.Equal(t, "search", capturedReq.Action)
	assert.Equal(t, "golang concurrency", capturedReq.Value)

	// 3. Ensure no message leaked to inbound agent bus
	select {
	case inbound := <-messageBus.InboundChan():
		t.Fatalf("search reply leaked to agent bus: %+v", inbound)
	default:
	}
}
