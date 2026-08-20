package telegram

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/channels"
	"github.com/As-tsaqib/picoclaw/pkg/logger"
)

const (
	sessionCallbackPrefix = "pcsm:"
	sessionMenuTTL        = 15 * time.Minute
	sessionMenuMax        = 512
	sessionRenameTTL      = 5 * time.Minute
	sessionRenameMax      = 256
	modelMenuTTL          = 5 * time.Minute
	richMessageMaxBytes   = 32768
	richMessageMaxBlocks  = 500
	richMessageMaxColumns = 20
	telegramTextMaxRunes  = 4096
)

type telegramSessionMenu struct {
	token            string
	menu             bus.InteractionMenu
	chatID           int64
	threadID         int
	messageID        int
	ephemeralID      int
	receiverUserID   int64
	createdAt        time.Time
	claimedMutations map[string]struct{}
}

type telegramSessionRenamePromptKey struct {
	chatID      int64
	threadID    int
	messageID   int
	ephemeralID int
}

type telegramSessionRenamePrompt struct {
	token     string
	menu      telegramSessionMenu
	action    string
	createdAt time.Time
	consumed  bool
}

type telegramFormattedFallbackChunk struct {
	formatted string
	plain     string
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

	// Ephemeral/private responses must never use SendRichMessage because that
	// method does not carry receiver_user_id authority.
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
	blocks := make([]telego.InputRichBlock, 0, len(content.Paragraphs)+len(content.Blocks)+len(content.Tables)+1)
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
	if len(content.Blocks) > 0 {
		typedBlocks, ok := buildTypedNativeRichBlocks(content.Blocks, &totalBytes, &blockUnits, 0)
		if !ok {
			return telego.InputRichMessage{}, false
		}
		blocks = append(blocks, typedBlocks...)
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
	return telego.RichBlockTableCell{Text: tu.RichTextPlain(value), IsHeader: header, Align: "left", Valign: "middle"}
}

func telegramFallbackRepresentations(raw string, useMarkdownV2 bool) (formatted, plain string) {
	plain = raw
	formatInput := raw
	if hasTelegramRichTable(raw) {
		formatInput = telegramTableFallbackMarkdown(raw)
		plain = telegramTableFallbackPlainText(raw)
	}
	return parseContent(formatInput, useMarkdownV2), plain
}

func splitTelegramFormattedFallback(raw string, useMarkdownV2 bool) []telegramFormattedFallbackChunk {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	queue := channels.SplitMessage(raw, 3500)
	if len(queue) == 0 {
		queue = []string{raw}
	}
	result := make([]telegramFormattedFallbackChunk, 0, len(queue))
	for len(queue) > 0 {
		chunk := queue[0]
		queue = queue[1:]
		formatted, plain := telegramFallbackRepresentations(chunk, useMarkdownV2)
		formattedRunes := utf8.RuneCountInString(formatted)
		plainRunes := utf8.RuneCountInString(plain)
		if formattedRunes <= telegramTextMaxRunes && plainRunes <= telegramTextMaxRunes {
			result = append(result, telegramFormattedFallbackChunk{formatted: formatted, plain: plain})
			continue
		}

		rawRunes := []rune(chunk)
		if len(rawRunes) <= 1 {
			// parseContent/table rendering has bounded expansion for one input
			// rune. This guard is defensive and guarantees progress.
			result = append(result, telegramFormattedFallbackChunk{
				formatted: truncateTelegramRunes(formatted, telegramTextMaxRunes),
				plain:     truncateTelegramRunes(plain, telegramTextMaxRunes),
			})
			continue
		}
		expanded := formattedRunes
		if plainRunes > expanded {
			expanded = plainRunes
		}
		if expanded <= 0 {
			expanded = len(rawRunes)
		}
		smallerLen := int(float64(telegramTextMaxRunes) * float64(len(rawRunes)) / float64(expanded) * 0.95)
		if smallerLen < 1 {
			smallerLen = 1
		}
		if smallerLen >= len(rawRunes) {
			smallerLen = len(rawRunes) - 1
		}
		subChunks := channels.SplitMessage(chunk, smallerLen)
		if len(subChunks) <= 1 || (len(subChunks) == 1 && utf8.RuneCountInString(subChunks[0]) >= len(rawRunes)) {
			subChunks = []string{string(rawRunes[:smallerLen]), string(rawRunes[smallerLen:])}
		}
		filtered := make([]string, 0, len(subChunks))
		for _, sub := range subChunks {
			if strings.TrimSpace(sub) != "" {
				filtered = append(filtered, sub)
			}
		}
		if len(filtered) == 0 {
			filtered = []string{string(rawRunes[:smallerLen]), string(rawRunes[smallerLen:])}
		}
		queue = append(filtered, queue...)
	}
	return result
}

func truncateTelegramRunes(value string, maximum int) string {
	runes := []rune(value)
	if maximum <= 0 {
		return ""
	}
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
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
	useMarkdownV2 := c.tgCfg.UseMarkdownV2
	chunks := splitTelegramFormattedFallback(fallback, useMarkdownV2)
	if len(chunks) == 0 {
		chunks = []telegramFormattedFallbackChunk{{formatted: fallback, plain: fallback}}
	}
	messageIDs := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		params := tu.Message(tu.ID(chatID), chunk.formatted)
		params.MessageThreadID = threadID
		if useMarkdownV2 {
			params.WithParseMode(telego.ModeMarkdownV2)
		} else {
			params.WithParseMode(telego.ModeHTML)
		}
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
				if !isTelegramParseRejection(err) {
					return nil, ephemeralDeliveryError("structured fallback", err)
				}
				params.Text = chunk.plain
				params.ParseMode = ""
				pMsg, err = c.bot.SendMessage(ctx, params)
				if err != nil {
					return nil, ephemeralDeliveryError("structured format fallback", err)
				}
			} else {
				logParseFailed(err, useMarkdownV2)
				params.Text = chunk.plain
				params.ParseMode = ""
				pMsg, err = c.bot.SendMessage(ctx, params)
				if err != nil {
					return nil, fmt.Errorf("telegram structured fallback: %w", channels.ErrTemporary)
				}
			}
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
			buttons = append(buttons, telego.InlineKeyboardButton{
				Text: button.Text, CallbackData: button.CallbackData, Style: normalizeButtonStyle(button.Style),
			})
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

func expectedMemoryMenuRoute(chatID int64, threadID int) (string, string) {
	chat := strconv.FormatInt(chatID, 10)
	topic := ""
	if threadID > 0 {
		topic = strconv.Itoa(threadID)
		chat += "/" + topic
	}
	return chat, topic
}

func sealMemoryInteractionAccount(menu *bus.InteractionMenu, account string) {
	if menu == nil {
		return
	}
	account = strings.TrimSpace(account)
	menuAccount := strings.TrimSpace(menu.Account)
	inboundAccount := strings.TrimSpace(menu.Inbound.Account)
	switch {
	case menuAccount != "" && inboundAccount == "":
		menu.Inbound.Account = menuAccount
	case menuAccount == "" && inboundAccount != "":
		menu.Account = inboundAccount
	case menuAccount == "" && inboundAccount == "" && account != "":
		// Seal legacy/adapter-local structured content to this channel account
		// before it becomes callback state. Mismatched non-empty values are
		// deliberately left untouched so validation still fails closed.
		menu.Account = account
		menu.Inbound.Account = account
	}
}

func rebindMemoryInteractionRoute(next *bus.InteractionMenu, current bus.InteractionMenu) {
	if next == nil {
		return
	}
	next.Kind = current.Kind
	next.OwnerID = current.OwnerID
	next.Channel = current.Channel
	next.Account = current.Account
	next.ChatID = current.ChatID
	next.TopicID = current.TopicID
	next.AgentID = current.AgentID
	next.Scope = current.Scope
	next.Inbound = current.Inbound
}

func validateMemoryInteractionEnvelope(menu bus.InteractionMenu, chatID int64, threadID int, channel string) error {
	if strings.TrimSpace(menu.OwnerID) == "" || strings.TrimSpace(menu.AgentID) == "" ||
		strings.TrimSpace(menu.Channel) == "" || strings.TrimSpace(menu.Account) == "" ||
		strings.TrimSpace(menu.ChatID) == "" {
		return fmt.Errorf("memory interaction route metadata is incomplete")
	}
	if menu.Channel != channel {
		return fmt.Errorf("memory interaction channel mismatch")
	}
	expectedChat, expectedTopic := expectedMemoryMenuRoute(chatID, threadID)
	if menu.ChatID != expectedChat || menu.TopicID != expectedTopic {
		return fmt.Errorf("memory interaction chat or topic mismatch")
	}
	inbound := bus.NormalizeInboundMessage(bus.InboundMessage{Context: menu.Inbound}).Context
	if inbound.SenderID != menu.OwnerID || inbound.Channel != menu.Channel || inbound.Account != menu.Account ||
		inbound.ChatID != menu.ChatID || inbound.TopicID != menu.TopicID {
		return fmt.Errorf("memory interaction trusted inbound mismatch")
	}
	return nil
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
	if (kind != "session" && kind != "model" && kind != "memory" && kind != "skill" && kind != "checkpoint") ||
		menu.OwnerID == "" || menu.AgentID == "" {
		return nil, nil, fmt.Errorf("interactive menu metadata is incomplete")
	}
	if (kind == "session" || kind == "model" || kind == "skill" || kind == "checkpoint") && menu.Scope == "" {
		return nil, nil, fmt.Errorf("interactive menu metadata is incomplete")
	}
	if kind == "skill" || kind == "checkpoint" {
		if strings.TrimSpace(menu.SessionKey) == "" {
			return nil, nil, fmt.Errorf("interactive menu session binding is incomplete")
		}
	}
	if kind == "memory" || kind == "skill" || kind == "checkpoint" {
		sealMemoryInteractionAccount(&menu, c.Name())
		if err := validateMemoryInteractionEnvelope(menu, chatID, threadID, c.Name()); err != nil {
			return nil, nil, err
		}
	} else if menu.Channel != "" && menu.Channel != c.Name() {
		return nil, nil, fmt.Errorf("interactive menu channel mismatch")
	}
	token, err := newSessionMenuToken()
	if err != nil {
		return nil, nil, err
	}
	callback := func(code string) string { return sessionCallbackPrefix + token + ":" + code }
	var keyboard [][]telego.InlineKeyboardButton
	switch kind {
	case "model":
		keyboard = modelInteractionKeyboard(menu, callback)
	case "memory", "skill", "checkpoint":
		keyboard = entryInteractionKeyboard(menu, callback)
	default:
		keyboard = sessionInteractionKeyboard(menu, callback)
	}
	for _, row := range keyboard {
		for _, button := range row {
			if len([]byte(button.CallbackData)) == 0 || len([]byte(button.CallbackData)) > 64 {
				return nil, nil, fmt.Errorf("callback_data exceeds Telegram limit")
			}
		}
	}
	pending := &telegramSessionMenu{token: token, menu: menu, chatID: chatID, threadID: threadID, createdAt: time.Now()}
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
		page, err := strconv.Atoi(entry.Value)
		if err != nil {
			continue
		}
		button := telego.InlineKeyboardButton{
			Text: entry.Label, CallbackData: callback("p" + strconv.Itoa(page)), Style: telego.ButtonStylePrimary,
		}
		if page < menu.Page {
			prev = button
		} else if page > menu.Page {
			next = button
		}
	}
	pageButton := telego.InlineKeyboardButton{
		Text: fmt.Sprintf("Halaman %d/%d", menu.Page+1, menu.Pages), CallbackData: callback("o"),
	}
	keyboard = append(keyboard, []telego.InlineKeyboardButton{prev, pageButton, next})
	keyboard = append(keyboard, []telego.InlineKeyboardButton{
		{Text: "➕ New", CallbackData: callback("n"), Style: telego.ButtonStylePrimary},
		{Text: "🗑️ Remove", CallbackData: callback("d"), Style: telego.ButtonStyleDanger},
		{Text: "✏️ Rename", CallbackData: callback("r"), Style: telego.ButtonStylePrimary},
	})
	keyboard = append(keyboard, []telego.InlineKeyboardButton{{
		Text: "✖️ Tutup", CallbackData: callback("x"), Style: telego.ButtonStyleDanger,
	}})
	return keyboard
}

