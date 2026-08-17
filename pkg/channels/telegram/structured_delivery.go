package telegram

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/channels"
	"github.com/As-tsaqib/picoclaw/pkg/logger"
)

const (
	sessionCallbackPrefix = "pcsm:"
	sessionMenuTTL        = 15 * time.Minute
	richMessageMaxBytes   = 32768
	richMessageMaxBlocks  = 500
	richMessageMaxColumns = 20
)

type telegramSessionMenu struct {
	token          string
	menu           bus.InteractionMenu
	chatID         int64
	threadID       int
	messageID      int
	ephemeralID    int
	receiverUserID int64
	createdAt      time.Time
}

func (c *TelegramChannel) SupportsStructuredContent() bool { return true }

func (c *TelegramChannel) SetInternalCallbackHandler(handler bus.InternalCallbackHandler) {
	c.internalCallbackMu.Lock()
	c.internalCallbackHandler = handler
	c.internalCallbackMu.Unlock()
}

func (c *TelegramChannel) currentInternalCallbackHandler() bus.InternalCallbackHandler {
	c.internalCallbackMu.RLock()
	defer c.internalCallbackMu.RUnlock()
	return c.internalCallbackHandler
}

func (c *TelegramChannel) sendStructuredContent(
	ctx context.Context,
	msg bus.OutboundMessage,
	chatID int64,
	threadID int,
	ephemeral *telegramEphemeralTarget,
) ([]string, error) {
	content := msg.Structured.Clone()
	if content == nil {
		return nil, nil
	}
	fallback := strings.TrimSpace(content.FallbackText())
	if fallback == "" {
		fallback = strings.TrimSpace(msg.Content)
	}
	markup, pending, err := c.structuredReplyMarkup(content, chatID, threadID)
	if err != nil {
		logger.WarnCF("telegram", "Interactive structured response was rejected", map[string]any{"error": err.Error()})
		markup = structuredKeyboardMarkup(content.Keyboard)
		pending = nil
	}

	if ephemeral == nil {
		if rich, ok := buildNativeRichMessage(content); ok {
			params := &telego.SendRichMessageParams{
				ChatID:          tu.ID(chatID),
				MessageThreadID: threadID,
				RichMessage:     rich,
				ReplyParameters: ephemeralReplyParameters(nil, msg.ReplyToMessageID),
			}
			if markup != nil {
				params.ReplyMarkup = markup
			}
			pMsg, richErr := c.bot.SendRichMessage(ctx, params)
			if richErr == nil {
				if pending != nil {
					pending.messageID = pMsg.MessageID
					c.storeSessionMenu(*pending)
				}
				return []string{strconv.Itoa(pMsg.MessageID)}, nil
			}
			logger.WarnCF("telegram", "Native structured send failed, using text fallback", map[string]any{
				"chat_id": chatID,
				"error":   richErr.Error(),
			})
		}
	}

	return c.sendStructuredFallback(ctx, chatID, threadID, msg.ReplyToMessageID, fallback, markup, pending, ephemeral)
}

func buildNativeRichMessage(content *bus.StructuredContent) (telego.InputRichMessage, bool) {
	if content == nil {
		return telego.InputRichMessage{}, false
	}
	blocks := make([]telego.InputRichBlock, 0, len(content.Paragraphs)+len(content.Tables)+1)
	totalBytes := 0
	blockUnits := 0
	if title := strings.TrimSpace(content.Title); title != "" {
		totalBytes += len([]byte(title))
		blocks = append(blocks, tu.RichBlockSectionHeading(tu.RichTextPlain(title), 3))
		blockUnits++
	}
	for _, paragraph := range content.Paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		totalBytes += len([]byte(paragraph))
		blocks = append(blocks, tu.RichBlockParagraph(tu.RichTextPlain(paragraph)))
		blockUnits++
	}
	for _, table := range content.Tables {
		if len(table.Columns) == 0 || len(table.Columns) > richMessageMaxColumns {
			return telego.InputRichMessage{}, false
		}
		cells := make([][]telego.RichBlockTableCell, 0, len(table.Rows)+1)
		if table.Header {
			header := make([]telego.RichBlockTableCell, len(table.Columns))
			for i, value := range table.Columns {
				totalBytes += len([]byte(value))
				header[i] = nativeTableCell(value, true)
			}
			cells = append(cells, header)
		}
		for _, row := range table.Rows {
			if len(row) > len(table.Columns) {
				return telego.InputRichMessage{}, false
			}
			nativeRow := make([]telego.RichBlockTableCell, len(table.Columns))
			for i := range table.Columns {
				value := ""
				if i < len(row) {
					value = row[i]
				}
				totalBytes += len([]byte(value))
				nativeRow[i] = nativeTableCell(value, false)
			}
			cells = append(cells, nativeRow)
		}
		block := tu.RichBlockTableGrid(cells)
		block.IsBordered = table.Border
		block.IsStriped = table.Striped
		if caption := strings.TrimSpace(table.Caption); caption != "" {
			totalBytes += len([]byte(caption))
			block.Caption = tu.RichTextPlain(caption)
		}
		blocks = append(blocks, block)
		blockUnits += 1 + len(cells)
	}
	if len(blocks) == 0 || totalBytes > richMessageMaxBytes || blockUnits > richMessageMaxBlocks {
		return telego.InputRichMessage{}, false
	}
	return tu.RichMessage(blocks...), true
}

