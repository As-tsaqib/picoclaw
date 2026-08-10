package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/commands"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/media"
)

func successEphemeralResponse(
	t *testing.T,
	chatID int64,
	threadID int,
	receiverUserID int64,
	ephemeralMessageID int,
) *ta.Response {
	t.Helper()
	message := &telego.Message{
		MessageThreadID:    threadID,
		MessageID:          0,
		EphemeralMessageID: ephemeralMessageID,
		ReceiverUser:       &telego.User{ID: receiverUserID},
		Chat:               telego.Chat{ID: chatID},
	}
	data, err := json.Marshal(message)
	require.NoError(t, err)
	return &ta.Response{Ok: true, Result: data}
}

func successBoolResponse() *ta.Response {
	return &ta.Response{Ok: true, Result: []byte("true")}
}

func newEphemeralInboundChannel(
	t *testing.T,
	ephemeral config.TelegramEphemeralConfig,
) (*TelegramChannel, *bus.MessageBus) {
	t.Helper()
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return successBoolResponse(), nil
	}}
	channel := newTestChannel(t, caller)
	messageBus := bus.NewMessageBus()
	channel.BaseChannel = channels.NewBaseChannel("telegram", nil, messageBus, nil)
	channel.ctx = context.Background()
	channel.tgCfg = &config.TelegramSettings{Ephemeral: ephemeral}
	return channel, messageBus
}

func mustRegisterEphemeralTarget(
	t *testing.T,
	channel *TelegramChannel,
	chatID int64,
	threadID int,
	receiverUserID int64,
	incomingEphemeralMessageID int,
	callbackQueryID string,
) telegramEphemeralTarget {
	t.Helper()
	target, err := channel.registerEphemeralTarget(&telego.Message{
		MessageThreadID:    threadID,
		EphemeralMessageID: incomingEphemeralMessageID,
		Chat: telego.Chat{
			ID:      chatID,
			Type:    telego.ChatTypeSupergroup,
			IsForum: threadID != 0,
		},
		From: &telego.User{ID: receiverUserID},
	}, telegramPrivateInboundPlan{enabled: true, callbackQueryID: callbackQueryID})
	require.NoError(t, err)
	return target
}

func privateOutboundContext(target telegramEphemeralTarget) bus.InboundContext {
	chatID := strconvFormatChat(target.ChatID, target.ThreadID)
	return bus.InboundContext{
		Channel:           "telegram",
		ChatID:            chatID,
		ChatType:          "group",
		TopicID:           topicIDString(target.ThreadID),
		SenderID:          fmt.Sprintf("%d", target.ReceiverUserID),
		PrivateResponse:   true,
		PrivateRouteToken: target.Token,
		Raw: map[string]string{
			"receiver_user_id": "999999",
		},
	}
}

func strconvFormatChat(chatID int64, threadID int) string {
	if threadID == 0 {
		return fmt.Sprintf("%d", chatID)
	}
	return fmt.Sprintf("%d/%d", chatID, threadID)
}

func topicIDString(threadID int) string {
	if threadID == 0 {
		return ""
	}
	return fmt.Sprintf("%d", threadID)
}

func TestTelegramEphemeralDisabled_NormalSendPayloadUnchanged(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return successResponseWithMessageID(t, 15), nil
	}}
	channel := newTestChannel(t, caller)

	ids, err := channel.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "12345",
		Content: "synthetic normal response",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"15"}, ids)
	require.Len(t, caller.calls, 1)
	assert.NotContains(t, string(caller.calls[0].Data.BodyRaw), "receiver_user_id")
	assert.NotContains(t, string(caller.calls[0].Data.BodyRaw), "callback_query_id")
}

