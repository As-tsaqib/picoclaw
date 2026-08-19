package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/capability"
	"github.com/As-tsaqib/picoclaw/pkg/logger"
)

func formatPollFallbackText(poll *bus.PollPayload) string {
	if poll == nil {
		return ""
	}
	if strings.TrimSpace(poll.FallbackText) != "" {
		return poll.FallbackText
	}
	if strings.EqualFold(strings.TrimSpace(poll.Mode), "quiz") {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📝 Quiz: %s\n", poll.Question))
		for i, opt := range poll.Options {
			sb.WriteString(fmt.Sprintf("\n%d. %s", i+1, opt))
		}
		return sb.String()
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 Poll: %s\n", poll.Question))
	for i, opt := range poll.Options {
		sb.WriteString(fmt.Sprintf("\n%d. %s", i+1, opt))
	}
	return sb.String()
}

func formatQuizRevealText(poll *bus.PollPayload) string {
	if poll == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📝 Quiz: %s\n", poll.Question))
	for i, opt := range poll.Options {
		sb.WriteString(fmt.Sprintf("\n%d. %s", i+1, opt))
	}
	if len(poll.CorrectOptionIDs) > 0 {
		var correctStrs []string
		for _, id := range poll.CorrectOptionIDs {
			if id >= 0 && id < len(poll.Options) {
				correctStrs = append(correctStrs, fmt.Sprintf("%d. %s", id+1, poll.Options[id]))
			} else {
				correctStrs = append(correctStrs, fmt.Sprintf("%d", id+1))
			}
		}
		sb.WriteString(fmt.Sprintf("\n\n✅ Correct answer: %s", strings.Join(correctStrs, ", ")))
	}
	if poll.Explanation != "" {
		sb.WriteString(fmt.Sprintf("\n💡 Explanation: %s", poll.Explanation))
	}
	return sb.String()
}

func (c *TelegramChannel) sendPoll(
	ctx context.Context,
	msg bus.OutboundMessage,
	chatID int64,
	threadID int,
	ephemeral *telegramEphemeralTarget,
) ([]string, error) {
	if ephemeral != nil {
		// Ephemeral polls are not natively supported by Telegram's ephemeral methods.
		// Fallback to structured text
		fallback := formatPollFallbackText(msg.Poll)
		return c.sendStructuredFallback(ctx, chatID, threadID, msg.ReplyToMessageID, fallback, nil, nil, ephemeral)
	}

	poll := msg.Poll
	pollType := telego.PollTypeRegular
	if strings.ToLower(poll.Mode) == "quiz" {
		pollType = telego.PollTypeQuiz
	}

	options := make([]telego.InputPollOption, len(poll.Options))
	for i, opt := range poll.Options {
		options[i] = telego.InputPollOption{Text: opt}
	}

	allowsMultiple := poll.AllowsMultipleAnswers
	if pollType == telego.PollTypeQuiz && len(poll.CorrectOptionIDs) > 1 {
		allowsMultiple = true
	}

	params := &telego.SendPollParams{
		ChatID:                 tu.ID(chatID),
		MessageThreadID:        threadID,
		Question:               poll.Question,
		Options:                options,
		Type:                   pollType,
		IsAnonymous:            &poll.IsAnonymous,
		AllowsMultipleAnswers:  allowsMultiple,
		AllowsRevoting:         poll.AllowsRevoting,
		ShuffleOptions:         poll.ShuffleOptions,
		AllowAddingOptions:     poll.AllowAddingOptions,
		HideResultsUntilCloses: poll.HideResultsUntilCloses,
		MembersOnly:            poll.MembersOnly,
		CountryCodes:           poll.CountryCodes,
		Description:            poll.Description,
		ReplyParameters:        ephemeralReplyParameters(nil, msg.ReplyToMessageID),
		Explanation:            poll.Explanation,
		OpenPeriod:             poll.OpenPeriodSeconds,
		IsClosed:               poll.IsClosed,
	}

	if pollType == telego.PollTypeQuiz {
		params.CorrectOptionIDs = poll.CorrectOptionIDs
	}

	pMsg, err := c.bot.SendPoll(ctx, params)
	if err != nil {
		logger.WarnCF("telegram", "Native poll send failed, using text fallback", map[string]any{
			"chat_id": chatID,
			"error":   err.Error(),
		})
		serverID := ""
		if c.tgCfg != nil {
			serverID = c.tgCfg.BaseURL
		}
		if pollType == telego.PollTypeQuiz {
			capability.GlobalNegativeCache.RecordFailure(
				"telegram",
				msg.Context.Account,
				serverID,
				capability.FeaturePollQuiz,
				err,
			)
		} else {
			capability.GlobalNegativeCache.RecordFailure(
				"telegram",
				msg.Context.Account,
				serverID,
				capability.FeaturePollRegular,
				err,
			)
		}
		fallback := formatPollFallbackText(msg.Poll)
		var keyboard *telego.InlineKeyboardMarkup
		if pollType == telego.PollTypeQuiz {
			c.registerPollEntry(telegramPollEntry{
				LocalHandle: msg.Poll.ID,
				Account:     msg.Context.Account,
				ChatID:      chatID,
				ThreadID:    threadID,
				AgentID:     msg.AgentID,
				SenderID:    msg.Context.SenderID,
				SessionKey:  msg.SessionKey,
				PollPayload: msg.Poll,
				CreatedAt:   time.Now().UTC(),
			})
			keyboard = tu.InlineKeyboard(
				tu.InlineKeyboardRow(
					tu.InlineKeyboardButton("💡 Reveal Answer").WithCallbackData("quiz_reveal:" + msg.Poll.ID),
				),
			)
		}
		return c.sendStructuredFallback(ctx, chatID, threadID, msg.ReplyToMessageID, fallback, keyboard, nil, ephemeral)
	}

	tgPollID := ""
	if pMsg.Poll != nil {
		tgPollID = pMsg.Poll.ID
	}
	c.registerPollEntry(telegramPollEntry{
		LocalHandle:    msg.Poll.ID,
		TelegramPollID: tgPollID,
		Account:        msg.Context.Account,
		ChatID:         chatID,
		ThreadID:       threadID,
		MessageID:      pMsg.MessageID,
		AgentID:        msg.AgentID,
		SenderID:       msg.Context.SenderID,
		SessionKey:     msg.SessionKey,
		PollPayload:    msg.Poll,
		CreatedAt:      time.Now().UTC(),
	})

	return []string{strconv.Itoa(pMsg.MessageID)}, nil
}