func nativeTableCell(value string, header bool) telego.RichBlockTableCell {
	return telego.RichBlockTableCell{
		Text:     tu.RichTextPlain(value),
		IsHeader: header,
		Align:    "left",
		Valign:   "middle",
	}
}

func (c *TelegramChannel) sendStructuredFallback(
	ctx context.Context,
	chatID int64,
	threadID int,
	replyToID string,
	fallback string,
	markup *telego.InlineKeyboardMarkup,
	pending *telegramSessionMenu,
	ephemeral *telegramEphemeralTarget,
) ([]string, error) {
	if fallback == "" {
		fallback = "Structured response is unavailable on this Telegram server."
	}
	formattedFallback := fallback
	if hasTelegramRichTable(fallback) {
		formattedFallback = telegramTableFallbackPlainText(fallback)
	}
	chunks := channels.SplitMessage(formattedFallback, 3500)
	if len(chunks) == 0 {
		chunks = []string{formattedFallback}
	}
	messageIDs := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		params := tu.Message(tu.ID(chatID), chunk)
		params.MessageThreadID = threadID
		if i == 0 && markup != nil {
			params.ReplyMarkup = markup
		}
		currentReply := ""
		if i == 0 {
			currentReply = replyToID
		}
		applyEphemeralSendMessage(params, ephemeral, currentReply)
		pMsg, err := c.bot.SendMessage(ctx, params)
		if err != nil {
			if ephemeral != nil {
				return nil, ephemeralDeliveryError("structured fallback", err)
			}
			return nil, fmt.Errorf("telegram structured fallback: %w", channels.ErrTemporary)
		}
		encodedID, err := validateEphemeralSendResult(pMsg, ephemeral)
		if err != nil {
			return nil, err
		}
		messageIDs = append(messageIDs, encodedID)
		if i == 0 && pending != nil {
			if ephemeral != nil {
				pending.ephemeralID = pMsg.EphemeralMessageID
				pending.receiverUserID = ephemeral.ReceiverUserID
			} else {
				pending.messageID = pMsg.MessageID
			}
			c.storeSessionMenu(*pending)
		}
	}
	return messageIDs, nil
}

func structuredKeyboardMarkup(rows [][]bus.StructuredButton) *telego.InlineKeyboardMarkup {
	if len(rows) == 0 {
		return nil
	}
	keyboard := make([][]telego.InlineKeyboardButton, 0, len(rows))
	for _, row := range rows {
		buttons := make([]telego.InlineKeyboardButton, 0, len(row))
		for _, button := range row {
			if strings.TrimSpace(button.Text) == "" || len([]byte(button.CallbackData)) > 64 {
				continue
			}
			style := normalizeButtonStyle(button.Style)
			buttons = append(
				buttons,
				telego.InlineKeyboardButton{Text: button.Text, CallbackData: button.CallbackData, Style: style},
			)
		}
		if len(buttons) > 0 {
			keyboard = append(keyboard, buttons)
		}
	}
	if len(keyboard) == 0 {
		return nil
	}
	return &telego.InlineKeyboardMarkup{InlineKeyboard: keyboard}
}

func normalizeButtonStyle(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case telego.ButtonStyleDanger, telego.ButtonStyleSuccess, telego.ButtonStylePrimary:
		return strings.ToLower(strings.TrimSpace(style))
	default:
		return ""
	}
}

