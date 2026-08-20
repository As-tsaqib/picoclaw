package telegram

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func discoveryTestContent() *bus.StructuredContent {
	inbound := bus.InboundContext{
		Channel: "telegram", Account: "telegram", ChatID: "12345/7", TopicID: "7",
		ChatType: "group", SenderID: "42", PrivateResponse: true, PrivateRouteToken: "verified-route",
	}
	return &bus.StructuredContent{
		Title:    "Browse Resources",
		Fallback: "Browse Resources\n- Models\n- Skills",
		Interaction: &bus.InteractionMenu{
			Kind: "discovery", OwnerID: "42", Channel: "telegram", Account: "telegram",
			ChatID: "12345/7", TopicID: "7", AgentID: "main", Scope: "safe-scope",
			SessionKey: "si_v1_private_bound", Inbound: inbound, Page: 0, Pages: 2,
			Entries: []bus.InteractionEntry{
				{Label: "🤖 Models", Action: "list_models", Value: "private-model-state"},
				{Label: "▶️", Action: "list_channels_page", Value: "1"},
				{Label: "✖️ Close", Action: "close"},
			},
		},
	}
}

func TestDiscoveryStructuredReplyMarkupIsAllowedBoundAndOpaque(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})
	content := discoveryTestContent()
	markup, pending, err := ch.structuredReplyMarkup(content, 12345, 7)
	require.NoError(t, err)
	require.NotNil(t, markup)
	require.NotNil(t, pending)
	assert.Equal(t, "discovery", pending.menu.Kind)
	assert.Equal(t, "si_v1_private_bound", pending.menu.SessionKey)
	assert.Equal(t, "telegram", pending.menu.Account)

	buttons := 0
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			buttons++
			data := button.CallbackData
			assert.NotEmpty(t, data)
			assert.LessOrEqual(t, len([]byte(data)), 64)
			assert.NotContains(t, data, "private-model-state")
			assert.NotContains(t, data, "si_v1_private_bound")
			assert.NotContains(t, data, "list_models")
			assert.NotContains(t, data, "/list")
		}
	}
	assert.GreaterOrEqual(t, buttons, 3)
}

func TestDiscoveryTransportRequiresScopeSessionAndTrustedRoute(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})

	for _, tc := range []struct {
		name   string
		mutate func(*bus.InteractionMenu)
	}{
		{name: "missing scope", mutate: func(menu *bus.InteractionMenu) { menu.Scope = "" }},
		{name: "missing session", mutate: func(menu *bus.InteractionMenu) { menu.SessionKey = "" }},
		{name: "wrong account", mutate: func(menu *bus.InteractionMenu) { menu.Account = "other-bot" }},
		{name: "wrong chat", mutate: func(menu *bus.InteractionMenu) { menu.ChatID = "999/7" }},
		{name: "wrong topic", mutate: func(menu *bus.InteractionMenu) { menu.TopicID = "8" }},
		{name: "wrong owner binding", mutate: func(menu *bus.InteractionMenu) { menu.OwnerID = "99" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := discoveryTestContent()
			tc.mutate(content.Interaction)
			_, _, err := ch.structuredReplyMarkup(content, 12345, 7)
			require.Error(t, err)
		})
	}
}

func TestDiscoveryEntryCallbacksResolveOnlyStoredActions(t *testing.T) {
	menu := discoveryTestContent().Interaction
	require.NotNil(t, menu)

	action, value, ok := resolveSessionMenuAction(*menu, "e0")
	require.True(t, ok)
	assert.Equal(t, "list_models", action)
	assert.Equal(t, "private-model-state", value)

	action, value, ok = resolveSessionMenuAction(*menu, "e1")
	require.True(t, ok)
	assert.Equal(t, "list_channels_page", action)
	assert.Equal(t, "1", value)

	_, _, ok = resolveSessionMenuAction(*menu, "e99")
	assert.False(t, ok)
	_, _, ok = resolveSessionMenuAction(*menu, "x999")
	assert.False(t, ok)
}

func TestDiscoveryCallbackEnvelopeRejectsWrongPhysicalMessageAndOwner(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})
	content := discoveryTestContent()
	_, pending, err := ch.structuredReplyMarkup(content, 12345, 7)
	require.NoError(t, err)
	require.NotNil(t, pending)
	pending.messageID = 91
	pending.receiverUserID = 42

	validMessage := &telego.Message{MessageID: 91, Chat: telego.Chat{ID: 12345}, MessageThreadID: 7}
	validQuery := &telego.CallbackQuery{ID: "q", From: telego.User{ID: 42, FirstName: "Owner"}}
	assert.True(t, ch.sessionCallbackEnvelopeValid(validQuery, validMessage, *pending))

	wrongMessage := *validMessage
	wrongMessage.MessageID = 92
	assert.False(t, ch.sessionCallbackEnvelopeValid(validQuery, &wrongMessage, *pending))

	wrongTopic := *validMessage
	wrongTopic.MessageThreadID = 8
	assert.False(t, ch.sessionCallbackEnvelopeValid(validQuery, &wrongTopic, *pending))

	wrongOwner := *validQuery
	wrongOwner.From.ID = 99
	assert.False(t, ch.sessionCallbackEnvelopeValid(&wrongOwner, validMessage, *pending))
}

func TestDiscoveryCallbackEncodingNeverContainsRawActionOrValue(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})
	content := discoveryTestContent()
	markup, pending, err := ch.structuredReplyMarkup(content, 12345, 7)
	require.NoError(t, err)
	require.NotNil(t, pending)

	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			token, code, ok := parseInternalSessionCallback(button.CallbackData)
			require.True(t, ok)
			assert.Len(t, token, 12)
			assert.NotEmpty(t, code)
			assert.NotContains(t, button.CallbackData, pending.menu.Entries[0].Action)
			assert.NotContains(t, button.CallbackData, pending.menu.Entries[0].Value)
			assert.False(t, strings.Contains(button.CallbackData, strconv.Itoa(pending.menu.Page)+":list"))
		}
	}
}
