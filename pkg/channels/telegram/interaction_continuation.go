package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

// sendStructuredInteractionContinuation is the strict delivery contract used
// by InteractionAppendContinuation. Ordinary structured sends may degrade to
// non-interactive output, but a continuation must establish one new actionable
// server-side menu before its caller is allowed to retire the previous menu.
func (c *TelegramChannel) sendStructuredInteractionContinuation(
	ctx context.Context,
	msg bus.OutboundMessage,
	chatID int64,
	threadID int,
	ephemeral *telegramEphemeralTarget,
) ([]string, error) {
	content := msg.Structured.Clone()
	if content == nil || content.Interaction == nil {
		return nil, fmt.Errorf("interaction continuation has no interaction menu")
	}

	// Validate the interaction before transport so deterministic menu/keyboard
	// failures cannot produce a visible but dead continuation card.
	_, expected, err := c.structuredReplyMarkup(content, chatID, threadID)
	if err != nil {
		return nil, fmt.Errorf("prepare interaction continuation: %w", err)
	}
	if expected == nil {
		return nil, fmt.Errorf("prepare interaction continuation: no menu candidate")
	}

	before := c.snapshotSessionMenuTokens()
	messageIDs, err := c.sendStructuredContent(ctx, msg, chatID, threadID, ephemeral)
	if err != nil {
		return messageIDs, err
	}

	registered := c.newContinuationMenus(before, *expected)
	if len(registered) != 1 {
		c.deleteContinuationMenus(registered)
		return messageIDs, fmt.Errorf("interaction continuation registration failed")
	}
	if err := validateContinuationRegistration(registered[0], *expected, ephemeral); err != nil {
		c.deleteContinuationMenus(registered)
		return messageIDs, err
	}
	if len(messageIDs) != 1 {
		c.deleteContinuationMenus(registered)
		return messageIDs, fmt.Errorf("interaction continuation returned an unexpected message identity count")
	}
	if ephemeral == nil {
		messageID, parseErr := strconv.Atoi(strings.TrimSpace(messageIDs[0]))
		if parseErr != nil || messageID != registered[0].messageID {
			c.deleteContinuationMenus(registered)
			return messageIDs, fmt.Errorf("interaction continuation message binding mismatch")
		}
	}
	return messageIDs, nil
}

func (c *TelegramChannel) snapshotSessionMenuTokens() map[string]struct{} {
	c.sessionMenuMu.Lock()
	defer c.sessionMenuMu.Unlock()
	c.pruneSessionMenusLocked(time.Now())
	tokens := make(map[string]struct{}, len(c.sessionMenus))
	for token := range c.sessionMenus {
		tokens[token] = struct{}{}
	}
	return tokens
}

func (c *TelegramChannel) newContinuationMenus(
	before map[string]struct{},
	expected telegramSessionMenu,
) []telegramSessionMenu {
	c.sessionMenuMu.Lock()
	defer c.sessionMenuMu.Unlock()
	c.pruneSessionMenusLocked(time.Now())
	menus := make([]telegramSessionMenu, 0, 1)
	for token, candidate := range c.sessionMenus {
		if _, existed := before[token]; existed {
			continue
		}
		if candidate.chatID != expected.chatID || candidate.threadID != expected.threadID ||
			!continuationMenuStateMatches(candidate.menu, expected.menu) {
			continue
		}
		menus = append(menus, candidate)
	}
	return menus
}

func continuationMenuStateMatches(actual, expected bus.InteractionMenu) bool {
	return strings.EqualFold(strings.TrimSpace(actual.Kind), strings.TrimSpace(expected.Kind)) &&
		strings.TrimSpace(actual.OwnerID) == strings.TrimSpace(expected.OwnerID) &&
		strings.TrimSpace(actual.Channel) == strings.TrimSpace(expected.Channel) &&
		strings.TrimSpace(actual.Account) == strings.TrimSpace(expected.Account) &&
		strings.TrimSpace(actual.ChatID) == strings.TrimSpace(expected.ChatID) &&
		strings.TrimSpace(actual.TopicID) == strings.TrimSpace(expected.TopicID) &&
		strings.EqualFold(strings.TrimSpace(actual.AgentID), strings.TrimSpace(expected.AgentID)) &&
		strings.TrimSpace(actual.Scope) == strings.TrimSpace(expected.Scope) &&
		strings.TrimSpace(actual.SessionKey) == strings.TrimSpace(expected.SessionKey) &&
		strings.TrimSpace(actual.Query) == strings.TrimSpace(expected.Query) &&
		actual.Page == expected.Page && actual.Pages == expected.Pages
}

func validateContinuationRegistration(
	registered telegramSessionMenu,
	expected telegramSessionMenu,
	ephemeral *telegramEphemeralTarget,
) error {
	if strings.TrimSpace(registered.token) == "" || !continuationMenuStateMatches(registered.menu, expected.menu) {
		return fmt.Errorf("interaction continuation registered invalid menu state")
	}
	if registered.chatID != expected.chatID || registered.threadID != expected.threadID {
		return fmt.Errorf("interaction continuation registered invalid route")
	}
	if ephemeral != nil {
		if registered.ephemeralID <= 0 || registered.messageID > 0 ||
			registered.receiverUserID <= 0 || registered.receiverUserID != ephemeral.ReceiverUserID {
			return fmt.Errorf("interaction continuation private message binding is invalid")
		}
		return nil
	}
	if registered.messageID <= 0 || registered.ephemeralID > 0 || registered.receiverUserID > 0 {
		return fmt.Errorf("interaction continuation public message binding is invalid")
	}
	return nil
}

func (c *TelegramChannel) deleteContinuationMenus(menus []telegramSessionMenu) {
	for _, menu := range menus {
		if strings.TrimSpace(menu.token) != "" {
			c.deleteSessionMenu(menu.token)
		}
	}
}