func (c *TelegramChannel) structuredReplyMarkup(
	content *bus.StructuredContent,
	chatID int64,
	threadID int,
) (*telego.InlineKeyboardMarkup, *telegramSessionMenu, error) {
	if content == nil || content.Interaction == nil {
		if content == nil {
			return nil, nil, nil
		}
		return structuredKeyboardMarkup(content.Keyboard), nil, nil
	}
	menu := *content.Interaction
	menu.Entries = append([]bus.InteractionEntry(nil), content.Interaction.Entries...)
	kind := strings.ToLower(strings.TrimSpace(menu.Kind))
	if (kind != "session" && kind != "model") || menu.OwnerID == "" || menu.Scope == "" || menu.AgentID == "" {
		return nil, nil, fmt.Errorf("interactive menu metadata is incomplete")
	}
	if menu.Channel != "" && menu.Channel != c.Name() {
		return nil, nil, fmt.Errorf("interactive menu channel mismatch")
	}
	token, err := newSessionMenuToken()
	if err != nil {
		return nil, nil, err
	}
	callback := func(code string) string { return sessionCallbackPrefix + token + ":" + code }
	var keyboard [][]telego.InlineKeyboardButton
	if kind == "model" {
		keyboard = modelInteractionKeyboard(menu, callback)
	} else {
		keyboard = sessionInteractionKeyboard(menu, callback)
	}
	for _, row := range keyboard {
		for _, button := range row {
			if len([]byte(button.CallbackData)) == 0 || len([]byte(button.CallbackData)) > 64 {
				return nil, nil, fmt.Errorf("callback_data exceeds Telegram limit")
			}
		}
	}
	pending := &telegramSessionMenu{
		token: token, menu: menu, chatID: chatID, threadID: threadID, createdAt: time.Now(),
	}
	return &telego.InlineKeyboardMarkup{InlineKeyboard: keyboard}, pending, nil
}

func sessionInteractionKeyboard(
	menu bus.InteractionMenu,
	callback func(string) string,
) [][]telego.InlineKeyboardButton {
	keyboard := make([][]telego.InlineKeyboardButton, 0, 4)
	selectRow := make([]telego.InlineKeyboardButton, 0, 5)
	for idx, entry := range menu.Entries {
		if entry.Action != "select" {
			continue
		}
		button := telego.InlineKeyboardButton{Text: entry.Label, CallbackData: callback("s" + strconv.Itoa(idx))}
		if entry.Value == menu.Current {
			button.Style = telego.ButtonStyleSuccess
		}
		selectRow = append(selectRow, button)
		if len(selectRow) == 5 {
			keyboard = append(keyboard, selectRow)
			selectRow = make([]telego.InlineKeyboardButton, 0, 5)
		}
	}
	if len(selectRow) > 0 {
		keyboard = append(keyboard, selectRow)
	}

	prev := telego.InlineKeyboardButton{Text: "·", CallbackData: callback("o")}
	next := telego.InlineKeyboardButton{Text: "·", CallbackData: callback("o")}
	for _, entry := range menu.Entries {
		if entry.Action != "page" {
			continue
		}
		page, parseErr := strconv.Atoi(entry.Value)
		if parseErr != nil {
			continue
		}
		button := telego.InlineKeyboardButton{
			Text:         entry.Label,
			CallbackData: callback("p" + strconv.Itoa(page)),
			Style:        telego.ButtonStylePrimary,
		}
		if page < menu.Page {
			prev = button
		} else if page > menu.Page {
			next = button
		}
	}
	pageButton := telego.InlineKeyboardButton{
		Text:         fmt.Sprintf("Halaman %d/%d", menu.Page+1, menu.Pages),
		CallbackData: callback("o"),
	}
	keyboard = append(keyboard, []telego.InlineKeyboardButton{prev, pageButton, next})
	keyboard = append(keyboard, []telego.InlineKeyboardButton{
		{Text: "➕ Baru", CallbackData: callback("n"), Style: telego.ButtonStylePrimary},
		{Text: "✏️ Rename", CallbackData: callback("r"), Style: telego.ButtonStylePrimary},
	})
	keyboard = append(
		keyboard,
		[]telego.InlineKeyboardButton{{Text: "✖️ Tutup", CallbackData: callback("x"), Style: telego.ButtonStyleDanger}},
	)
	return keyboard
}

