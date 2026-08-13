package telegram

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/channels"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/logger"
)

const (
	ephemeralRouteTTL        = time.Hour
	ephemeralMessageIDPrefix = "ephemeral:"
	inboundEphemeralIDPrefix = "ephemeral-inbound:"
	callbackInboundIDPrefix  = "callback:"
	ephemeralRouteTokenBytes = 16
)

type telegramEphemeralTarget struct {
	Token                      string
	ChatID                     int64
	ThreadID                   int
	ReceiverUserID             int64
	CallbackQueryID            string
	IncomingEphemeralMessageID int
	IncomingReceiverUserID     int64
	IncomingReceiverIsBot      bool
	CreatedAt                  time.Time
}

type telegramPrivateInboundPlan struct {
	enabled           bool
	callbackQueryID   string
	callback          bool
	incomingEphemeral bool
}

func (c *TelegramChannel) ephemeralConfig() config.TelegramEphemeralConfig {
	if c == nil || c.tgCfg == nil {
		return config.TelegramEphemeralConfig{}
	}
	return c.tgCfg.Ephemeral
}

func (c *TelegramChannel) planPrivateMessage(message *telego.Message) (telegramPrivateInboundPlan, error) {
	if message == nil || !telegramSupportsEphemeralChat(message.Chat.Type) {
		return telegramPrivateInboundPlan{}, nil
	}

	ephemeralCfg := c.ephemeralConfig()
	incomingEphemeral := message.EphemeralMessageID > 0
	eligible := incomingEphemeral && ephemeralCfg.Enabled()
	switch ephemeralCfg.EffectiveMode() {
	case config.TelegramEphemeralModeAll:
		eligible = true
	case config.TelegramEphemeralModeCommands:
		if !eligible {
			command, ok := telegramMessageCommand(message)
			if !ok {
				break
			}
			eligible = ephemeralCfg.IsCommandEphemeral(command)
		}
	}

	// An incoming ephemeral message is already private. Processing it without
	// an enabled matching policy could expose its contents through a public
	// response, so stale or unsupported command configurations are dropped.
	if incomingEphemeral && !eligible {
		return telegramPrivateInboundPlan{}, fmt.Errorf("incoming ephemeral message is not enabled by policy")
	}
	return telegramPrivateInboundPlan{
		enabled:           eligible,
		incomingEphemeral: incomingEphemeral,
	}, nil
}

func (c *TelegramChannel) planPrivateCallback(query *telego.CallbackQuery) (telegramPrivateInboundPlan, error) {
	if query == nil || query.Message == nil {
		return telegramPrivateInboundPlan{}, nil
	}
	message := query.Message.Message()
	if message == nil || !telegramSupportsEphemeralChat(message.Chat.Type) {
		return telegramPrivateInboundPlan{}, nil
	}

	ephemeralCfg := c.ephemeralConfig()
	incomingEphemeral := message.EphemeralMessageID > 0
	eligible := incomingEphemeral && ephemeralCfg.Enabled()
	switch ephemeralCfg.EffectiveMode() {
	case config.TelegramEphemeralModeAll:
		eligible = true
	case config.TelegramEphemeralModeCommands:
		if !eligible {
			command, ok := commands.CommandName(strings.TrimSpace(query.Data))
			if !ok {
				break
			}
			eligible = ephemeralCfg.IsCommandEphemeral(command)
		}
	}
	if incomingEphemeral && !eligible {
		return telegramPrivateInboundPlan{}, fmt.Errorf("ephemeral callback is not enabled by policy")
	}
	if !eligible {
		return telegramPrivateInboundPlan{}, nil
	}
	if strings.TrimSpace(query.ID) == "" {
		return telegramPrivateInboundPlan{}, fmt.Errorf("callback query ID is empty")
	}
	return telegramPrivateInboundPlan{
		enabled:           true,
		callbackQueryID:   query.ID,
		callback:          true,
		incomingEphemeral: incomingEphemeral,
	}, nil
}