func entryInteractionKeyboard(menu bus.InteractionMenu, callback func(string) string) [][]telego.InlineKeyboardButton {
	keyboard := make([][]telego.InlineKeyboardButton, 0, 6)
	selects := make([]int, 0, 5)
	pages := make([]int, 0, 3)
	actions := make([]int, 0, 8)
	dangerActions := make([]int, 0, 2)
	closeEntries := make([]int, 0, 1)
	for idx, entry := range menu.Entries {
		action := strings.ToLower(strings.TrimSpace(entry.Action))
		switch action {
		case "close":
			closeEntries = append(closeEntries, idx)
		case "page", "browse_page", "search_page", "pending_page", "noop":
			pages = append(pages, idx)
		case "forget", "forget_confirm", "reject", "archive", "archive_confirm":
			dangerActions = append(dangerActions, idx)
		default:
			if len(entry.Label) <= 4 && action == "detail" {
				selects = append(selects, idx)
			} else {
				actions = append(actions, idx)
			}
		}
	}
	appendGrouped := func(indices []int, size int, defaultStyle string) {
		for len(indices) > 0 {
			n := size
			if len(indices) < n {
				n = len(indices)
			}
			row := make([]telego.InlineKeyboardButton, 0, n)
			for _, idx := range indices[:n] {
				entry := menu.Entries[idx]
				style := defaultStyle
				if entry.Action == "forget" || entry.Action == "reject" || entry.Action == "archive" ||
					entry.Action == "archive_confirm" || entry.Action == "close" {
					style = telego.ButtonStyleDanger
				} else if entry.Action == "approve" || entry.Action == "restore" {
					style = telego.ButtonStyleSuccess
				}
				row = append(row, telego.InlineKeyboardButton{
					Text: entry.Label, CallbackData: callback("e" + strconv.Itoa(idx)), Style: style,
				})
			}
			keyboard = append(keyboard, row)
			indices = indices[n:]
		}
	}
	appendGrouped(selects, 5, "")
	appendGrouped(pages, 3, telego.ButtonStylePrimary)
	appendGrouped(actions, 2, telego.ButtonStylePrimary)
	appendGrouped(dangerActions, 2, telego.ButtonStyleDanger)
	appendGrouped(closeEntries, 1, telego.ButtonStyleDanger)
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
	c.enforceSessionMenuCapacityLocked()
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
	for token, existing := range c.sessionMenus {
		if existing.chatID == menu.chatID && existing.threadID == menu.threadID &&
			existing.messageID == menu.messageID && existing.ephemeralID == menu.ephemeralID {
			delete(c.sessionMenus, token)
		}
	}
	c.pruneSessionMenusLocked(time.Now())
	c.sessionMenus[menu.token] = menu
	c.enforceSessionMenuCapacityLocked()
}