func modelInteractionKeyboard(menu bus.InteractionMenu, callback func(string) string) [][]telego.InlineKeyboardButton {
	keyboard := make([][]telego.InlineKeyboardButton, 0, 8)
	appendGrouped := func(indices []int, size int, style string) {
		for len(indices) > 0 {
			n := size
			if len(indices) < n {
				n = len(indices)
			}
			row := make([]telego.InlineKeyboardButton, 0, n)
			for _, idx := range indices[:n] {
				entry := menu.Entries[idx]
				row = append(row, telego.InlineKeyboardButton{
					Text: entry.Label, CallbackData: callback("m" + strconv.Itoa(idx)), Style: style,
				})
			}
			keyboard = append(keyboard, row)
			indices = indices[n:]
		}
	}

	selects := make([]int, 0, 5)
	navigation := make([]int, 0, 3)
	providerFilters := make([]int, 0, 6)
	actions := make([]int, 0, 8)
	closeEntries := make([]int, 0, 1)
	for idx, entry := range menu.Entries {
		switch entry.Action {
		case "select":
			selects = append(selects, idx)
		case "page", "noop":
			navigation = append(navigation, idx)
		case "provider":
			providerFilters = append(providerFilters, idx)
		case "close":
			closeEntries = append(closeEntries, idx)
		default:
			actions = append(actions, idx)
		}
	}
	appendGrouped(selects, 5, "")
	appendGrouped(navigation, 3, telego.ButtonStylePrimary)
	appendGrouped(providerFilters, 3, telego.ButtonStylePrimary)
	appendGrouped(actions, 2, telego.ButtonStylePrimary)
	appendGrouped(closeEntries, 1, telego.ButtonStyleDanger)
	return keyboard
}