func TestTelegramEphemeralInboundCommand_UsesVerifiedSenderAndIsolatesSession(t *testing.T) {
	channel, messageBus := newEphemeralInboundChannel(t, config.TelegramEphemeralConfig{
		Mode:     config.TelegramEphemeralModeCommands,
		Commands: []string{"clear"},
	})
	message := &telego.Message{
		Text:      "/clear",
		MessageID: 31,
		Entities: []telego.MessageEntity{{
			Type:   telego.EntityTypeBotCommand,
			Offset: 0,
			Length: len("/clear"),
		}},
		Chat: telego.Chat{ID: -100123, Type: telego.ChatTypeSupergroup},
		From: &telego.User{ID: 42, FirstName: "Synthetic User"},
	}

	require.NoError(t, channel.handleMessage(context.Background(), message))
	inbound := <-messageBus.InboundChan()
	assert.True(t, inbound.Context.PrivateResponse)
	assert.True(t, inbound.Context.PrivateSession)
	assert.NotEmpty(t, inbound.Context.PrivateRouteToken)
	assert.Equal(t, "42", inbound.Context.SenderID)

	channel.ephemeralMu.Lock()
	target := channel.ephemeralRoutes[inbound.Context.PrivateRouteToken]
	channel.ephemeralMu.Unlock()
	assert.Equal(t, int64(42), target.ReceiverUserID)
}

func TestTelegramEphemeralInbound_CapturesReceiverMetadataAndDistinctIncomingID(t *testing.T) {
	channel, messageBus := newEphemeralInboundChannel(t, config.TelegramEphemeralConfig{
		Mode: config.TelegramEphemeralModeAll,
	})
	message := &telego.Message{
		Text:               "synthetic private request",
		MessageID:          0,
		EphemeralMessageID: 73,
		Chat:               telego.Chat{ID: -100124, Type: telego.ChatTypeSupergroup},
		From:               &telego.User{ID: 43},
		ReceiverUser:       &telego.User{ID: 900, IsBot: true},
	}

	require.NoError(t, channel.handleMessage(context.Background(), message))
	inbound := <-messageBus.InboundChan()
	assert.True(t, strings.HasPrefix(inbound.MessageID, inboundEphemeralIDPrefix))
	assert.False(t, strings.HasPrefix(inbound.MessageID, ephemeralMessageIDPrefix))

	channel.ephemeralMu.Lock()
	target := channel.ephemeralRoutes[inbound.Context.PrivateRouteToken]
	channel.ephemeralMu.Unlock()
	assert.Equal(t, int64(900), target.IncomingReceiverUserID)
	assert.True(t, target.IncomingReceiverIsBot)
	assert.Equal(t, 73, target.IncomingEphemeralMessageID)
}

func TestTelegramEphemeralCommands_ContinuesVerifiedIncomingEphemeralMessage(t *testing.T) {
	channel, messageBus := newEphemeralInboundChannel(t, config.TelegramEphemeralConfig{
		Mode:     config.TelegramEphemeralModeCommands,
		Commands: []string{"clear"},
	})
	message := &telego.Message{
		Text:               "follow-up in the private interaction",
		MessageID:          0,
		EphemeralMessageID: 74,
		Chat:               telego.Chat{ID: -100125, Type: telego.ChatTypeSupergroup},
		From:               &telego.User{ID: 44},
	}

	require.NoError(t, channel.handleMessage(context.Background(), message))
	inbound := <-messageBus.InboundChan()
	assert.True(t, inbound.Context.PrivateResponse)
	assert.True(t, inbound.Context.PrivateSession)
}

func TestTelegramEphemeralCommands_CallbackFromPrivateMessageRemainsPrivate(t *testing.T) {
	channel, messageBus := newEphemeralInboundChannel(t, config.TelegramEphemeralConfig{
		Mode:     config.TelegramEphemeralModeCommands,
		Commands: []string{"clear"},
	})
	query := &telego.CallbackQuery{
		ID:   "synthetic-private-callback",
		From: telego.User{ID: 315},
		Data: "button-data-without-command-prefix",
		Message: &telego.Message{
			MessageID:          0,
			EphemeralMessageID: 75,
			Chat:               telego.Chat{ID: -100790, Type: telego.ChatTypeSupergroup},
		},
	}

	require.NoError(t, channel.handleCallbackQuery(context.Background(), query))
	inbound := <-messageBus.InboundChan()
	assert.True(t, inbound.Context.PrivateResponse)
}