func (c *TelegramChannel) deleteSessionMenu(token string) {
	c.sessionMenuMu.Lock()
	delete(c.sessionMenus, token)
	c.sessionMenuMu.Unlock()
}

func (c *TelegramChannel) consumeSessionMenu(token string) bool {
	c.sessionMenuMu.Lock()
	defer c.sessionMenuMu.Unlock()
	if c.sessionMenus == nil {
		return false
	}
	c.pruneSessionMenusLocked(time.Now())
	if _, ok := c.sessionMenus[token]; !ok {
		return false
	}
	delete(c.sessionMenus, token)
	return true
}

func isInteractionMutationAction(kind, action string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	action = strings.ToLower(strings.TrimSpace(action))
	switch kind {
	case "memory":
		switch action {
		case "forget", "approve", "reject", "pin", "unpin", "archive", "restore":
			return true
		}
	case "skill":
		return action == "arm" || action == "clear"
	case "checkpoint":
		return action == "resume" || action == "archive_confirm"
	}
	return false
}

func isInteractionPromptAction(kind, action string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	action = strings.ToLower(strings.TrimSpace(action))
	return (kind == "session" && action == "rename") ||
		(kind == "memory" && (action == "search" || action == "edit")) ||
		(kind == "skill" && action == "search")
}

