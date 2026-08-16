package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
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
		Interaction: &bus.InteractionMenu{Kind: "session", OwnerID: "42", Channel: "telegram", ChatID: "12345", AgentID: "main", Scope: "scope-signature", Inbound: inbound, Page: 0, Pages: 1, Current: "si_v1_secret-session-key-that-must-not-leak", Entries: []bus.InteractionEntry{
			{Label: "1", Action: "select", Value: "si_v1_secret-session-key-that-must-not-leak"},
			{Label: "2", Action: "select", Value: "si_v1_other-secret"},
			{Label: "Halaman 1/1", Action: "noop"},
			{Label: "➕ Baru", Action: "new"},
			{Label: "✏️ Rename", Action: "rename"},
			{Label: "✖️ Tutup", Action: "close"},
		}},
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
	_, err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "12345", Content: "fallback", Structured: testSessionStructuredContent()})
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
	_, err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "12345", Content: content.Fallback, Structured: content})
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
	ch.SetInternalCallbackHandler(func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
		got = req
		updated := testSessionStructuredContent()
		updated.Interaction.Page = 0
		return &bus.InternalCallbackResponse{Content: updated}, nil
	})
	_, err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "12345", Content: "fallback", Structured: testSessionStructuredContent()})
	require.NoError(t, err)
	require.Len(t, caller.calls, 1)
	var sent struct {
		ReplyMarkup telego.InlineKeyboardMarkup `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &sent))
	button := sent.ReplyMarkup.InlineKeyboard[0][0]
	query := &telego.CallbackQuery{ID: "callback-1", From: telego.User{ID: 42}, Data: button.CallbackData, Message: &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}}}
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
	ch.SetInternalCallbackHandler(func(context.Context, bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
		called = true
		return nil, nil
	})
	_, err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "12345", Content: "fallback", Structured: testSessionStructuredContent()})
	require.NoError(t, err)
	var sent struct {
		ReplyMarkup telego.InlineKeyboardMarkup `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &sent))
	data := sent.ReplyMarkup.InlineKeyboard[0][0].CallbackData
	other := &telego.CallbackQuery{ID: "other", From: telego.User{ID: 99}, Data: data, Message: &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}}}
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
	expired := &telego.CallbackQuery{ID: "expired", From: telego.User{ID: 42}, Data: data, Message: &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}}}
	require.NoError(t, ch.handleCallbackQuery(context.Background(), expired))
	assert.True(t, len(caller.calls) >= 3)
}