func TestTelegramEphemeralCommandRegistration_MarksOnlyConfiguredCommands(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		if strings.Contains(url, "getMyCommands") {
			return &ta.Response{Ok: true, Result: []byte("[]")}, nil
		}
		return successBoolResponse(), nil
	}}
	channel := newTestChannel(t, caller)
	channel.tgCfg.Ephemeral = config.TelegramEphemeralConfig{
		Mode:     config.TelegramEphemeralModeCommands,
		Commands: []string{"clear"},
	}

	err := channel.RegisterCommands(context.Background(), []commands.Definition{
		{Name: "clear", Description: "Clear synthetic history"},
		{Name: "help", Description: "Show synthetic help"},
	})
	require.NoError(t, err)
	require.Len(t, caller.calls, 2)
	assert.Contains(t, caller.calls[1].URL, "setMyCommands")

	var payload telego.SetMyCommandsParams
	require.NoError(t, json.Unmarshal(caller.calls[1].Data.BodyRaw, &payload))
	require.Len(t, payload.Commands, 2)
	assert.True(t, payload.Commands[0].IsEphemeral)
	assert.False(t, payload.Commands[1].IsEphemeral)
}

func TestTelegramEphemeralSend_IgnoresForgedReceiverMetadata(t *testing.T) {
	const verifiedUserID int64 = 42
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return successEphemeralResponse(t, -100123, 0, verifiedUserID, 7), nil
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100123, 0, verifiedUserID, 0, "")
	ctx := privateOutboundContext(target)

	ids, err := channel.Send(context.Background(), bus.OutboundMessage{
		ChatID:  ctx.ChatID,
		Context: ctx,
		Content: "synthetic private response",
	})
	require.NoError(t, err)
	require.Len(t, ids, 1)
	assert.True(t, strings.HasPrefix(ids[0], ephemeralMessageIDPrefix))

	var payload telego.SendMessageParams
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &payload))
	assert.Equal(t, verifiedUserID, payload.ReceiverUserID)
	assert.NotEqual(t, int64(999999), payload.ReceiverUserID)
}

func TestTelegramEphemeralPrivateChat_RemainsNormal(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return successResponseWithMessageID(t, 22), nil
	}}
	channel := newTestChannel(t, caller)
	channel.tgCfg.Ephemeral.Mode = config.TelegramEphemeralModeAll

	ids, err := channel.Send(context.Background(), bus.OutboundMessage{
		ChatID: "42",
		Context: bus.InboundContext{
			Channel:  "telegram",
			ChatID:   "42",
			ChatType: "direct",
			SenderID: "42",
		},
		Content: "synthetic private-chat response",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"22"}, ids)
	assert.NotContains(t, string(caller.calls[0].Data.BodyRaw), "receiver_user_id")
}

func TestTelegramEphemeralPrivateChat_InboundRemainsNormal(t *testing.T) {
	channel, messageBus := newEphemeralInboundChannel(t, config.TelegramEphemeralConfig{
		Mode: config.TelegramEphemeralModeAll,
	})
	message := &telego.Message{
		Text:      "synthetic direct request",
		MessageID: 23,
		Chat:      telego.Chat{ID: 45, Type: telego.ChatTypePrivate},
		From:      &telego.User{ID: 45},
	}

	require.NoError(t, channel.handleMessage(context.Background(), message))
	inbound := <-messageBus.InboundChan()
	assert.False(t, inbound.Context.PrivateResponse)
	assert.False(t, inbound.Context.PrivateSession)
	assert.Empty(t, inbound.Context.PrivateRouteToken)
	assert.Equal(t, "23", inbound.MessageID)
}

func TestTelegramEphemeralDisabled_DropsUnexpectedIncomingEphemeralMessage(t *testing.T) {
	channel, messageBus := newEphemeralInboundChannel(t, config.TelegramEphemeralConfig{})
	message := &telego.Message{
		Text:               "synthetic unexpected private request",
		MessageID:          0,
		EphemeralMessageID: 76,
		Chat:               telego.Chat{ID: -100126, Type: telego.ChatTypeSupergroup},
		From:               &telego.User{ID: 46},
	}

	require.NoError(t, channel.handleMessage(context.Background(), message))
	select {
	case inbound := <-messageBus.InboundChan():
		t.Fatalf("unexpected public processing of private update: %+v", inbound.Context)
	default:
	}
}