func (c *TelegramChannel) claimSessionMenuMutation(token, code string) bool {
	c.sessionMenuMu.Lock()
	defer c.sessionMenuMu.Unlock()
	if c.sessionMenus == nil {
		return false
	}
	c.pruneSessionMenusLocked(time.Now())
	menu, ok := c.sessionMenus[token]
	if !ok {
		return false
	}
	if menu.claimedMutations == nil {
		menu.claimedMutations = make(map[string]struct{})
	}
	if _, exists := menu.claimedMutations[code]; exists {
		return false
	}
	menu.claimedMutations[code] = struct{}{}
	c.sessionMenus[token] = menu
	return true
}

func (c *TelegramChannel) pruneSessionMenusLocked(now time.Time) {
	for token, menu := range c.sessionMenus {
		menuTTL := sessionMenuTTL
		if strings.EqualFold(strings.TrimSpace(menu.menu.Kind), "model") {
			menuTTL = modelMenuTTL
		}
		if now.Sub(menu.createdAt) > menuTTL {
			delete(c.sessionMenus, token)
		}
	}
}

func (c *TelegramChannel) enforceSessionMenuCapacityLocked() {
	for len(c.sessionMenus) > sessionMenuMax {
		keys := make([]string, 0, len(c.sessionMenus))
		for token := range c.sessionMenus {
			keys = append(keys, token)
		}
		sort.Slice(keys, func(i, j int) bool {
			a := c.sessionMenus[keys[i]]
			b := c.sessionMenus[keys[j]]
			if a.createdAt.Equal(b.createdAt) {
				return keys[i] < keys[j]
			}
			return a.createdAt.Before(b.createdAt)
		})
		delete(c.sessionMenus, keys[0])
	}
}

func (c *TelegramChannel) beginSessionRenamePrompt(
	ctx context.Context,
	query *telego.CallbackQuery,
	token string,
	menu telegramSessionMenu,
	promptText string,
	action string,
) error {
	promptText = strings.TrimSpace(promptText)
	if promptText == "" {
		promptText = "Balas pesan ini dengan input baru."
	}
	placeholder := "Input baru"
	switch action {
	case "rename":
		placeholder = "Nama session baru"
	case "search":
		placeholder = "Kata kunci pencarian"
	case "edit":
		placeholder = "Konten baru"
	}
	params := tu.Message(tu.ID(menu.chatID), promptText)
	params.MessageThreadID = menu.threadID
	params.ReplyMarkup = &telego.ForceReply{ForceReply: true, InputFieldPlaceholder: placeholder, Selective: true}
	if menu.ephemeralID > 0 {
		params.ReceiverUserID = menu.receiverUserID
		if params.ReceiverUserID <= 0 && query != nil {
			params.ReceiverUserID = query.From.ID
		}
		if query != nil {
			params.CallbackQueryID = query.ID
		}
	} else if replyID, err := strconv.Atoi(strings.TrimSpace(menu.menu.Inbound.MessageID)); err == nil && replyID > 0 {
		params.ReplyParameters = &telego.ReplyParameters{MessageID: replyID, AllowSendingWithoutReply: true}
	}
	promptMessage, err := c.bot.SendMessage(ctx, params)
	if err != nil {
		return fmt.Errorf("send session rename prompt: %w", err)
	}
	if promptMessage == nil || (promptMessage.MessageID <= 0 && promptMessage.EphemeralMessageID <= 0) {
		return fmt.Errorf("send session rename prompt: Telegram returned no message identity")
	}
	key := telegramSessionRenamePromptKey{chatID: menu.chatID, threadID: menu.threadID}
	if promptMessage.EphemeralMessageID > 0 {
		key.ephemeralID = promptMessage.EphemeralMessageID
	} else {
		key.messageID = promptMessage.MessageID
	}
	c.storeSessionRenamePrompt(key, telegramSessionRenamePrompt{
		token: token, menu: menu, action: action, createdAt: time.Now(),
	})
	return nil
}

