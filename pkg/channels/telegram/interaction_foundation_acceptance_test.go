package telegram

import (
	"testing"
	"time"

	"github.com/mymmrac/telego"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestInteractionMutationActionAllowlistIncludesSkillAndCheckpoint(t *testing.T) {
	for _, tc := range []struct {
		kind   string
		action string
		want   bool
	}{
		{kind: "skill", action: "arm", want: true},
		{kind: "skill", action: "clear", want: true},
		{kind: "skill", action: "detail", want: false},
		{kind: "checkpoint", action: "resume", want: true},
		{kind: "checkpoint", action: "archive_confirm", want: true},
		{kind: "checkpoint", action: "archive", want: false},
		{kind: "checkpoint", action: "detail", want: false},
		{kind: "unknown", action: "resume", want: false},
	} {
		t.Run(tc.kind+"/"+tc.action, func(t *testing.T) {
			assert.Equal(t, tc.want, isInteractionMutationAction(tc.kind, tc.action))
		})
	}
}

func TestSkillSearchPromptRejectsCrossScopeExpiredAndReplay(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})
	inbound := bus.InboundContext{
		Channel: "telegram", Account: "telegram", ChatID: "12345/7", TopicID: "7",
		ChatType: "group", SenderID: "42", PrivateResponse: true, PrivateRouteToken: "verified-route",
	}
	menu := telegramSessionMenu{
		chatID: 12345, threadID: 7, messageID: 91, receiverUserID: 42,
		menu: bus.InteractionMenu{
			Kind: "skill", OwnerID: "42", Channel: "telegram", Account: "telegram", ChatID: "12345/7",
			TopicID: "7", AgentID: "main", Scope: "safe-scope", SessionKey: "si_v1_bound", Inbound: inbound,
		},
	}
	key := telegramSessionRenamePromptKey{chatID: 12345, threadID: 7, messageID: 92}
	ch.storeSessionRenamePrompt(key, telegramSessionRenamePrompt{
		token: "skill-search", menu: menu, action: "search", createdAt: time.Now(),
	})

	wrongUser := &telego.Message{
		MessageID: 100, From: &telego.User{ID: 99}, Text: "secret",
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

	invalid := *wrongUser
	invalid.From = &telego.User{ID: 42}
	invalid.Text = ""
	_, status = ch.claimSessionRenamePrompt(&invalid)
	assert.Equal(t, sessionRenameClaimInvalid, status)

	valid := *wrongUser
	valid.From = &telego.User{ID: 42}
	valid.Text = "calendar"
	_, status = ch.claimSessionRenamePrompt(&valid)
	assert.Equal(t, sessionRenameClaimed, status)
	_, status = ch.claimSessionRenamePrompt(&valid)
	assert.Equal(t, sessionRenameClaimReplay, status)

	expiredKey := telegramSessionRenamePromptKey{chatID: 12345, threadID: 7, messageID: 94}
	ch.storeSessionRenamePrompt(expiredKey, telegramSessionRenamePrompt{
		token: "expired-skill-search", menu: menu, action: "search",
		createdAt: time.Now().Add(-sessionRenameTTL - time.Second),
	})
	expiredReply := &telego.Message{
		MessageID: 102, From: &telego.User{ID: 42}, Text: "calendar",
		Chat: telego.Chat{ID: 12345}, MessageThreadID: 7,
		ReplyToMessage: &telego.Message{MessageID: 94},
	}
	_, status = ch.claimSessionRenamePrompt(expiredReply)
	require.Equal(t, sessionRenameClaimExpired, status)
}

func TestSkillCallbackPayloadDoesNotExposeSearchQuery(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})
	query := "confidential search query"
	inbound := bus.InboundContext{
		Channel: "telegram", Account: "telegram", ChatID: "12345", ChatType: "direct", SenderID: "42",
	}
	content := &bus.StructuredContent{Interaction: &bus.InteractionMenu{
		Kind: "skill", OwnerID: "42", Channel: "telegram", Account: "telegram", ChatID: "12345",
		AgentID: "main", Scope: "safe-scope", SessionKey: "si_v1_bound", Query: query, Inbound: inbound,
		Page: 0, Pages: 1, Entries: []bus.InteractionEntry{
			{Label: "1", Action: "detail", Value: "calendar"},
			{Label: "Close", Action: "close"},
		},
	}}
	markup, pending, err := ch.structuredReplyMarkup(content, 12345, 0)
	require.NoError(t, err)
	require.NotNil(t, markup)
	require.NotNil(t, pending)
	assert.Equal(t, query, pending.menu.Query)
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			assert.LessOrEqual(t, len([]byte(button.CallbackData)), 64)
			assert.NotContains(t, button.CallbackData, query)
		}
	}
}