func TestTelegramEphemeralSend_PreservesForumAndEphemeralReply(t *testing.T) {
	const verifiedUserID int64 = 77
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return successEphemeralResponse(t, -100456, 9, verifiedUserID, 88), nil
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100456, 9, verifiedUserID, 66, "")
	ctx := privateOutboundContext(target)

	ids, err := channel.Send(context.Background(), bus.OutboundMessage{
		ChatID:  ctx.ChatID,
		Context: ctx,
		Content: "synthetic forum response",
	})
	require.NoError(t, err)
	require.Len(t, ids, 1)
	assert.NotContains(t, ids[0], inboundEphemeralIDPrefix)

	var payload telego.SendMessageParams
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &payload))
	assert.Equal(t, 9, payload.MessageThreadID)
	assert.Equal(t, verifiedUserID, payload.ReceiverUserID)
	require.NotNil(t, payload.ReplyParameters)
	assert.Equal(t, 66, payload.ReplyParameters.EphemeralMessageID)
	assert.Zero(t, payload.ReplyParameters.MessageID)
}

func TestTelegramEphemeralSend_RejectsMismatchedForumConfirmation(t *testing.T) {
	const verifiedUserID int64 = 78
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return successEphemeralResponse(t, -100457, 10, verifiedUserID, 89), nil
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100457, 9, verifiedUserID, 0, "")
	ctx := privateOutboundContext(target)

	_, err := channel.Send(context.Background(), bus.OutboundMessage{
		ChatID:  ctx.ChatID,
		Context: ctx,
		Content: "synthetic forum response",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, channels.ErrSendFailed)
	require.Len(t, caller.calls, 1)
}

func TestTelegramEphemeralCallback_PropagatesVerifiedCallbackID(t *testing.T) {
	channel, messageBus := newEphemeralInboundChannel(t, config.TelegramEphemeralConfig{
		Mode: config.TelegramEphemeralModeAll,
	})
	query := &telego.CallbackQuery{
		ID:   "synthetic-callback-id",
		From: telego.User{ID: 314},
		Data: "synthetic_callback_action",
		Message: &telego.Message{
			MessageID: 5,
			Chat:      telego.Chat{ID: -100789, Type: telego.ChatTypeSupergroup},
		},
	}
	require.NoError(t, channel.handleCallbackQuery(context.Background(), query))
	inbound := <-messageBus.InboundChan()
	require.True(t, inbound.Context.PrivateResponse)

	channel.ephemeralMu.Lock()
	target := channel.ephemeralRoutes[inbound.Context.PrivateRouteToken]
	channel.ephemeralMu.Unlock()
	assert.Equal(t, "synthetic-callback-id", target.CallbackQueryID)
	assert.Equal(t, int64(314), target.ReceiverUserID)
}

func TestTelegramEphemeralCallback_SendCarriesCallbackID(t *testing.T) {
	const verifiedUserID int64 = 314
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return successEphemeralResponse(t, -100789, 0, verifiedUserID, 91), nil
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(
		t, channel, -100789, 0, verifiedUserID, 0, "synthetic-callback-id",
	)
	ctx := privateOutboundContext(target)

	_, err := channel.Send(context.Background(), bus.OutboundMessage{
		ChatID:  ctx.ChatID,
		Context: ctx,
		Content: "synthetic callback response",
	})
	require.NoError(t, err)

	var payload telego.SendMessageParams
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &payload))
	assert.Equal(t, "synthetic-callback-id", payload.CallbackQueryID)
	assert.Equal(t, verifiedUserID, payload.ReceiverUserID)
}

func TestTelegramEphemeralEditAndDelete_UseDedicatedMethods(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return successBoolResponse(), nil
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100321, 0, 55, 0, "")
	messageID := encodeEphemeralMessageID(target.Token, 12)

	require.NoError(t, channel.EditMessage(context.Background(), "-100321", messageID, "synthetic edit"))
	require.NoError(t, channel.DeleteMessage(context.Background(), "-100321", messageID))
	require.Len(t, caller.calls, 2)
	assert.Contains(t, caller.calls[0].URL, "editEphemeralMessageText")
	assert.Contains(t, caller.calls[1].URL, "deleteEphemeralMessage")

	for _, call := range caller.calls {
		var payload struct {
			ReceiverUserID     int64 `json:"receiver_user_id"`
			EphemeralMessageID int   `json:"ephemeral_message_id"`
		}
		require.NoError(t, json.Unmarshal(call.Data.BodyRaw, &payload))
		assert.Equal(t, int64(55), payload.ReceiverUserID)
		assert.Equal(t, 12, payload.EphemeralMessageID)
	}
}