func (c *TelegramChannel) storeSessionRenamePrompt(
	key telegramSessionRenamePromptKey,
	prompt telegramSessionRenamePrompt,
) {
	c.sessionRenameMu.Lock()
	defer c.sessionRenameMu.Unlock()
	if c.sessionRenamePrompts == nil {
		c.sessionRenamePrompts = make(map[telegramSessionRenamePromptKey]telegramSessionRenamePrompt)
	}
	now := time.Now()
	c.pruneSessionRenamePromptsLocked(now)
	for existingKey, existing := range c.sessionRenamePrompts {
		if existing.menu.chatID == prompt.menu.chatID && existing.menu.threadID == prompt.menu.threadID &&
			existing.menu.menu.OwnerID == prompt.menu.menu.OwnerID &&
			existing.menu.menu.AgentID == prompt.menu.menu.AgentID &&
			existing.action == prompt.action && !existing.consumed {
			existing.consumed = true
			c.sessionRenamePrompts[existingKey] = existing
		}
	}
	for len(c.sessionRenamePrompts) >= sessionRenameMax {
		var oldestKey telegramSessionRenamePromptKey
		var oldestTime time.Time
		found := false
		for candidateKey, candidate := range c.sessionRenamePrompts {
			if !found || candidate.createdAt.Before(oldestTime) {
				oldestKey, oldestTime, found = candidateKey, candidate.createdAt, true
			}
		}
		if !found {
			break
		}
		delete(c.sessionRenamePrompts, oldestKey)
	}
	c.sessionRenamePrompts[key] = prompt
}

func (c *TelegramChannel) pruneSessionRenamePromptsLocked(now time.Time) {
	for key, prompt := range c.sessionRenamePrompts {
		if now.Sub(prompt.createdAt) > 2*sessionRenameTTL {
			delete(c.sessionRenamePrompts, key)
		}
	}
}

type sessionRenameClaimStatus uint8

const (
	sessionRenameClaimNone sessionRenameClaimStatus = iota
	sessionRenameClaimRejected
	sessionRenameClaimExpired
	sessionRenameClaimReplay
	sessionRenameClaimInvalid
	sessionRenameClaimed
)

func sessionRenamePromptKeyFromReply(message *telego.Message) (telegramSessionRenamePromptKey, bool) {
	if message == nil || message.ReplyToMessage == nil {
		return telegramSessionRenamePromptKey{}, false
	}
	reply := message.ReplyToMessage
	key := telegramSessionRenamePromptKey{chatID: message.Chat.ID, threadID: message.MessageThreadID}
	if reply.EphemeralMessageID > 0 {
		key.ephemeralID = reply.EphemeralMessageID
	} else if reply.MessageID > 0 {
		key.messageID = reply.MessageID
	} else {
		return telegramSessionRenamePromptKey{}, false
	}
	return key, true
}

func (c *TelegramChannel) claimSessionRenamePrompt(
	message *telego.Message,
) (telegramSessionRenamePrompt, sessionRenameClaimStatus) {
	key, ok := sessionRenamePromptKeyFromReply(message)
	if !ok {
		return telegramSessionRenamePrompt{}, sessionRenameClaimNone
	}
	c.sessionRenameMu.Lock()
	defer c.sessionRenameMu.Unlock()
	prompt, ok := c.sessionRenamePrompts[key]
	if !ok {
		c.pruneSessionRenamePromptsLocked(time.Now())
		return telegramSessionRenamePrompt{}, sessionRenameClaimNone
	}
	now := time.Now()
	if now.Sub(prompt.createdAt) > sessionRenameTTL {
		prompt.consumed = true
		c.sessionRenamePrompts[key] = prompt
		return prompt, sessionRenameClaimExpired
	}
	if prompt.consumed {
		return prompt, sessionRenameClaimReplay
	}
	if message.From == nil || message.From.IsBot || message.From.ID <= 0 ||
		strconv.FormatInt(message.From.ID, 10) != strings.TrimSpace(prompt.menu.menu.OwnerID) ||
		(prompt.menu.receiverUserID > 0 && message.From.ID != prompt.menu.receiverUserID) ||
		(prompt.menu.menu.Channel != "" && prompt.menu.menu.Channel != c.Name()) ||
		strings.TrimSpace(prompt.menu.menu.AgentID) == "" {
		return prompt, sessionRenameClaimRejected
	}
	kind := strings.ToLower(strings.TrimSpace(prompt.menu.menu.Kind))
	if (kind == "session" || kind == "model" || kind == "skill" || kind == "checkpoint") &&
		strings.TrimSpace(prompt.menu.menu.Scope) == "" {
		return prompt, sessionRenameClaimRejected
	}
	if kind == "session" && prompt.action == "rename" && strings.TrimSpace(prompt.menu.menu.Current) == "" {
		return prompt, sessionRenameClaimRejected
	}
	if kind == "memory" || kind == "skill" || kind == "checkpoint" {
		if err := validateMemoryInteractionEnvelope(
			prompt.menu.menu, prompt.menu.chatID, prompt.menu.threadID, c.Name(),
		); err != nil {
			return prompt, sessionRenameClaimRejected
		}
	}
	if kind == "memory" {
		if (prompt.action == "edit" && strings.TrimSpace(prompt.menu.menu.Current) == "") ||
			(prompt.action != "edit" && prompt.action != "search") {
			return prompt, sessionRenameClaimRejected
		}
	}
	if kind == "skill" {
		if prompt.action != "search" || strings.TrimSpace(prompt.menu.menu.SessionKey) == "" {
			return prompt, sessionRenameClaimRejected
		}
	}
	senderID := strconv.FormatInt(message.From.ID, 10)
	if !c.IsAllowedSender(bus.SenderInfo{
		Platform: "telegram", PlatformID: senderID, CanonicalID: "telegram:" + senderID,
		Username: message.From.Username, DisplayName: message.From.FirstName,
	}) {
		return prompt, sessionRenameClaimRejected
	}
	if strings.TrimSpace(message.Text) == "" {
		return prompt, sessionRenameClaimInvalid
	}
	prompt.consumed = true
	c.sessionRenamePrompts[key] = prompt
	return prompt, sessionRenameClaimed
}