func telegramSupportsEphemeralChat(chatType string) bool {
	return chatType == telego.ChatTypeGroup || chatType == telego.ChatTypeSupergroup
}

func telegramMessageCommand(message *telego.Message) (string, bool) {
	text, entities := telegramEntityTextAndList(message)
	if text == "" {
		return "", false
	}
	runes := []rune(text)
	for _, entity := range entities {
		if entity.Type != telego.EntityTypeBotCommand || entity.Offset != 0 {
			continue
		}
		entityText, ok := telegramEntityText(runes, entity)
		if !ok {
			continue
		}
		return commands.CommandName(entityText)
	}
	return "", false
}

func (c *TelegramChannel) registerEphemeralTarget(
	message *telego.Message,
	plan telegramPrivateInboundPlan,
) (telegramEphemeralTarget, error) {
	if message == nil || message.From == nil {
		return telegramEphemeralTarget{}, fmt.Errorf("verified sender is missing")
	}
	if message.From.ID <= 0 || message.From.IsBot {
		return telegramEphemeralTarget{}, fmt.Errorf("ephemeral receiver must be a non-bot user")
	}
	if !telegramSupportsEphemeralChat(message.Chat.Type) {
		return telegramEphemeralTarget{}, fmt.Errorf("ephemeral target is not a group chat")
	}

	token, err := newEphemeralRouteToken()
	if err != nil {
		return telegramEphemeralTarget{}, err
	}
	target := telegramEphemeralTarget{
		Token:                      token,
		ChatID:                     message.Chat.ID,
		ThreadID:                   message.MessageThreadID,
		ReceiverUserID:             message.From.ID,
		CallbackQueryID:            plan.callbackQueryID,
		IncomingEphemeralMessageID: message.EphemeralMessageID,
		CreatedAt:                  time.Now(),
	}
	if message.ReceiverUser != nil {
		target.IncomingReceiverUserID = message.ReceiverUser.ID
		target.IncomingReceiverIsBot = message.ReceiverUser.IsBot
	}

	c.ephemeralMu.Lock()
	c.ensureEphemeralMapsLocked()
	c.pruneEphemeralRoutesLocked(target.CreatedAt)
	c.ephemeralRoutes[token] = target
	c.ephemeralMu.Unlock()
	return target, nil
}