func TestTelegramEphemeralIncomingID_IsNeverUsedAsOutgoingDeleteID(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		t.Fatal("incoming ephemeral identifiers must not reach a delete API")
		return nil, nil
	}}
	channel := newTestChannel(t, caller)
	incomingID := encodeInboundEphemeralMessageID(13)

	err := channel.DeleteMessage(context.Background(), "-100322", incomingID)
	require.Error(t, err)
	assert.Empty(t, caller.calls)
}

func TestTelegramEphemeralAuxiliaryEdits_UseDedicatedMethods(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return successBoolResponse(), nil
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100654, 0, 73, 0, "")
	messageID := encodeEphemeralMessageID(target.Token, 18)
	markup := &telego.InlineKeyboardMarkup{}
	inputMedia := &telego.InputMediaPhoto{
		Type:  telego.MediaTypePhoto,
		Media: telego.InputFile{FileID: "synthetic-file-id"},
	}

	require.NoError(
		t,
		channel.EditEphemeralMessageMedia(
			context.Background(),
			"-100654",
			messageID,
			inputMedia,
			markup,
		),
	)
	require.NoError(
		t,
		channel.EditEphemeralMessageCaption(
			context.Background(),
			"-100654",
			messageID,
			"synthetic caption",
			markup,
		),
	)
	require.NoError(t, channel.EditEphemeralMessageReplyMarkup(context.Background(), "-100654", messageID, markup))
	require.Len(t, caller.calls, 3)
	assert.Contains(t, caller.calls[0].URL, "editEphemeralMessageMedia")
	assert.Contains(t, caller.calls[1].URL, "editEphemeralMessageCaption")
	assert.Contains(t, caller.calls[2].URL, "editEphemeralMessageReplyMarkup")
}

func TestTelegramEphemeralTextEdit_UnsupportedAPIReturnsSafeError(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return nil, &ta.Error{ErrorCode: 400, Description: "synthetic unsupported method"}
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100655, 0, 74, 0, "")
	messageID := encodeEphemeralMessageID(target.Token, 19)

	err := channel.EditMessage(context.Background(), "-100655", messageID, "synthetic private edit")
	require.Error(t, err)
	assert.ErrorIs(t, err, channels.ErrSendFailed)
	require.Len(t, caller.calls, 1)
	assert.Contains(t, caller.calls[0].URL, "editEphemeralMessageText")
	assert.NotContains(t, caller.calls[0].URL, "sendMessage")
}

func TestTelegramEphemeralTextEdit_AmbiguousNetworkErrorDoesNotSendFallback(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return nil, errors.New("synthetic connection reset by peer")
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100656, 0, 75, 0, "")
	messageID := encodeEphemeralMessageID(target.Token, 20)

	require.NoError(t, channel.EditMessage(
		context.Background(),
		"-100656",
		messageID,
		"synthetic private edit",
	))
	require.Len(t, caller.calls, 1)
	assert.Contains(t, caller.calls[0].URL, "editEphemeralMessageText")
}

func TestTelegramEphemeralMediaEdit_AmbiguousNetworkErrorDoesNotSurfaceFallbackSignal(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return nil, errors.New("synthetic connection reset by peer")
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100658, 0, 77, 0, "")
	messageID := encodeEphemeralMessageID(target.Token, 22)
	inputMedia := &telego.InputMediaPhoto{
		Type:  telego.MediaTypePhoto,
		Media: telego.InputFile{FileID: "synthetic-file-id"},
	}

	require.NoError(t, channel.EditEphemeralMessageMedia(
		context.Background(),
		"-100658",
		messageID,
		inputMedia,
		nil,
	))
	require.Len(t, caller.calls, 1)
	assert.Contains(t, caller.calls[0].URL, "editEphemeralMessageMedia")
}