func (c *TelegramChannel) handlePendingSessionRenameReply(ctx context.Context, message *telego.Message) (bool, error) {
	prompt, status := c.claimSessionRenamePrompt(message)
	switch status {
	case sessionRenameClaimNone:
		return false, nil
	case sessionRenameClaimRejected, sessionRenameClaimReplay:
		return true, nil
	case sessionRenameClaimExpired:
		noticeText := "Permintaan sudah kedaluwarsa. Jalankan command lagi."
		if prompt.menu.menu.Kind == "session" {
			noticeText = "Permintaan rename sudah kedaluwarsa. Jalankan /session lagi."
		} else if prompt.menu.menu.Kind == "memory" {
			noticeText = "Permintaan memory sudah kedaluwarsa. Jalankan /memory lagi."
		} else if prompt.menu.menu.Kind == "skill" {
			noticeText = "Skill search sudah kedaluwarsa. Jalankan /use lagi."
		}
		return true, c.sendSessionRenameNotice(ctx, message, prompt, noticeText)
	case sessionRenameClaimInvalid:
		invalidText := "Input harus berupa teks yang tidak kosong."
		if prompt.menu.menu.Kind == "session" {
			invalidText = "Nama session harus berupa teks yang tidak kosong."
		} else if prompt.menu.menu.Kind == "memory" {
			invalidText = "Input memory harus berupa teks yang tidak kosong."
		} else if prompt.menu.menu.Kind == "skill" {
			invalidText = "Skill search harus berupa teks yang tidak kosong."
		}
		return true, c.sendSessionRenameNotice(ctx, message, prompt, invalidText)
	}
	handler := c.currentInternalCallbackHandler()
	if handler == nil {
		return true, c.sendSessionRenameNotice(ctx, message, prompt, "Layanan sedang tidak tersedia.")
	}
	inbound := prompt.menu.menu.Inbound
	inbound.MessageID = strconv.Itoa(message.MessageID)
	if message.EphemeralMessageID > 0 {
		inbound.MessageID = encodeInboundEphemeralMessageID(message.EphemeralMessageID)
	}
	if message.ReplyToMessage != nil {
		inbound.ReplyToMessageID = strconv.Itoa(message.ReplyToMessage.MessageID)
		if message.ReplyToMessage.EphemeralMessageID > 0 {
			inbound.ReplyToMessageID = encodeInboundEphemeralMessageID(message.ReplyToMessage.EphemeralMessageID)
		}
	}
	response, err := handler(ctx, bus.InternalCallbackRequest{
		Kind: prompt.menu.menu.Kind, Action: prompt.action, Value: message.Text,
		OwnerID: prompt.menu.menu.OwnerID, Channel: prompt.menu.menu.Channel, Account: prompt.menu.menu.Account,
		ChatID: prompt.menu.menu.ChatID, TopicID: prompt.menu.menu.TopicID, MessageID: inbound.MessageID,
		AgentID: prompt.menu.menu.AgentID, Scope: prompt.menu.menu.Scope, Inbound: inbound,
		Page: prompt.menu.menu.Page, SessionKey: interactionMenuSessionState(prompt.menu.menu), Query: prompt.menu.menu.Query,
	})
	if err != nil {
		logger.WarnCF("telegram", "Prompt reply was rejected", map[string]any{"reason": "scope_or_state_validation"})
		return true, c.sendSessionRenameNotice(ctx, message, prompt, "Permintaan tidak dapat diproses. Jalankan command lagi.")
	}
	if response == nil || response.Content == nil {
		text := "Perubahan berhasil disimpan."
		if response != nil && strings.TrimSpace(response.Text) != "" {
			text = strings.TrimSpace(response.Text)
		}
		return true, c.sendSessionRenameNotice(ctx, message, prompt, text)
	}
	if err := c.refreshSessionMenuAfterRename(ctx, prompt, response.Content); err != nil {
		logger.WarnCF(
			"telegram", "Menu refresh after prompt reply failed", map[string]any{"reason": "telegram_edit_failed"},
		)
		return true, c.sendSessionRenameNotice(
			ctx, message, prompt, "Perubahan berhasil disimpan. Jalankan command lagi untuk memperbarui dashboard.",
		)
	}
	return true, nil
}

