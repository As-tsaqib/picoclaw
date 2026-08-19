package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/capability"
	"github.com/As-tsaqib/picoclaw/pkg/logger"
)

var allowedDiceEmoji = map[string]bool{
	"🎲": true,
	"🎯": true,
	"🏀": true,
	"⚽": true,
	"🎳": true,
	"🎰": true,
}

func (c *TelegramChannel) sendLocation(
	ctx context.Context,
	msg bus.OutboundMessage,
	chatID int64,
	threadID int,
	ephemeral *telegramEphemeralTarget,
) ([]string, error) {
	loc := msg.Location
	if loc == nil {
		return nil, nil
	}

	if loc.Latitude < -90 || loc.Latitude > 90 || loc.Longitude < -180 || loc.Longitude > 180 {
		return nil, fmt.Errorf("invalid coordinates: lat=%f, lon=%f", loc.Latitude, loc.Longitude)
	}

	if ephemeral != nil {
		fallback := fmt.Sprintf("📍 Location: %f, %f", loc.Latitude, loc.Longitude)
		if loc.Title != "" {
			fallback = fmt.Sprintf("📍 %s\n%s\nCoordinates: %f, %f", loc.Title, loc.Address, loc.Latitude, loc.Longitude)
		}
		return c.sendStructuredFallback(ctx, chatID, threadID, msg.ReplyToMessageID, fallback, nil, nil, ephemeral)
	}

	serverID := ""
	if c.tgCfg != nil {
		serverID = c.tgCfg.BaseURL
	}

	if loc.Title != "" {
		params := &telego.SendVenueParams{
			ChatID:          tu.ID(chatID),
			MessageThreadID: threadID,
			Latitude:        loc.Latitude,
			Longitude:       loc.Longitude,
			Title:           loc.Title,
			Address:         loc.Address,
			ReplyParameters: ephemeralReplyParameters(nil, msg.ReplyToMessageID),
		}
		pMsg, err := c.bot.SendVenue(ctx, params)
		if err != nil {
			capability.GlobalNegativeCache.RecordFailure(
				"telegram",
				msg.Context.Account,
				serverID,
				capability.FeatureLocationVenue,
				err,
			)
			logger.WarnCF(
				"telegram",
				"Native venue send failed, using text fallback",
				map[string]any{"error": err.Error()},
			)
			fallback := fmt.Sprintf(
				"📍 %s\n%s\nCoordinates: %f, %f",
				loc.Title,
				loc.Address,
				loc.Latitude,
				loc.Longitude,
			)
			return c.sendStructuredFallback(ctx, chatID, threadID, msg.ReplyToMessageID, fallback, nil, nil, ephemeral)
		}
		return []string{strconv.Itoa(pMsg.MessageID)}, nil
	}

	params := &telego.SendLocationParams{
		ChatID:          tu.ID(chatID),
		MessageThreadID: threadID,
		Latitude:        loc.Latitude,
		Longitude:       loc.Longitude,
		ReplyParameters: ephemeralReplyParameters(nil, msg.ReplyToMessageID),
	}
	pMsg, err := c.bot.SendLocation(ctx, params)
	if err != nil {
		capability.GlobalNegativeCache.RecordFailure(
			"telegram",
			msg.Context.Account,
			serverID,
			capability.FeatureLocationPoint,
			err,
		)
		logger.WarnCF(
			"telegram",
			"Native location send failed, using text fallback",
			map[string]any{"error": err.Error()},
		)
		fallback := fmt.Sprintf("📍 Location: %f, %f", loc.Latitude, loc.Longitude)
		return c.sendStructuredFallback(ctx, chatID, threadID, msg.ReplyToMessageID, fallback, nil, nil, ephemeral)
	}
	return []string{strconv.Itoa(pMsg.MessageID)}, nil
}

func (c *TelegramChannel) sendContact(
	ctx context.Context,
	msg bus.OutboundMessage,
	chatID int64,
	threadID int,
	ephemeral *telegramEphemeralTarget,
) ([]string, error) {
	contact := msg.Contact
	if contact == nil {
		return nil, nil
	}

	if ephemeral != nil {
		fallback := fmt.Sprintf("👤 Contact: %s %s (%s)", contact.FirstName, contact.LastName, contact.PhoneNumber)
		return c.sendStructuredFallback(ctx, chatID, threadID, msg.ReplyToMessageID, fallback, nil, nil, ephemeral)
	}

	params := &telego.SendContactParams{
		ChatID:          tu.ID(chatID),
		MessageThreadID: threadID,
		PhoneNumber:     contact.PhoneNumber,
		FirstName:       contact.FirstName,
		LastName:        contact.LastName,
		Vcard:           contact.VCard,
		ReplyParameters: ephemeralReplyParameters(nil, msg.ReplyToMessageID),
	}
	pMsg, err := c.bot.SendContact(ctx, params)
	if err != nil {
		serverID := ""
		if c.tgCfg != nil {
			serverID = c.tgCfg.BaseURL
		}
		capability.GlobalNegativeCache.RecordFailure(
			"telegram",
			msg.Context.Account,
			serverID,
			capability.FeatureContactCard,
			err,
		)
		logger.WarnCF(
			"telegram",
			"Native contact send failed, using text fallback",
			map[string]any{"error": err.Error()},
		)
		fallback := fmt.Sprintf("👤 Contact: %s %s (%s)", contact.FirstName, contact.LastName, contact.PhoneNumber)
		return c.sendStructuredFallback(ctx, chatID, threadID, msg.ReplyToMessageID, fallback, nil, nil, ephemeral)
	}
	return []string{strconv.Itoa(pMsg.MessageID)}, nil
}

func (c *TelegramChannel) sendDice(
	ctx context.Context,
	msg bus.OutboundMessage,
	chatID int64,
	threadID int,
	ephemeral *telegramEphemeralTarget,
) ([]string, error) {
	dice := msg.Dice
	if dice == nil {
		return nil, nil
	}

	emoji := strings.TrimSpace(dice.Emoji)
	if emoji == "" || !allowedDiceEmoji[emoji] {
		emoji = "🎲"
	}

	if ephemeral != nil {
		fallback := fmt.Sprintf("🎲 Rolling dice: %s", emoji)
		return c.sendStructuredFallback(ctx, chatID, threadID, msg.ReplyToMessageID, fallback, nil, nil, ephemeral)
	}

	params := &telego.SendDiceParams{
		ChatID:          tu.ID(chatID),
		MessageThreadID: threadID,
		Emoji:           emoji,
		ReplyParameters: ephemeralReplyParameters(nil, msg.ReplyToMessageID),
	}
	pMsg, err := c.bot.SendDice(ctx, params)
	if err != nil {
		serverID := ""
		if c.tgCfg != nil {
			serverID = c.tgCfg.BaseURL
		}
		capability.GlobalNegativeCache.RecordFailure(
			"telegram",
			msg.Context.Account,
			serverID,
			capability.FeatureDiceAnimated,
			err,
		)
		logger.WarnCF(
			"telegram",
			"Native dice send failed, using text fallback",
			map[string]any{"error": err.Error()},
		)
		fallback := fmt.Sprintf("🎲 Rolled: %s", emoji)
		return c.sendStructuredFallback(ctx, chatID, threadID, msg.ReplyToMessageID, fallback, nil, nil, ephemeral)
	}
	return []string{strconv.Itoa(pMsg.MessageID)}, nil
}