func TestTelegramEphemeralCaptionEdit_ParseFallbackRemainsReceiverAware(t *testing.T) {
	callCount := 0
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		callCount++
		if callCount == 1 {
			return nil, &ta.Error{ErrorCode: 400, Description: "Bad Request: can't parse entities"}
		}
		return successBoolResponse(), nil
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100657, 0, 76, 0, "")
	messageID := encodeEphemeralMessageID(target.Token, 21)

	require.NoError(t, channel.EditEphemeralMessageCaption(
		context.Background(),
		"-100657",
		messageID,
		"**synthetic private caption**",
		nil,
	))
	require.Len(t, caller.calls, 2)
	for _, call := range caller.calls {
		assert.Contains(t, call.URL, "editEphemeralMessageCaption")
		var payload telego.EditEphemeralMessageCaptionParams
		require.NoError(t, json.Unmarshal(call.Data.BodyRaw, &payload))
		assert.Equal(t, int64(76), payload.ReceiverUserID)
		assert.Equal(t, 21, payload.EphemeralMessageID)
	}
}

func TestTelegramEphemeralUnsupportedAPI_FailsClosedWithoutPublicFallback(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return nil, errors.New("Bad Request: unsupported ephemeral parameter")
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100987, 0, 44, 0, "")
	ctx := privateOutboundContext(target)

	_, err := channel.Send(context.Background(), bus.OutboundMessage{
		ChatID:  ctx.ChatID,
		Context: ctx,
		Content: "synthetic private response",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, channels.ErrSendFailed)
	require.Len(t, caller.calls, 1)
	assert.Contains(t, caller.calls[0].URL, "sendMessage")
	assert.Contains(t, string(caller.calls[0].Data.BodyRaw), "receiver_user_id")
}

func TestTelegramEphemeralNetworkError_DoesNotRetry(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return nil, errors.New("synthetic network failure")
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100111, 0, 45, 0, "")
	ctx := privateOutboundContext(target)

	_, err := channel.Send(context.Background(), bus.OutboundMessage{
		ChatID:  ctx.ChatID,
		Context: ctx,
		Content: "synthetic private response",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, channels.ErrSendFailed)
	assert.Len(t, caller.calls, 1)
}

func TestTelegramEphemeralParseRejection_RetriesOnlyWithPrivatePlainText(t *testing.T) {
	const verifiedUserID int64 = 49
	callCount := 0
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("Bad Request: can't parse entities")
		}
		return successEphemeralResponse(t, -100112, 0, verifiedUserID, 35), nil
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100112, 0, verifiedUserID, 0, "")
	ctx := privateOutboundContext(target)

	_, err := channel.Send(context.Background(), bus.OutboundMessage{
		ChatID:  ctx.ChatID,
		Context: ctx,
		Content: "**synthetic private formatting**",
	})
	require.NoError(t, err)
	require.Len(t, caller.calls, 2)
	for _, call := range caller.calls {
		var payload telego.SendMessageParams
		require.NoError(t, json.Unmarshal(call.Data.BodyRaw, &payload))
		assert.Equal(t, verifiedUserID, payload.ReceiverUserID)
	}
}

func TestTelegramEphemeralServerIgnoringReceiver_FailsClosedWithoutRetry(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return successResponseWithMessageID(t, 99), nil
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100113, 0, 50, 0, "")
	ctx := privateOutboundContext(target)

	_, err := channel.Send(context.Background(), bus.OutboundMessage{
		ChatID:  ctx.ChatID,
		Context: ctx,
		Content: "synthetic private response",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, channels.ErrSendFailed)
	assert.Len(t, caller.calls, 1)
}

func TestTelegramEphemeralUnknownCapability_FailsBeforeAPICall(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		t.Fatal("Telegram API must not be called for an unverified route")
		return nil, nil
	}}
	channel := newTestChannel(t, caller)

	_, err := channel.Send(context.Background(), bus.OutboundMessage{
		ChatID: "-100222",
		Context: bus.InboundContext{
			PrivateResponse:   true,
			PrivateRouteToken: strings.Repeat("0", ephemeralRouteTokenBytes*2),
		},
		Content: "synthetic private response",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, channels.ErrSendFailed)
	assert.Empty(t, caller.calls)
}

func TestTelegramEphemeralTable_UsesReceiverAwarePreformattedPath(t *testing.T) {
	const verifiedUserID int64 = 46
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		assert.Contains(t, url, "sendMessage")
		assert.NotContains(t, url, "sendRichMessage")
		return successEphemeralResponse(t, -100333, 0, verifiedUserID, 33), nil
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100333, 0, verifiedUserID, 0, "")
	ctx := privateOutboundContext(target)

	_, err := channel.Send(context.Background(), bus.OutboundMessage{
		ChatID:  ctx.ChatID,
		Context: ctx,
		Content: testMarkdownTable,
	})
	require.NoError(t, err)
	require.Len(t, caller.calls, 1)
	var payload telego.SendMessageParams
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &payload))
	assert.Equal(t, verifiedUserID, payload.ReceiverUserID)
	assert.Contains(t, payload.Text, "<pre><code>")
}