func (c *TelegramChannel) refreshSessionMenuAfterRename(
	ctx context.Context,
	prompt telegramSessionRenamePrompt,
	content *bus.StructuredContent,
) error {
	if content != nil && strings.EqualFold(prompt.menu.menu.Kind, "memory") {
		rebindMemoryInteractionRoute(content.Interaction, prompt.menu.menu)
	}
	markup, pending, err := c.structuredReplyMarkup(content, prompt.menu.chatID, prompt.menu.threadID)
	if err != nil {
		return err
	}
	if pending == nil {
		return fmt.Errorf("prompt reply response has no interaction menu")
	}
	message := &telego.Message{MessageID: prompt.menu.messageID, Chat: telego.Chat{ID: prompt.menu.chatID}}
	if err := c.editStructuredSessionMenu(ctx, message, prompt.menu, content, markup); err != nil {
		return err
	}
	pending.messageID = prompt.menu.messageID
	pending.ephemeralID = prompt.menu.ephemeralID
	pending.receiverUserID = prompt.menu.receiverUserID
	c.replaceSessionMenu(prompt.token, *pending)
	return nil
}

func (c *TelegramChannel) sendSessionRenameNotice(
	ctx context.Context,
	message *telego.Message,
	prompt telegramSessionRenamePrompt,
	text string,
) error {
	if message == nil {
		return nil
	}
	params := tu.Message(tu.ID(message.Chat.ID), text)
	params.MessageThreadID = message.MessageThreadID
	if prompt.menu.ephemeralID > 0 {
		if prompt.menu.receiverUserID <= 0 {
			return fmt.Errorf("private prompt receiver authority is unavailable")
		}
		params.ReceiverUserID = prompt.menu.receiverUserID
		if message.EphemeralMessageID > 0 {
			params.ReplyParameters = &telego.ReplyParameters{EphemeralMessageID: message.EphemeralMessageID}
		}
	} else if message.MessageID > 0 {
		params.ReplyParameters = &telego.ReplyParameters{MessageID: message.MessageID, AllowSendingWithoutReply: true}
	}
	if _, err := c.bot.SendMessage(ctx, params); err != nil {
		return fmt.Errorf("send session rename notice: %w", err)
	}
	return nil
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

func interactionMenuSessionState(menu bus.InteractionMenu) string {
	if bound := strings.TrimSpace(menu.SessionKey); bound != "" {
		return bound
	}
	// Compatibility for pre-foundation session/model/memory menus, which used
	// Current as private server-side state. New session-sensitive interactions
	// always populate SessionKey explicitly.
	return strings.TrimSpace(menu.Current)
}

func (c *TelegramChannel) handleInternalSessionCallback(ctx context.Context, query *telego.CallbackQuery) error {
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
	kind := strings.ToLower(strings.TrimSpace(menu.menu.Kind))
	consume := kind == "session" &&
		(action == "select" || action == "new" || action == "remove" || action == "rename" || action == "close")
	if consume && !c.consumeSessionMenu(token) {
		return c.answerSessionCallback(ctx, query.ID, "Tombol sudah diproses. Jalankan /session lagi.", true)
	}
	if isInteractionMutationAction(kind, action) && !c.claimSessionMenuMutation(token, code) {
		return c.answerSessionCallback(ctx, query.ID, "Tombol sudah diproses. Jalankan command lagi.", true)
	}
	answerText := ""
	switch action {
	case "rename":
		answerText = "Balas prompt untuk mengganti nama session."
	case "search":
		if kind == "skill" {
			answerText = "Balas prompt untuk mencari skill."
		} else {
			answerText = "Balas prompt untuk mencari memory."
		}
	case "edit":
		answerText = "Balas prompt untuk mengedit memory."
	case "noop":
		answerText = fmt.Sprintf("Halaman %d/%d", menu.menu.Page+1, menu.menu.Pages)
	}
	if err := c.answerSessionCallback(ctx, query.ID, answerText, false); err != nil {
		return err
	}
	handler := c.currentInternalCallbackHandler()
	if handler == nil {
		return nil
	}
	response, handlerErr := handler(ctx, bus.InternalCallbackRequest{
		Kind: menu.menu.Kind, Action: action, Value: value,
		OwnerID: menu.menu.OwnerID, Channel: menu.menu.Channel, Account: menu.menu.Account,
		ChatID: menu.menu.ChatID, TopicID: menu.menu.TopicID, MessageID: strconv.Itoa(message.MessageID),
		AgentID: menu.menu.AgentID, Scope: menu.menu.Scope, Inbound: menu.menu.Inbound,
		Page: menu.menu.Page, SessionKey: interactionMenuSessionState(menu.menu), Query: menu.menu.Query,
	})
	if handlerErr != nil {
		logger.WarnCF(
			"telegram", "Internal callback was rejected", map[string]any{"reason": "scope_or_state_validation"},
		)
		return handlerErr
	}
	if response == nil {
		return nil
	}
	if isInteractionPromptAction(kind, action) && strings.TrimSpace(value) == "" {
		return c.beginSessionRenamePrompt(ctx, query, token, menu, response.Text, action)
	}
	if response.Close {
		if err := c.clearSessionMenuKeyboard(ctx, message, menu); err != nil {
			return err
		}
		c.deleteSessionMenu(token)
		return nil
	}
	if response.Content == nil {
		return nil
	}
	if kind == "memory" {
		rebindMemoryInteractionRoute(response.Content.Interaction, menu.menu)
	}
	markup, pending, err := c.structuredReplyMarkup(response.Content, menu.chatID, menu.threadID)
	if err != nil {
		return err
	}
	if pending == nil {
		return nil
	}
	if err := c.editStructuredSessionMenu(ctx, message, menu, response.Content, markup); err != nil {
		return err
	}
	pending.messageID = menu.messageID
	pending.ephemeralID = menu.ephemeralID
	pending.receiverUserID = menu.receiverUserID
	c.replaceSessionMenu(token, *pending)
	return nil
}

func (c *TelegramChannel) answerSessionCallback(ctx context.Context, id, text string, alert bool) error {
	return c.bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: id, Text: text, ShowAlert: alert,
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
	if menu.receiverUserID > 0 && query.From.ID != menu.receiverUserID {
		return false
	}
	senderID := strconv.FormatInt(query.From.ID, 10)
	if !c.IsAllowedSender(bus.SenderInfo{
		Platform: "telegram", PlatformID: senderID, CanonicalID: "telegram:" + senderID,
		Username: query.From.Username, DisplayName: query.From.FirstName,
	}) {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(menu.menu.Kind))
	if kind == "memory" || kind == "skill" || kind == "checkpoint" {
		if err := validateMemoryInteractionEnvelope(menu.menu, menu.chatID, menu.threadID, c.Name()); err != nil {
			return false
		}
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
	kind := strings.ToLower(strings.TrimSpace(menu.Kind))
	if (kind == "memory" || kind == "skill" || kind == "checkpoint") && strings.HasPrefix(code, "e") {
		idx, err := strconv.Atoi(strings.TrimPrefix(code, "e"))
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
	case code == "d":
		return menuStaticAction(menu, "remove")
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
	useMarkdownV2 := c.tgCfg.UseMarkdownV2
	fallback := content.FallbackText()
	chunks := splitTelegramFormattedFallback(fallback, useMarkdownV2)
	if len(chunks) == 0 {
		chunks = []telegramFormattedFallbackChunk{{formatted: fallback, plain: fallback}}
	}
	if len(chunks) != 1 {
		return fmt.Errorf("interactive menu text exceeds Telegram edit limit")
	}
	formatted := chunks[0]
	if menu.ephemeralID > 0 {
		params := &telego.EditEphemeralMessageTextParams{
			ChatID: tu.ID(menu.chatID), ReceiverUserID: menu.receiverUserID,
			EphemeralMessageID: menu.ephemeralID, Text: formatted.formatted, ReplyMarkup: markup,
		}
		if useMarkdownV2 {
			params.ParseMode = telego.ModeMarkdownV2
		} else {
			params.ParseMode = telego.ModeHTML
		}
		err := c.bot.EditEphemeralMessageText(ctx, params)
		if err == nil || strings.Contains(strings.ToLower(errorString(err)), "message is not modified") {
			return nil
		}
		if isTelegramParseRejection(err) {
			params.Text = formatted.plain
			params.ParseMode = ""
			err = c.bot.EditEphemeralMessageText(ctx, params)
			if err == nil || strings.Contains(strings.ToLower(errorString(err)), "message is not modified") {
				return nil
			}
		}
		return err
	}
	if rich, ok := buildNativeRichMessage(content); ok {
		_, err := c.bot.EditMessageText(ctx, &telego.EditMessageTextParams{
			ChatID: tu.ID(menu.chatID), MessageID: message.MessageID, RichMessage: &rich, ReplyMarkup: markup,
		})
		if err == nil || strings.Contains(strings.ToLower(errorString(err)), "message is not modified") {
			return nil
		}
		if !isTelegramFormattingRejection(err) {
			return err
		}
	}
	params := &telego.EditMessageTextParams{
		ChatID: tu.ID(menu.chatID), MessageID: message.MessageID,
		Text: formatted.formatted, ReplyMarkup: markup,
	}
	if useMarkdownV2 {
		params.ParseMode = telego.ModeMarkdownV2
	} else {
		params.ParseMode = telego.ModeHTML
	}
	_, err := c.bot.EditMessageText(ctx, params)
	if err == nil || strings.Contains(strings.ToLower(errorString(err)), "message is not modified") {
		return nil
	}
	if isTelegramParseRejection(err) {
		params.Text = formatted.plain
		params.ParseMode = ""
		_, err = c.bot.EditMessageText(ctx, params)
		if err == nil || strings.Contains(strings.ToLower(errorString(err)), "message is not modified") {
			return nil
		}
	}
	return err
}

func (c *TelegramChannel) clearSessionMenuKeyboard(
	ctx context.Context, message *telego.Message, menu telegramSessionMenu,
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
