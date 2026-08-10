package channels

import (
	"github.com/sipeed/picoclaw/pkg/bus"
	runtimeevents "github.com/sipeed/picoclaw/pkg/events"
)

func channelTypeForEvent(m *Manager, channelName string) string {
	if m == nil || m.config == nil {
		return channelName
	}
	if bc := m.config.Channels.Get(channelName); bc != nil && bc.Type != "" {
		return bc.Type
	}
	return channelName
}

func (m *Manager) publishChannelEvent(
	kind runtimeevents.Kind,
	channelName string,
	scope runtimeevents.Scope,
	severity runtimeevents.Severity,
	payload any,
) {
	if m == nil || m.runtimeEvents == nil {
		return
	}
	if scope.Channel == "" {
		scope.Channel = channelName
	}
	m.runtimeEvents.PublishNonBlocking(runtimeevents.Event{
		Kind:     kind,
		Source:   runtimeevents.Source{Component: "channel", Name: channelName},
		Scope:    scope,
		Severity: severity,
		Payload:  payload,
		Attrs:    channelEventAttrs(payload),
	})
}

func channelEventAttrs(payload any) map[string]any {
	switch payload := payload.(type) {
	case ChannelLifecyclePayload:
		attrs := map[string]any{}
		setAttrString(attrs, "type", payload.Type)
		setAttrString(attrs, "error", payload.Error)
		return attrs
	case ChannelOutboundPayload:
		attrs := map[string]any{}
		if payload.Media {
			attrs["media"] = payload.Media
		}
		if payload.ContentLen > 0 {
			attrs["content_len"] = payload.ContentLen
		}
		if len(payload.MessageIDs) > 0 {
			attrs["message_ids_count"] = len(payload.MessageIDs)
		}
		setAttrString(attrs, "reply_to_message_id", payload.ReplyToMessageID)
		setAttrString(attrs, "error", payload.Error)
		if payload.Retries > 0 {
			attrs["retries"] = payload.Retries
		}
		return attrs
	default:
		return nil
	}
}

func setAttrString(attrs map[string]any, key, value string) {
	if value != "" {
		attrs[key] = value
	}
}

func (m *Manager) publishOutboundSent(
	channelName string,
	msg bus.OutboundMessage,
	messageIDs []string,
) {
	private := outboundMessageIsPrivate(msg)
	payload := ChannelOutboundPayload{
		ContentLen: len([]rune(msg.Content)),
	}
	if !private {
		payload.MessageIDs = append([]string(nil), messageIDs...)
		payload.ReplyToMessageID = msg.ReplyToMessageID
	}
	m.publishChannelEvent(
		runtimeevents.KindChannelMessageOutboundSent,
		channelName,
		scopeFromOutboundMessage(msg),
		runtimeevents.SeverityInfo,
		payload,
	)
}

func (m *Manager) publishOutboundQueued(
	channelName string,
	msg bus.OutboundMessage,
) {
	payload := ChannelOutboundPayload{ContentLen: len([]rune(msg.Content))}
	if !outboundMessageIsPrivate(msg) {
		payload.ReplyToMessageID = msg.ReplyToMessageID
	}
	m.publishChannelEvent(
		runtimeevents.KindChannelMessageOutboundQueued,
		channelName,
		scopeFromOutboundMessage(msg),
		runtimeevents.SeverityInfo,
		payload,
	)
}

func (m *Manager) publishOutboundFailed(
	channelName string,
	msg bus.OutboundMessage,
	err error,
	media bool,
) {
	private := outboundMessageIsPrivate(msg)
	payload := ChannelOutboundPayload{
		Media:      media,
		ContentLen: len([]rune(msg.Content)),
	}
	if private {
		payload.Error = "private delivery failed"
	} else {
		payload.ReplyToMessageID = msg.ReplyToMessageID
		payload.Retries = maxRetries
	}
	if err != nil && !private {
		payload.Error = err.Error()
	}
	m.publishChannelEvent(
		runtimeevents.KindChannelMessageOutboundFailed,
		channelName,
		scopeFromOutboundMessage(msg),
		runtimeevents.SeverityError,
		payload,
	)
}

func (m *Manager) publishOutboundMediaSent(
	channelName string,
	msg bus.OutboundMediaMessage,
	messageIDs []string,
) {
	private := outboundMediaMessageIsPrivate(msg)
	payload := ChannelOutboundPayload{Media: true}
	if !private {
		payload.MessageIDs = append([]string(nil), messageIDs...)
	}
	m.publishChannelEvent(
		runtimeevents.KindChannelMessageOutboundSent,
		channelName,
		scopeFromOutboundMediaMessage(msg),
		runtimeevents.SeverityInfo,
		payload,
	)
}

func (m *Manager) publishOutboundMediaQueued(
	channelName string,
	msg bus.OutboundMediaMessage,
) {
	m.publishChannelEvent(
		runtimeevents.KindChannelMessageOutboundQueued,
		channelName,
		scopeFromOutboundMediaMessage(msg),
		runtimeevents.SeverityInfo,
		ChannelOutboundPayload{Media: true},
	)
}

func (m *Manager) publishOutboundMediaFailed(
	channelName string,
	msg bus.OutboundMediaMessage,
	err error,
) {
	private := outboundMediaMessageIsPrivate(msg)
	payload := ChannelOutboundPayload{Media: true}
	if private {
		payload.Error = "private delivery failed"
	} else {
		payload.Retries = maxRetries
	}
	if err != nil && !private {
		payload.Error = err.Error()
	}
	m.publishChannelEvent(
		runtimeevents.KindChannelMessageOutboundFailed,
		channelName,
		scopeFromOutboundMediaMessage(msg),
		runtimeevents.SeverityError,
		payload,
	)
}

func scopeFromOutboundContext(ctx bus.InboundContext) runtimeevents.Scope {
	if ctx.PrivateResponse {
		// Omit user/chat/message identifiers and opaque route-bearing IDs from
		// telemetry for private interactions.
		return runtimeevents.Scope{
			Channel: ctx.Channel,
			Account: ctx.Account,
		}
	}
	return runtimeevents.Scope{
		Channel:   ctx.Channel,
		Account:   ctx.Account,
		ChatID:    ctx.ChatID,
		TopicID:   ctx.TopicID,
		SpaceID:   ctx.SpaceID,
		SpaceType: ctx.SpaceType,
		ChatType:  ctx.ChatType,
		SenderID:  ctx.SenderID,
		MessageID: ctx.MessageID,
	}
}

func scopeFromOutboundMessage(msg bus.OutboundMessage) runtimeevents.Scope {
	if outboundMessageIsPrivate(msg) && !msg.Context.PrivateResponse {
		ctx := msg.Context
		ctx.PrivateResponse = true
		return scopeFromOutboundContext(ctx)
	}
	return scopeFromOutboundContext(msg.Context)
}

func scopeFromOutboundMediaMessage(msg bus.OutboundMediaMessage) runtimeevents.Scope {
	if outboundMediaMessageIsPrivate(msg) && !msg.Context.PrivateResponse {
		ctx := msg.Context
		ctx.PrivateResponse = true
		return scopeFromOutboundContext(ctx)
	}
	return scopeFromOutboundContext(msg.Context)
}

func outboundMessageIsPrivate(msg bus.OutboundMessage) bool {
	return msg.Context.PrivateResponse || (msg.Scope != nil && msg.Scope.PrivateResponse)
}

func outboundMediaMessageIsPrivate(msg bus.OutboundMediaMessage) bool {
	return msg.Context.PrivateResponse || (msg.Scope != nil && msg.Scope.PrivateResponse)
}
