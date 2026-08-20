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

	messageIDs, err := c.sendStructuredContent(ctx, msg, chatID, threadID, ephemeral)
	if err != nil {
		return messageIDs, err
	}
	if len(messageIDs) != 1 {
		c.deleteMenusForContinuationIdentity(messageIDs, ephemeral)
		return messageIDs, fmt.Errorf("interaction continuation returned an unexpected message identity count")
	}

	registered, err := c.registeredContinuationForIdentity(messageIDs[0], *expected, ephemeral)
	if err != nil {
		c.deleteMenusForContinuationIdentity(messageIDs, ephemeral)
		return messageIDs, err
	}
	if err := validateContinuationRegistration(registered, *expected, ephemeral); err != nil {
		c.deleteMenusForContinuationIdentity(messageIDs, ephemeral)
		return messageIDs, err
	}
	return messageIDs, nil
}

func (c *TelegramChannel) registeredContinuationForIdentity(
	messageID string,
	expected telegramSessionMenu,
	ephemeral *telegramEphemeralTarget,
) (telegramSessionMenu, error) {
	publicID, privateID, err := continuationPlatformIdentity(messageID, ephemeral)
	if err != nil {
		return telegramSessionMenu{}, err
	}

	c.sessionMenuMu.Lock()
	defer c.sessionMenuMu.Unlock()
	c.pruneSessionMenusLocked(time.Now())
	var matched telegramSessionMenu
	matches := 0
	for _, candidate := range c.sessionMenus {
		if candidate.chatID != expected.chatID || candidate.threadID != expected.threadID {
			continue
		}
		if ephemeral == nil {
			if candidate.messageID != publicID || candidate.ephemeralID > 0 {
				continue
			}
		} else if candidate.ephemeralID != privateID || candidate.messageID > 0 ||
			candidate.receiverUserID != ephemeral.ReceiverUserID {
			continue
		}
		matches++
		matched = candidate
	}
	if matches != 1 {
		return telegramSessionMenu{}, fmt.Errorf("interaction continuation registration failed")
	}
	return matched, nil
}

func continuationPlatformIdentity(
	messageID string,
	ephemeral *telegramEphemeralTarget,
) (publicID int, privateID int, err error) {
	if ephemeral == nil {
		publicID, err = strconv.Atoi(strings.TrimSpace(messageID))
		if err != nil || publicID <= 0 {
			return 0, 0, fmt.Errorf("interaction continuation public message identity is invalid")
		}
		return publicID, 0, nil
	}
	token, privateID, ok := parseEphemeralMessageID(strings.TrimSpace(messageID))
	if !ok || token != ephemeral.Token || privateID <= 0 {
		return 0, 0, fmt.Errorf("interaction continuation private message identity is invalid")
	}
	return 0, privateID, nil
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

func (c *TelegramChannel) deleteMenusForContinuationIdentity(
	messageIDs []string,
	ephemeral *telegramEphemeralTarget,
) {
	if len(messageIDs) != 1 {
		return
	}
	publicID, privateID, err := continuationPlatformIdentity(messageIDs[0], ephemeral)
	if err != nil {
		return
	}
	c.sessionMenuMu.Lock()
	defer c.sessionMenuMu.Unlock()
	for token, menu := range c.sessionMenus {
		if ephemeral == nil {
			if menu.messageID == publicID && menu.ephemeralID <= 0 {
				delete(c.sessionMenus, token)
			}
			continue
		}
		if menu.ephemeralID == privateID && menu.messageID <= 0 && menu.receiverUserID == ephemeral.ReceiverUserID {
			delete(c.sessionMenus, token)
		}
	}
}
