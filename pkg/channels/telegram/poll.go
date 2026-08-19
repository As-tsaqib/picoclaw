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

func (c *TelegramChannel) pollAccount(account string) string {
	if account = strings.TrimSpace(account); account != "" {
		return account
	}
	return c.Name()
}

func validatePollRouteFields(msg bus.OutboundMessage, poll *bus.PollPayload) error {
	if poll == nil {
		return fmt.Errorf("poll payload is nil")
	}
	chatType := strings.ToLower(strings.TrimSpace(msg.Context.ChatType))
	if (poll.MembersOnly || len(poll.CountryCodes) > 0) && chatType != "channel" {
		return fmt.Errorf("members_only and country_codes are supported only for Telegram channel chats")
	}
	if strings.EqualFold(strings.TrimSpace(poll.Mode), "quiz") && poll.AllowAddingOptions {
		return fmt.Errorf("allow_adding_options is not supported for quizzes")
	}
	if poll.AllowAddingOptions && poll.IsAnonymous {
		return fmt.Errorf("allow_adding_options is not supported for anonymous polls")
	}
	if poll.OpenPeriodSeconds != 0 && !poll.CloseAt.IsZero() {
		return fmt.Errorf("open_period and close_date are mutually exclusive")
	}
	return nil
}

func (c *TelegramChannel) sendPoll(
	ctx context.Context,
	msg bus.OutboundMessage,
	chatID int64,
	threadID int,
	ephemeral *telegramEphemeralTarget,
) ([]string, error) {
	if msg.Poll == nil {
		return nil, fmt.Errorf("telegram poll payload is missing")
	}
	if err := validatePollRouteFields(msg, msg.Poll); err != nil {
		return nil, err
	}
	if ephemeral != nil {
		fallback := formatPollFallbackText(msg.Poll)
		return c.sendStructuredFallback(ctx, chatID, threadID, msg.ReplyToMessageID, fallback, nil, nil, ephemeral)
	}

	poll := msg.Poll
	pollType := telego.PollTypeRegular
	if strings.EqualFold(strings.TrimSpace(poll.Mode), "quiz") {
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
	if !poll.CloseAt.IsZero() {
		params.CloseDate = poll.CloseAt.Unix()
	}
	if pollType == telego.PollTypeQuiz {
		params.CorrectOptionIDs = poll.CorrectOptionIDs
	}

	pMsg, err := c.bot.SendPoll(ctx, params)
	if err != nil {
		logger.WarnCF("telegram", "Native poll send failed, using deterministic text fallback", map[string]any{"error": "native_poll_unavailable"})
		serverID := ""
		if c.tgCfg != nil {
			serverID = c.tgCfg.BaseURL
		}
		feature := capability.FeaturePollRegular
		if pollType == telego.PollTypeQuiz {
			feature = capability.FeaturePollQuiz
		}
		capability.GlobalNegativeCache.RecordFailure("telegram", c.pollAccount(msg.Context.Account), serverID, feature, err)

		fallback := formatPollFallbackText(poll)
		var keyboard *telego.InlineKeyboardMarkup
		if pollType == telego.PollTypeQuiz {
			keyboard = tu.InlineKeyboard(tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("💡 Reveal Answer").WithCallbackData("quiz_reveal:" + poll.ID),
			))
		}
		ids, fallbackErr := c.sendStructuredFallback(ctx, chatID, threadID, msg.ReplyToMessageID, fallback, keyboard, nil, nil)
		if fallbackErr != nil {
			return nil, fallbackErr
		}
		if pollType == telego.PollTypeQuiz && len(ids) > 0 {
			messageID, parseErr := strconv.Atoi(ids[0])
			if parseErr == nil && messageID > 0 {
				c.registerPollEntry(telegramPollEntry{
					LocalHandle: poll.ID, Account: c.pollAccount(msg.Context.Account), ChatID: chatID, ThreadID: threadID,
					MessageID: messageID, AgentID: msg.AgentID, SenderID: msg.Context.SenderID,
					SessionKey: msg.SessionKey, PollPayload: poll, CreatedAt: time.Now().UTC(),
				})
			}
		}
		return ids, nil
	}

	if pMsg == nil {
		return nil, fmt.Errorf("telegram send poll returned no message")
	}
	tgPollID := ""
	if pMsg.Poll != nil {
		tgPollID = pMsg.Poll.ID
	}
	c.registerPollEntry(telegramPollEntry{
		LocalHandle: poll.ID, TelegramPollID: tgPollID, Account: c.pollAccount(msg.Context.Account),
		ChatID: chatID, ThreadID: threadID, MessageID: pMsg.MessageID,
		AgentID: msg.AgentID, SenderID: msg.Context.SenderID, SessionKey: msg.SessionKey,
		PollPayload: poll, CreatedAt: time.Now().UTC(),
	})
	return []string{strconv.Itoa(pMsg.MessageID)}, nil
}