func TestTelegramEphemeralToolFeedback_FinalizesWithPrivateEdit(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return successBoolResponse(), nil
	}}
	channel := newTestChannel(t, caller)
	target := mustRegisterEphemeralTarget(t, channel, -100334, 0, 51, 0, "")
	ctx := privateOutboundContext(target)
	messageID := encodeEphemeralMessageID(target.Token, 36)
	trackedKey := telegramToolFeedbackChatKey(ctx.ChatID, &ctx)
	channel.RecordToolFeedbackMessage(trackedKey, messageID, "synthetic progress")

	ids, handled := channel.FinalizeToolFeedbackMessage(context.Background(), bus.OutboundMessage{
		ChatID:  ctx.ChatID,
		Context: ctx,
		Content: testMarkdownTable,
	})
	assert.True(t, handled)
	assert.Equal(t, []string{messageID}, ids)
	require.Len(t, caller.calls, 1)
	assert.Contains(t, caller.calls[0].URL, "editEphemeralMessageText")
}

func TestTelegramEphemeralMedia_UsesReceiverAwareIndividualMethod(t *testing.T) {
	const verifiedUserID int64 = 47
	constructor := &multipartRecordingConstructor{}
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		assert.Contains(t, url, "sendPhoto")
		assert.NotContains(t, url, "sendMediaGroup")
		return successEphemeralResponse(t, -100444, 0, verifiedUserID, 34), nil
	}}
	channel := newTestChannelWithConstructor(t, caller, constructor)
	target := mustRegisterEphemeralTarget(t, channel, -100444, 0, verifiedUserID, 0, "")
	ctx := privateOutboundContext(target)

	store := media.NewFileMediaStore()
	channel.SetMediaStore(store)
	path := filepath.Join(t.TempDir(), "synthetic.png")
	require.NoError(t, os.WriteFile(path, []byte("synthetic-image"), 0o600))
	ref, err := store.Store(path, media.MediaMeta{Filename: "synthetic.png"}, "synthetic-scope")
	require.NoError(t, err)

	ids, err := channel.SendMedia(context.Background(), bus.OutboundMediaMessage{
		ChatID:  ctx.ChatID,
		Context: ctx,
		Parts:   []bus.MediaPart{{Type: "image", Ref: ref}},
	})
	require.NoError(t, err)
	require.Len(t, ids, 1)
	require.Len(t, constructor.calls, 1)
	assert.Equal(t, fmt.Sprintf("%d", verifiedUserID), constructor.calls[0].Parameters["receiver_user_id"])
}

func TestTelegramEphemeralStreaming_IsExplicitlyDisabledPerSession(t *testing.T) {
	channel := newTestChannel(t, &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		t.Fatal("streaming API must not be called")
		return nil, nil
	}})
	channel.tgCfg.Streaming.Enabled = true
	target := mustRegisterEphemeralTarget(t, channel, -100555, 0, 48, 0, "")
	inbound := privateOutboundContext(target)
	require.NoError(t, channel.BindPrivateRoute("synthetic-session", inbound))

	streamer, err := channel.BeginStreamForSession(context.Background(), inbound.ChatID, "synthetic-session")
	require.Error(t, err)
	assert.Nil(t, streamer)
}

func TestTelegramEphemeralBind_RejectsForgedSenderOrChannel(t *testing.T) {
	channel := newTestChannel(t, &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		t.Fatal("route binding must not call Telegram")
		return nil, nil
	}})
	target := mustRegisterEphemeralTarget(t, channel, -100556, 0, 48, 0, "")
	inbound := privateOutboundContext(target)
	inbound.SenderID = "999"
	require.Error(t, channel.BindPrivateRoute("synthetic-session", inbound))
	inbound.SenderID = "48"
	inbound.Channel = "discord"
	require.Error(t, channel.BindPrivateRoute("synthetic-session", inbound))
}