func TestSessionCallbackDataParserAndActions(t *testing.T) {
	menu := *testSessionStructuredContent().Interaction
	for _, code := range []string{"s0", "s1", "n", "r", "x", "o", "p0"} {
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
		bus.InteractionEntry{Label: "➕ Baru", Action: "new"},
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
	assert.Len(t, markup.InlineKeyboard[2], 2, "long action labels are limited to two per row")
	assert.Len(t, markup.InlineKeyboard[3], 1)
	assert.Equal(t, telego.ButtonStyleSuccess, findInlineButton(t, markup, "6").Style)
	assert.Equal(t, telego.ButtonStylePrimary, findInlineButton(t, markup, "➕ Baru").Style)
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
		{name: "new", label: "➕ Baru", expect: "new", expectEdit: "editMessageText"},
		{name: "rename", label: "✏️ Rename", expect: "rename"},
		{name: "close", label: "✖️ Tutup", expect: "close", close: true, expectEdit: "editMessageReplyMarkup"},
		{name: "pagination", label: "▶️", expect: "page", withPages: true, expectEdit: "editMessageText"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
				switch {
				case strings.Contains(url, "answerCallbackQuery"):
					return callbackSuccessResponse(t), nil
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
				content.Interaction.Entries = append(content.Interaction.Entries, bus.InteractionEntry{Label: "▶️", Action: "page", Value: "1"})
			}
			markup, pending, err := ch.structuredReplyMarkup(content, 12345, 0)
			require.NoError(t, err)
			require.NotNil(t, pending)
			pending.messageID = 91
			ch.storeSessionMenu(*pending)
			var got bus.InternalCallbackRequest
			ch.SetInternalCallbackHandler(func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
				got = req
				if tt.close {
					return &bus.InternalCallbackResponse{Close: true}, nil
				}
				if tt.expect == "rename" {
					return &bus.InternalCallbackResponse{Text: "rename help"}, nil
				}
				return &bus.InternalCallbackResponse{Content: testSessionStructuredContent()}, nil
			})
			button := findInlineButton(t, markup, tt.label)
			query := &telego.CallbackQuery{ID: "callback-" + tt.name, From: telego.User{ID: 42}, Data: button.CallbackData, Message: &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}}}
			require.NoError(t, ch.handleCallbackQuery(context.Background(), query))
			assert.Equal(t, tt.expect, got.Action)
			require.NotEmpty(t, caller.calls)
			assert.Contains(t, caller.calls[0].URL, "answerCallbackQuery")
			if tt.expectEdit == "" {
				assert.Len(t, caller.calls, 1)
			} else {
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
			menu := telegramSessionMenu{chatID: tt.chatID, threadID: tt.threadID, messageID: 91, menu: bus.InteractionMenu{OwnerID: "42", Channel: "telegram"}}
			message := &telego.Message{MessageID: 91, MessageThreadID: tt.threadID, Chat: telego.Chat{ID: tt.chatID, Type: tt.chatType, IsForum: tt.threadID != 0}}
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

	ids, err := ch.sendStructuredContent(context.Background(), bus.OutboundMessage{Content: content.FallbackText(), Structured: content}, chatID, threadID, &target)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	require.Len(t, caller.calls, 1)
	assert.Contains(t, caller.calls[0].URL, "sendMessage")
	assert.NotContains(t, caller.calls[0].URL, "sendRichMessage")
	var sent struct {
		ReplyMarkup telego.InlineKeyboardMarkup `json:"reply_markup"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &sent))

	ch.SetInternalCallbackHandler(func(_ context.Context, req bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
		assert.Equal(t, "select", req.Action)
		return &bus.InternalCallbackResponse{Content: content}, nil
	})
	button := sent.ReplyMarkup.InlineKeyboard[0][0]
	query := &telego.CallbackQuery{ID: "ephemeral-callback", From: telego.User{ID: ownerID}, Data: button.CallbackData, Message: &telego.Message{
		MessageID: 0, EphemeralMessageID: ephemeralID, MessageThreadID: threadID,
		Chat: telego.Chat{ID: chatID, Type: telego.ChatTypeSupergroup, IsForum: true},
	}}
	require.NoError(t, ch.handleCallbackQuery(context.Background(), query))
	require.Len(t, caller.calls, 3)
	assert.Contains(t, caller.calls[1].URL, "answerCallbackQuery")
	assert.Contains(t, caller.calls[2].URL, "editEphemeralMessageText")
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
	ch.SetInternalCallbackHandler(func(context.Context, bus.InternalCallbackRequest) (*bus.InternalCallbackResponse, error) {
		return &bus.InternalCallbackResponse{Content: content}, nil
	})
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
	tooManyColumns := &bus.StructuredContent{Tables: []bus.StructuredTable{{Columns: make([]string, richMessageMaxColumns+1)}}}
	_, ok := buildNativeRichMessage(tooManyColumns)
	assert.False(t, ok)
	tooManyBlocks := &bus.StructuredContent{Tables: []bus.StructuredTable{{Columns: []string{"A"}, Header: true, Rows: make([][]string, richMessageMaxBlocks)}}}
	_, ok = buildNativeRichMessage(tooManyBlocks)
	assert.False(t, ok)
	tooManyBytes := &bus.StructuredContent{Paragraphs: []string{strings.Repeat("x", richMessageMaxBytes+1)}}
	_, ok = buildNativeRichMessage(tooManyBytes)
	assert.False(t, ok)
}