func newSessionMenuToken() (string, error) {
	var raw [9]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate session menu token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func (c *TelegramChannel) storeSessionMenu(menu telegramSessionMenu) {
	c.sessionMenuMu.Lock()
	defer c.sessionMenuMu.Unlock()
	if c.sessionMenus == nil {
		c.sessionMenus = make(map[string]telegramSessionMenu)
	}
	c.pruneSessionMenusLocked(time.Now())
	c.sessionMenus[menu.token] = menu
}

func (c *TelegramChannel) takeSessionMenu(token string) (telegramSessionMenu, bool) {
	c.sessionMenuMu.Lock()
	defer c.sessionMenuMu.Unlock()
	if c.sessionMenus == nil {
		return telegramSessionMenu{}, false
	}
	c.pruneSessionMenusLocked(time.Now())
	menu, ok := c.sessionMenus[token]
	return menu, ok
}

func (c *TelegramChannel) replaceSessionMenu(oldToken string, menu telegramSessionMenu) {
	c.sessionMenuMu.Lock()
	defer c.sessionMenuMu.Unlock()
	if c.sessionMenus == nil {
		c.sessionMenus = make(map[string]telegramSessionMenu)
	}
	delete(c.sessionMenus, oldToken)
	c.pruneSessionMenusLocked(time.Now())
	c.sessionMenus[menu.token] = menu
}

func (c *TelegramChannel) deleteSessionMenu(token string) {
	c.sessionMenuMu.Lock()
	delete(c.sessionMenus, token)
	c.sessionMenuMu.Unlock()
}

func (c *TelegramChannel) pruneSessionMenusLocked(now time.Time) {
	for token, menu := range c.sessionMenus {
		if now.Sub(menu.createdAt) > sessionMenuTTL {
			delete(c.sessionMenus, token)
		}
	}
}

func isInternalSessionCallback(data string) bool {
	return strings.HasPrefix(strings.TrimSpace(data), sessionCallbackPrefix)
}

func parseInternalSessionCallback(data string) (token, code string, ok bool) {
	if !isInternalSessionCallback(data) {
		return "", "", false
	}
	rest := strings.TrimPrefix(strings.TrimSpace(data), sessionCallbackPrefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 || len(parts[0]) != 12 || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	if _, err := base64.RawURLEncoding.DecodeString(parts[0]); err != nil {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (c *TelegramChannel) handleInternalSessionCallback(
	ctx context.Context,
	query *telego.CallbackQuery,
) error {
	if query == nil || strings.TrimSpace(query.ID) == "" {
		return nil
	}
	token, code, parsed := parseInternalSessionCallback(query.Data)
	if !parsed {
		return c.answerSessionCallback(ctx, query.ID, "Menu tidak valid. Jalankan command lagi.", true)
	}
	menu, found := c.takeSessionMenu(token)
	if !found {
		return c.answerSessionCallback(ctx, query.ID, "Menu sudah kedaluwarsa. Jalankan command lagi.", true)
	}
	if query.Message == nil {
		return c.answerSessionCallback(ctx, query.ID, "Menu tidak memiliki konteks pesan yang valid.", true)
	}
	message := query.Message.Message()
	if message == nil || !c.sessionCallbackEnvelopeValid(query, message, menu) {
		return c.answerSessionCallback(ctx, query.ID, "Menu ini tidak dapat digunakan dari akun atau chat ini.", true)
	}
	action, value, ok := resolveSessionMenuAction(menu.menu, code)
	if !ok {
		return c.answerSessionCallback(ctx, query.ID, "Tombol tidak valid. Jalankan command lagi.", true)
	}

	answerText := ""
	showAlert := false
	switch action {
	case "rename":
		answerText = "Gunakan /session rename <nama baru>."
		showAlert = true
	case "noop":
		answerText = fmt.Sprintf("Halaman %d/%d", menu.menu.Page+1, menu.menu.Pages)
	}
	// Stop the client spinner before any disk I/O, discovery request, or message edit.
	if err := c.answerSessionCallback(ctx, query.ID, answerText, showAlert); err != nil {
		return err
	}

	handler := c.currentInternalCallbackHandler()
	if handler == nil {
		return nil
	}
	response, handlerErr := handler(ctx, bus.InternalCallbackRequest{
		Kind:       menu.menu.Kind,
		Action:     action,
		Value:      value,
		OwnerID:    menu.menu.OwnerID,
		Channel:    menu.menu.Channel,
		Account:    menu.menu.Account,
		ChatID:     menu.menu.ChatID,
		TopicID:    menu.menu.TopicID,
		MessageID:  strconv.Itoa(message.MessageID),
		AgentID:    menu.menu.AgentID,
		Scope:      menu.menu.Scope,
		Inbound:    menu.menu.Inbound,
		Page:       menu.menu.Page,
		SessionKey: menu.menu.Current,
	})
	if handlerErr != nil {
		logger.WarnCF(
			"telegram",
			"Internal callback was rejected",
			map[string]any{"reason": "scope_or_state_validation"},
		)
		return handlerErr
	}
	if response == nil {
		return nil
	}
	if response.Close {
		if clearErr := c.clearSessionMenuKeyboard(ctx, message, menu); clearErr != nil {
			return clearErr
		}
		c.deleteSessionMenu(token)
		return nil
	}
	if response.Content == nil {
		return nil
	}
	markup, pending, markupErr := c.structuredReplyMarkup(response.Content, menu.chatID, menu.threadID)
	if markupErr != nil {
		return markupErr
	}
	if pending == nil {
		return nil
	}
	if editErr := c.editStructuredSessionMenu(ctx, message, menu, response.Content, markup); editErr != nil {
		return editErr
	}
	pending.messageID = menu.messageID
	pending.ephemeralID = menu.ephemeralID
	pending.receiverUserID = menu.receiverUserID
	c.replaceSessionMenu(token, *pending)
	return nil
}

func (c *TelegramChannel) answerSessionCallback(ctx context.Context, id, text string, alert bool) error {
	return c.bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: id,
		Text:            text,
		ShowAlert:       alert,
	})
}

func (c *TelegramChannel) sessionCallbackEnvelopeValid(
	query *telego.CallbackQuery,
	message *telego.Message,
	menu telegramSessionMenu,
) bool {
	if query == nil || message == nil || query.From.IsBot || query.From.ID <= 0 {
		return false
	}
	if strconv.FormatInt(query.From.ID, 10) != strings.TrimSpace(menu.menu.OwnerID) {
		return false
	}
	if menu.menu.Channel != "" && menu.menu.Channel != c.Name() {
		return false
	}
	if message.Chat.ID != menu.chatID || message.MessageThreadID != menu.threadID {
		return false
	}
	if menu.messageID > 0 && message.MessageID != menu.messageID {
		return false
	}
	if menu.ephemeralID > 0 && message.EphemeralMessageID != menu.ephemeralID {
		return false
	}
	return true
}

func resolveSessionMenuAction(menu bus.InteractionMenu, code string) (action, value string, ok bool) {
	if strings.EqualFold(strings.TrimSpace(menu.Kind), "model") && strings.HasPrefix(code, "m") {
		idx, err := strconv.Atoi(strings.TrimPrefix(code, "m"))
		if err != nil || idx < 0 || idx >= len(menu.Entries) {
			return "", "", false
		}
		entry := menu.Entries[idx]
		if strings.TrimSpace(entry.Action) == "" {
			return "", "", false
		}
		return entry.Action, entry.Value, true
	}
	switch {
	case strings.HasPrefix(code, "s"):
		idx, err := strconv.Atoi(strings.TrimPrefix(code, "s"))
		if err != nil || idx < 0 || idx >= len(menu.Entries) || menu.Entries[idx].Action != "select" {
			return "", "", false
		}
		return "select", menu.Entries[idx].Value, true
	case strings.HasPrefix(code, "p"):
		page := strings.TrimPrefix(code, "p")
		for _, entry := range menu.Entries {
			if entry.Action == "page" && entry.Value == page {
				return "page", page, true
			}
		}
		return "", "", false
	case code == "n":
		return menuStaticAction(menu, "new")
	case code == "r":
		return menuStaticAction(menu, "rename")
	case code == "x":
		return menuStaticAction(menu, "close")
	case code == "o":
		return "noop", "", true
	default:
		return "", "", false
	}
}

func menuStaticAction(menu bus.InteractionMenu, action string) (string, string, bool) {
	for _, entry := range menu.Entries {
		if entry.Action == action {
			return action, entry.Value, true
		}
	}
	return "", "", false
}

func (c *TelegramChannel) editStructuredSessionMenu(
	ctx context.Context,
	message *telego.Message,
	menu telegramSessionMenu,
	content *bus.StructuredContent,
	markup *telego.InlineKeyboardMarkup,
) error {
	if menu.ephemeralID > 0 {
		fallback := content.FallbackText()
		if hasTelegramRichTable(fallback) {
			fallback = telegramTableFallbackPlainText(fallback)
		}
		params := &telego.EditEphemeralMessageTextParams{
			ChatID:             tu.ID(menu.chatID),
			ReceiverUserID:     menu.receiverUserID,
			EphemeralMessageID: menu.ephemeralID,
			Text:               fallback,
			ReplyMarkup:        markup,
		}
		if err := c.bot.EditEphemeralMessageText(
			ctx,
			params,
		); err != nil &&
			!strings.Contains(strings.ToLower(err.Error()), "message is not modified") {
			return err
		}
		return nil
	}

	if rich, ok := buildNativeRichMessage(content); ok {
		_, err := c.bot.EditMessageText(ctx, &telego.EditMessageTextParams{
			ChatID: tu.ID(menu.chatID), MessageID: message.MessageID,
			RichMessage: &rich, ReplyMarkup: markup,
		})
		if err == nil || strings.Contains(strings.ToLower(errorString(err)), "message is not modified") {
			return nil
		}
		if !isTelegramFormattingRejection(err) {
			return err
		}
	}
	fallback := content.FallbackText()
	if hasTelegramRichTable(fallback) {
		fallback = telegramTableFallbackPlainText(fallback)
	}
	params := &telego.EditMessageTextParams{
		ChatID: tu.ID(menu.chatID), MessageID: message.MessageID,
		Text: fallback, ReplyMarkup: markup,
	}
	_, err := c.bot.EditMessageText(ctx, params)
	if err != nil && !strings.Contains(strings.ToLower(errorString(err)), "message is not modified") {
		return err
	}
	return nil
}

func (c *TelegramChannel) clearSessionMenuKeyboard(
	ctx context.Context,
	message *telego.Message,
	menu telegramSessionMenu,
) error {
	empty := &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{}}
	if menu.ephemeralID > 0 {
		return c.bot.EditEphemeralMessageReplyMarkup(ctx, &telego.EditEphemeralMessageReplyMarkupParams{
			ChatID: tu.ID(menu.chatID), ReceiverUserID: menu.receiverUserID,
			EphemeralMessageID: menu.ephemeralID, ReplyMarkup: empty,
		})
	}
	_, err := c.bot.EditMessageReplyMarkup(ctx, &telego.EditMessageReplyMarkupParams{
		ChatID: tu.ID(menu.chatID), MessageID: message.MessageID, ReplyMarkup: empty,
	})
	if err != nil && strings.Contains(strings.ToLower(errorString(err)), "message is not modified") {
		return nil
	}
	return err
}