func newEphemeralRouteToken() (string, error) {
	buf := make([]byte, ephemeralRouteTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate private route token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func (c *TelegramChannel) ensureEphemeralMapsLocked() {
	if c.ephemeralRoutes == nil {
		c.ephemeralRoutes = make(map[string]telegramEphemeralTarget)
	}
	if c.ephemeralSessions == nil {
		c.ephemeralSessions = make(map[string]string)
	}
}

func (c *TelegramChannel) pruneEphemeralRoutesLocked(now time.Time) {
	for token, target := range c.ephemeralRoutes {
		if now.Sub(target.CreatedAt) <= ephemeralRouteTTL {
			continue
		}
		delete(c.ephemeralRoutes, token)
		for sessionKey, sessionToken := range c.ephemeralSessions {
			if sessionToken == token {
				delete(c.ephemeralSessions, sessionKey)
			}
		}
	}
}

// BindPrivateRoute implements channels.PrivateRouteBinder. Only an opaque
// capability previously issued from a verified Telegram update can be bound.
func (c *TelegramChannel) BindPrivateRoute(sessionKey string, inbound bus.InboundContext) error {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || !inbound.PrivateResponse || inbound.PrivateRouteToken == "" {
		return fmt.Errorf("private route binding is incomplete")
	}
	if !strings.EqualFold(strings.TrimSpace(inbound.Channel), c.Name()) {
		return fmt.Errorf("private route channel mismatch")
	}
	verifiedSenderID, err := strconv.ParseInt(strings.TrimSpace(inbound.SenderID), 10, 64)
	if err != nil || verifiedSenderID <= 0 {
		return fmt.Errorf("private route sender is invalid")
	}

	chatID, threadID, err := resolveTelegramOutboundTarget(inbound.ChatID, &inbound)
	if err != nil {
		return fmt.Errorf("private route target is invalid")
	}

	c.ephemeralMu.Lock()
	defer c.ephemeralMu.Unlock()
	c.ensureEphemeralMapsLocked()
	c.pruneEphemeralRoutesLocked(time.Now())
	target, ok := c.ephemeralRoutes[inbound.PrivateRouteToken]
	if !ok || target.ChatID != chatID || target.ThreadID != threadID ||
		target.ReceiverUserID != verifiedSenderID {
		return fmt.Errorf("private route capability was not issued for this target")
	}
	c.ephemeralSessions[sessionKey] = inbound.PrivateRouteToken
	return nil
}

func (c *TelegramChannel) resolveEphemeralTarget(
	inbound bus.InboundContext,
	scope *bus.OutboundScope,
	sessionKey string,
	chatID int64,
	threadID int,
) (*telegramEphemeralTarget, error) {
	privateRequested := inbound.PrivateResponse
	token := strings.TrimSpace(inbound.PrivateRouteToken)
	if scope != nil && scope.PrivateResponse {
		privateRequested = true
		if token == "" {
			token = strings.TrimSpace(scope.PrivateRouteToken)
		}
	}

	c.ephemeralMu.Lock()
	defer c.ephemeralMu.Unlock()
	c.ensureEphemeralMapsLocked()
	c.pruneEphemeralRoutesLocked(time.Now())
	if sessionToken := c.ephemeralSessions[strings.TrimSpace(sessionKey)]; sessionToken != "" {
		privateRequested = true
		// The explicit capability from the current verified turn always wins.
		// Replacing it with the latest session binding could route an older
		// concurrent response through the wrong callback or client instance.
		if token == "" {
			token = sessionToken
		}
	}
	if !privateRequested {
		return nil, nil
	}
	if token == "" {
		return nil, fmt.Errorf("private delivery capability is missing")
	}
	target, ok := c.ephemeralRoutes[token]
	if !ok {
		return nil, fmt.Errorf("private delivery capability is unknown or expired")
	}
	if target.ChatID != chatID || target.ThreadID != threadID {
		return nil, fmt.Errorf("private delivery target mismatch")
	}
	targetCopy := target
	return &targetCopy, nil
}

func (c *TelegramChannel) sessionHasEphemeralRoute(sessionKey string) bool {
	c.ephemeralMu.Lock()
	defer c.ephemeralMu.Unlock()
	c.ensureEphemeralMapsLocked()
	c.pruneEphemeralRoutesLocked(time.Now())
	token := c.ephemeralSessions[strings.TrimSpace(sessionKey)]
	if token == "" {
		return false
	}
	_, ok := c.ephemeralRoutes[token]
	return ok
}

// IsPrivateSession implements channels.PrivateSessionCapable.
func (c *TelegramChannel) IsPrivateSession(sessionKey string) bool {
	return c.sessionHasEphemeralRoute(sessionKey)
}

func ephemeralDeliveryError(operation string, _ error) error {
	return fmt.Errorf("telegram ephemeral %s failed: %w", operation, channels.ErrSendFailed)
}

func encodeEphemeralMessageID(token string, messageID int) string {
	return ephemeralMessageIDPrefix + token + ":" + strconv.Itoa(messageID)
}

func encodeInboundEphemeralMessageID(messageID int) string {
	return inboundEphemeralIDPrefix + strconv.Itoa(messageID)
}

func parseEphemeralMessageID(messageID string) (token string, id int, ok bool) {
	if !strings.HasPrefix(messageID, ephemeralMessageIDPrefix) ||
		strings.HasPrefix(messageID, inboundEphemeralIDPrefix) {
		return "", 0, false
	}
	parts := strings.Split(strings.TrimPrefix(messageID, ephemeralMessageIDPrefix), ":")
	if len(parts) != 2 || len(parts[0]) != ephemeralRouteTokenBytes*2 {
		return "", 0, false
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return "", 0, false
	}
	parsed, err := strconv.Atoi(parts[1])
	if err != nil || parsed <= 0 {
		return "", 0, false
	}
	return parts[0], parsed, true
}

func (c *TelegramChannel) resolveEphemeralMessageReference(
	chatID string,
	messageID string,
) (telegramEphemeralTarget, int, bool, error) {
	token, ephemeralID, encoded := parseEphemeralMessageID(messageID)
	if !encoded {
		return telegramEphemeralTarget{}, 0, false, nil
	}
	// Tool-feedback tracking appends an opaque private suffix to the chat key
	// so users in one group cannot share animator state. Strip that suffix only
	// at the final platform call; it is never accepted as a receiver selector.
	chatID = stripPrivateTrackingSuffix(chatID)
	cid, threadID, err := parseTelegramChatID(chatID)
	if err != nil {
		return telegramEphemeralTarget{}, 0, true, err
	}

	c.ephemeralMu.Lock()
	defer c.ephemeralMu.Unlock()
	c.ensureEphemeralMapsLocked()
	c.pruneEphemeralRoutesLocked(time.Now())
	target, ok := c.ephemeralRoutes[token]
	if !ok || target.ChatID != cid || target.ThreadID != threadID {
		return telegramEphemeralTarget{}, 0, true, fmt.Errorf("ephemeral message capability is invalid")
	}
	return target, ephemeralID, true, nil
}

func stripPrivateTrackingSuffix(chatID string) string {
	const marker = ":private:"
	idx := strings.LastIndex(chatID, marker)
	if idx <= 0 {
		return chatID
	}
	token := chatID[idx+len(marker):]
	if len(token) != ephemeralRouteTokenBytes*2 {
		return chatID
	}
	if _, err := hex.DecodeString(token); err != nil {
		return chatID
	}
	return chatID[:idx]
}

func ephemeralReplyParameters(target *telegramEphemeralTarget, replyToID string) *telego.ReplyParameters {
	if target != nil && target.CallbackQueryID == "" && target.IncomingEphemeralMessageID > 0 {
		return &telego.ReplyParameters{EphemeralMessageID: target.IncomingEphemeralMessageID}
	}
	if replyToID == "" {
		return nil
	}
	mid, err := strconv.Atoi(replyToID)
	if err != nil || mid <= 0 {
		return nil
	}
	return &telego.ReplyParameters{MessageID: mid}
}

func applyEphemeralSendMessage(
	params *telego.SendMessageParams,
	target *telegramEphemeralTarget,
	replyToID string,
) {
	params.ReplyParameters = ephemeralReplyParameters(target, replyToID)
	if target == nil {
		return
	}
	params.ReceiverUserID = target.ReceiverUserID
	params.CallbackQueryID = target.CallbackQueryID
}

func ephemeralReceiverUserID(target *telegramEphemeralTarget) int64 {
	if target == nil {
		return 0
	}
	return target.ReceiverUserID
}

func ephemeralCallbackQueryID(target *telegramEphemeralTarget) string {
	if target == nil {
		return ""
	}
	return target.CallbackQueryID
}

func validateEphemeralSendResult(
	message *telego.Message,
	target *telegramEphemeralTarget,
) (string, error) {
	if target == nil {
		if message == nil {
			return "", fmt.Errorf("telegram returned an empty message")
		}
		return strconv.Itoa(message.MessageID), nil
	}
	if message == nil || message.EphemeralMessageID <= 0 || message.MessageID != 0 ||
		message.Chat.ID != target.ChatID || message.MessageThreadID != target.ThreadID ||
		message.ReceiverUser == nil || message.ReceiverUser.ID != target.ReceiverUserID ||
		message.ReceiverUser.IsBot {
		return "", ephemeralDeliveryError("confirmation", fmt.Errorf("server did not confirm private delivery"))
	}
	return encodeEphemeralMessageID(target.Token, message.EphemeralMessageID), nil
}

func (c *TelegramChannel) editEphemeralMessageText(
	ctx context.Context,
	chatID, messageID, content string,
) error {
	target, ephemeralID, encoded, err := c.resolveEphemeralMessageReference(chatID, messageID)
	if err != nil || !encoded {
		return ephemeralDeliveryError("text edit", err)
	}

	plainFallback := content
	if hasTelegramRichTable(content) {
		content = telegramTableFallbackMarkdown(content)
		plainFallback = telegramTableFallbackPlainText(plainFallback)
	}
	params := &telego.EditEphemeralMessageTextParams{
		ChatID:             tu.ID(target.ChatID),
		ReceiverUserID:     target.ReceiverUserID,
		EphemeralMessageID: ephemeralID,
		Text:               parseContent(content, c.tgCfg.UseMarkdownV2),
	}
	if c.tgCfg.UseMarkdownV2 {
		params.ParseMode = telego.ModeMarkdownV2
	} else {
		params.ParseMode = telego.ModeHTML
	}

	err = c.bot.EditEphemeralMessageText(ctx, params)
	if err == nil || strings.Contains(strings.ToLower(errorString(err)), "message is not modified") {
		return nil
	}
	if isTelegramParseRejection(err) {
		params.Text = plainFallback
		params.ParseMode = ""
		err = c.bot.EditEphemeralMessageText(ctx, params)
		if err == nil || strings.Contains(strings.ToLower(errorString(err)), "message is not modified") {
			return nil
		}
	}
	if isTelegramDefinitiveAPIRejection(err) {
		return ephemeralDeliveryError("text edit", err)
	}

	// The result of an edit can be ambiguous after a network failure. Returning
	// success prevents placeholder/tool-feedback machinery from sending the same
	// private content as a second message.
	logger.WarnCF("telegram", "Ephemeral message edit result is unavailable", map[string]any{
		"operation": "editEphemeralMessageText",
	})
	return nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// isTelegramDefinitiveAPIRejection distinguishes an explicit Telegram API
// response from a transport failure whose delivery result may be ambiguous.
// Only definitive rejections are safe to surface from edit paths: callers may
// otherwise replace a failed edit with a second private message.
func isTelegramDefinitiveAPIRejection(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *ta.Error
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode >= 400 && apiErr.ErrorCode < 500
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "bad request") ||
		strings.Contains(message, "forbidden") ||
		strings.Contains(message, "unauthorized") ||
		strings.Contains(message, "method not found")
}

func handleEphemeralEditResult(operation string, err error) error {
	if err == nil {
		return nil
	}
	if isTelegramDefinitiveAPIRejection(err) {
		return ephemeralDeliveryError(operation, err)
	}

	// A transport failure after an edit request is ambiguous. Treat it as
	// delivered so higher-level placeholder logic cannot publish a duplicate.
	logger.WarnCF("telegram", "Ephemeral edit result is unavailable", map[string]any{
		"operation": operation,
	})
	return nil
}

// EditEphemeralMessageMedia exposes the Bot API 10.2 media-edit method for
// callers that already hold a channel-issued encoded ephemeral message ID.
func (c *TelegramChannel) EditEphemeralMessageMedia(
	ctx context.Context,
	chatID, messageID string,
	media telego.InputMedia,
	replyMarkup *telego.InlineKeyboardMarkup,
) error {
	target, ephemeralID, encoded, err := c.resolveEphemeralMessageReference(chatID, messageID)
	if err != nil || !encoded {
		return ephemeralDeliveryError("media edit", err)
	}
	err = c.bot.EditEphemeralMessageMedia(ctx, &telego.EditEphemeralMessageMediaParams{
		ChatID:             tu.ID(target.ChatID),
		ReceiverUserID:     target.ReceiverUserID,
		EphemeralMessageID: ephemeralID,
		Media:              media,
		ReplyMarkup:        replyMarkup,
	})
	return handleEphemeralEditResult("media edit", err)
}

// EditEphemeralMessageCaption exposes the Bot API 10.2 caption-edit method for
// a channel-issued encoded ephemeral message ID.
func (c *TelegramChannel) EditEphemeralMessageCaption(
	ctx context.Context,
	chatID, messageID, caption string,
	replyMarkup *telego.InlineKeyboardMarkup,
) error {
	target, ephemeralID, encoded, err := c.resolveEphemeralMessageReference(chatID, messageID)
	if err != nil || !encoded {
		return ephemeralDeliveryError("caption edit", err)
	}
	plainFallback := caption
	if hasTelegramRichTable(caption) {
		caption = telegramTableFallbackMarkdown(caption)
		plainFallback = telegramTableFallbackPlainText(plainFallback)
	}
	params := &telego.EditEphemeralMessageCaptionParams{
		ChatID:             tu.ID(target.ChatID),
		ReceiverUserID:     target.ReceiverUserID,
		EphemeralMessageID: ephemeralID,
		Caption:            parseContent(caption, c.tgCfg.UseMarkdownV2),
		ReplyMarkup:        replyMarkup,
	}
	if c.tgCfg.UseMarkdownV2 {
		params.ParseMode = telego.ModeMarkdownV2
	} else {
		params.ParseMode = telego.ModeHTML
	}
	err = c.bot.EditEphemeralMessageCaption(ctx, params)
	if err == nil || strings.Contains(strings.ToLower(errorString(err)), "message is not modified") {
		return nil
	}
	if isTelegramParseRejection(err) {
		params.Caption = plainFallback
		params.ParseMode = ""
		err = c.bot.EditEphemeralMessageCaption(ctx, params)
		if err == nil || strings.Contains(strings.ToLower(errorString(err)), "message is not modified") {
			return nil
		}
	}
	if isTelegramDefinitiveAPIRejection(err) {
		return ephemeralDeliveryError("caption edit", err)
	}
	logger.WarnCF("telegram", "Ephemeral caption edit result is unavailable", map[string]any{
		"operation": "editEphemeralMessageCaption",
	})
	return nil
}

// EditEphemeralMessageReplyMarkup exposes the Bot API 10.2 reply-markup edit
// method for a channel-issued encoded ephemeral message ID.
func (c *TelegramChannel) EditEphemeralMessageReplyMarkup(
	ctx context.Context,
	chatID, messageID string,
	replyMarkup *telego.InlineKeyboardMarkup,
) error {
	target, ephemeralID, encoded, err := c.resolveEphemeralMessageReference(chatID, messageID)
	if err != nil || !encoded {
		return ephemeralDeliveryError("reply-markup edit", err)
	}
	err = c.bot.EditEphemeralMessageReplyMarkup(ctx, &telego.EditEphemeralMessageReplyMarkupParams{
		ChatID:             tu.ID(target.ChatID),
		ReceiverUserID:     target.ReceiverUserID,
		EphemeralMessageID: ephemeralID,
		ReplyMarkup:        replyMarkup,
	})
	return handleEphemeralEditResult("reply-markup edit", err)
}

func (c *TelegramChannel) handleCallbackQuery(ctx context.Context, query *telego.CallbackQuery) error {
	plan, err := c.planPrivateCallback(query)
	if err != nil {
		c.logPrivateInboundDrop(err.Error())
		return nil
	}
	if !plan.enabled {
		return nil
	}
	message := query.Message.Message()
	if message == nil || strings.TrimSpace(query.Data) == "" {
		c.logPrivateInboundDrop("callback message or data is unavailable")
		return nil
	}

	callbackMessage := *message
	callbackMessage.From = &query.From
	callbackMessage.Text = strings.TrimSpace(query.Data)
	callbackMessage.Entities = nil
	callbackMessage.Caption = ""
	callbackMessage.CaptionEntities = nil
	callbackMessage.MediaGroupID = ""
	callbackMessage.Photo = nil
	callbackMessage.Voice = nil
	callbackMessage.Audio = nil
	callbackMessage.Document = nil
	callbackMessage.Location = nil
	callbackMessage.ReplyToMessage = nil

	return c.handleMessagesWithPrivatePlan(ctx, []*telego.Message{&callbackMessage}, plan)
}

func (c *TelegramChannel) logPrivateInboundDrop(reason string) {
	logger.WarnCF("telegram", "Private inbound update was dropped", map[string]any{
		"reason": reason,
	})
}
