package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryInteractionAccountSealedFromTrustedChannel(t *testing.T) {
	content := testMemoryStructuredContent()
	content.Interaction.Account = ""
	content.Interaction.Inbound.Account = ""
	ch := newTestChannel(t, &stubCaller{})

	_, pending, err := ch.structuredReplyMarkup(content, 12345, 0)
	require.NoError(t, err)
	require.NotNil(t, pending)
	assert.Equal(t, ch.Name(), pending.menu.Account)
	assert.Equal(t, ch.Name(), pending.menu.Inbound.Account)
}

func TestMemoryInteractionAccountMismatchStillFailsClosed(t *testing.T) {
	content := testMemoryStructuredContent()
	content.Interaction.Account = "account-a"
	content.Interaction.Inbound.Account = "account-b"
	ch := newTestChannel(t, &stubCaller{})

	_, pending, err := ch.structuredReplyMarkup(content, 12345, 0)
	require.Error(t, err)
	assert.Nil(t, pending)
	assert.Contains(t, err.Error(), "trusted inbound mismatch")
}

func TestPrivatePromptNoticeUsesStoredReceiverAuthority(t *testing.T) {
	const (
		chatID   = int64(-10054)
		threadID = 6
		ownerID  = int64(42)
	)
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		if !strings.Contains(url, "sendMessage") {
			return nil, errors.New("unexpected API call " + url)
		}
		return successEphemeralResponse(t, chatID, threadID, ownerID, 81), nil
	}}
	ch := newTestChannel(t, caller)
	prompt := telegramSessionRenamePrompt{menu: telegramSessionMenu{
		chatID: chatID, threadID: threadID, ephemeralID: 77, receiverUserID: ownerID,
	}}

	// The user's reply itself is deliberately non-ephemeral. Privacy authority
	// must come from the previously validated prompt, not from this message.
	err := ch.sendSessionRenameNotice(context.Background(), &telego.Message{
		MessageID: 79, MessageThreadID: threadID, From: &telego.User{ID: ownerID},
		Chat: telego.Chat{ID: chatID, Type: telego.ChatTypeSupergroup, IsForum: true},
	}, prompt, "private notice")
	require.NoError(t, err)
	require.Len(t, caller.calls, 1)
	var sent struct {
		ReceiverUserID int64                   `json:"receiver_user_id"`
		Reply          *telego.ReplyParameters `json:"reply_parameters"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &sent))
	assert.Equal(t, ownerID, sent.ReceiverUserID)
	assert.Nil(t, sent.Reply, "private notice must not widen authority by replying to a public group message")
}

func TestPrivatePromptNoticeFailsClosedWithoutReceiverAuthority(t *testing.T) {
	caller := &stubCaller{}
	ch := newTestChannel(t, caller)
	prompt := telegramSessionRenamePrompt{menu: telegramSessionMenu{
		chatID: -10054, threadID: 6, ephemeralID: 77,
	}}
	err := ch.sendSessionRenameNotice(context.Background(), &telego.Message{
		MessageID: 79, MessageThreadID: 6, Chat: telego.Chat{ID: -10054},
	}, prompt, "must stay private")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "receiver authority")
	assert.Empty(t, caller.calls)
}
