package telegram

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/capability"
	"github.com/As-tsaqib/picoclaw/pkg/logger"
)

const (
	maxPollRegistryEntries = 1000
	pollRegistryTTL        = 30 * 24 * time.Hour
)

type telegramPollVote struct {
	OptionIDs           []int
	OptionPersistentIDs []string
	UpdatedAt           time.Time
}

type telegramPollEntry struct {
	LocalHandle    string
	TelegramPollID string
	Account        string
	ChatID         int64
	ThreadID       int
	MessageID      int
	AgentID        string
	SenderID       string
	SessionKey     string
	IsClosed       bool
	CreatedAt      time.Time
	PollPayload    *bus.PollPayload
	Votes          map[string]telegramPollVote
	RevealPending  bool
	RevealConsumed bool
}

type telegramPollRoute struct {
	Account    string
	ChatID     int64
	ThreadID   int
	AgentID    string
	SenderID   string
	SessionKey string
}

func (c *TelegramChannel) ensurePollRegistryLocked() {
	if c.pollRegistry == nil {
		c.pollRegistry = make(map[string]telegramPollEntry)
	}
	if c.pollByTgID == nil {
		c.pollByTgID = make(map[string]string)
	}
}

func (c *TelegramChannel) prunePollsLocked(now time.Time) {
	c.ensurePollRegistryLocked()
	cutoff := now.Add(-pollRegistryTTL)
	closedCutoff := now.Add(-24 * time.Hour)
	for handle, entry := range c.pollRegistry {
		if entry.CreatedAt.Before(cutoff) || (entry.IsClosed && entry.CreatedAt.Before(closedCutoff)) {
			delete(c.pollRegistry, handle)
			if entry.TelegramPollID != "" {
				delete(c.pollByTgID, entry.TelegramPollID)
			}
		}
	}
	for len(c.pollRegistry) >= maxPollRegistryEntries {
		var oldestHandle string
		var oldestTime time.Time
		for handle, entry := range c.pollRegistry {
			if oldestHandle == "" || entry.CreatedAt.Before(oldestTime) {
				oldestHandle = handle
				oldestTime = entry.CreatedAt
			}
		}
		if oldestHandle == "" {
			break
		}
		if entry, ok := c.pollRegistry[oldestHandle]; ok && entry.TelegramPollID != "" {
			delete(c.pollByTgID, entry.TelegramPollID)
		}
		delete(c.pollRegistry, oldestHandle)
	}
}

func (c *TelegramChannel) registerPollEntry(entry telegramPollEntry) {
	c.pollRegistryMu.Lock()
	defer c.pollRegistryMu.Unlock()
	now := time.Now().UTC()
	c.prunePollsLocked(now)
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if strings.TrimSpace(entry.Account) == "" {
		entry.Account = c.Name()
	}
	if entry.Votes == nil {
		entry.Votes = make(map[string]telegramPollVote)
	}
	c.pollRegistry[entry.LocalHandle] = entry
	if entry.TelegramPollID != "" {
		c.pollByTgID[entry.TelegramPollID] = entry.LocalHandle
	}
}

func (c *TelegramChannel) resolvePollByLocalHandle(handle string) (telegramPollEntry, bool) {
	c.pollRegistryMu.Lock()
	defer c.pollRegistryMu.Unlock()
	c.ensurePollRegistryLocked()
	entry, ok := c.pollRegistry[handle]
	return entry, ok
}

func (c *TelegramChannel) resolvePollByTgPollID(tgPollID string) (telegramPollEntry, bool) {
	c.pollRegistryMu.Lock()
	defer c.pollRegistryMu.Unlock()
	c.ensurePollRegistryLocked()
	handle, ok := c.pollByTgID[tgPollID]
	if !ok {
		return telegramPollEntry{}, false
	}
	entry, ok := c.pollRegistry[handle]
	return entry, ok
}

func (c *TelegramChannel) updatePollStateByTgPollID(tgPollID string, isClosed bool) {
	c.pollRegistryMu.Lock()
	defer c.pollRegistryMu.Unlock()
	c.ensurePollRegistryLocked()
	handle, ok := c.pollByTgID[tgPollID]
	if !ok {
		return
	}
	if entry, exists := c.pollRegistry[handle]; exists {
		entry.IsClosed = isClosed
		c.pollRegistry[handle] = entry
	}
}

func pollRouteAuthorized(entry telegramPollEntry, route telegramPollRoute) error {
	if want, got := strings.TrimSpace(entry.Account), strings.TrimSpace(route.Account); want != "" && got != want {
		return fmt.Errorf("poll route account mismatch")
	}
	if want, got := strings.TrimSpace(entry.AgentID), strings.TrimSpace(route.AgentID); want != "" && got != want {
		return fmt.Errorf("caller agent %q not authorized", got)
	}
	if want, got := strings.TrimSpace(
		entry.SessionKey,
	), strings.TrimSpace(
		route.SessionKey,
	); want != "" &&
		got != want {
		return fmt.Errorf("caller session %q not authorized", got)
	}
	if want, got := strings.TrimSpace(entry.SenderID), strings.TrimSpace(route.SenderID); want != "" && got != want {
		return fmt.Errorf("caller sender %q not authorized", got)
	}
	if entry.ChatID != route.ChatID {
		return fmt.Errorf("poll route chat mismatch")
	}
	if entry.ThreadID != route.ThreadID {
		return fmt.Errorf("poll route topic mismatch")
	}
	return nil
}

func pollEntryRouteDigest(entry telegramPollEntry) string {
	topicID := ""
	if entry.ThreadID != 0 {
		topicID = strconv.Itoa(entry.ThreadID)
	}
	return bus.PollStopRouteDigest(
		entry.Account,
		strconv.FormatInt(entry.ChatID, 10),
		topicID,
		entry.AgentID,
		"",
		entry.SessionKey,
	)
}

func stopPollHandleForEntry(value string, entry telegramPollEntry) (string, error) {
	handle, digest, bound := bus.ParsePollStopRouteToken(value)
	if !bound {
		return value, nil
	}
	expected := pollEntryRouteDigest(entry)
	if subtle.ConstantTimeCompare([]byte(digest), []byte(expected)) != 1 {
		return "", fmt.Errorf("poll route proof mismatch")
	}
	return handle, nil
}

// StopPoll preserves the existing channel interface while validating both the
// trusted runtime principal and, for semantic-tool calls, a one-way proof of
// account/chat/topic/agent/session scope. The model can only supply the opaque
// poll handle; PicoClaw injects the route proof after tool argument parsing.
func (c *TelegramChannel) StopPoll(
	ctx context.Context,
	localHandle string,
	callerAgentID string,
	callerSessionKey string,
	callerSenderID string,
) error {
	lookupHandle := strings.TrimSpace(localHandle)
	if handle, _, ok := bus.ParsePollStopRouteToken(lookupHandle); ok {
		lookupHandle = handle
	}
	entry, ok := c.resolvePollByLocalHandle(lookupHandle)
	if !ok {
		return fmt.Errorf("poll %q not found or expired", lookupHandle)
	}
	verifiedHandle, err := stopPollHandleForEntry(localHandle, entry)
	if err != nil || verifiedHandle != lookupHandle {
		return fmt.Errorf("not authorized to stop poll: poll route proof mismatch")
	}
	return c.stopPollEntry(ctx, lookupHandle, entry, telegramPollRoute{
		Account:    entry.Account,
		ChatID:     entry.ChatID,
		ThreadID:   entry.ThreadID,
		AgentID:    callerAgentID,
		SessionKey: callerSessionKey,
		SenderID:   callerSenderID,
	})
}

func (c *TelegramChannel) StopPollForRoute(
	ctx context.Context,
	localHandle string,
	route telegramPollRoute,
) error {
	lookupHandle := strings.TrimSpace(localHandle)
	if handle, _, ok := bus.ParsePollStopRouteToken(lookupHandle); ok {
		lookupHandle = handle
	}
	entry, ok := c.resolvePollByLocalHandle(lookupHandle)
	if !ok {
		return fmt.Errorf("poll %q not found or expired", lookupHandle)
	}
	verifiedHandle, err := stopPollHandleForEntry(localHandle, entry)
	if err != nil || verifiedHandle != lookupHandle {
		return fmt.Errorf("not authorized to stop poll: poll route proof mismatch")
	}
	return c.stopPollEntry(ctx, lookupHandle, entry, route)
}

func (c *TelegramChannel) stopPollEntry(
	ctx context.Context,
	localHandle string,
	entry telegramPollEntry,
	route telegramPollRoute,
) error {
	if err := pollRouteAuthorized(entry, route); err != nil {
		return fmt.Errorf("not authorized to stop poll: %w", err)
	}
	_, err := c.bot.StopPoll(ctx, &telego.StopPollParams{
		ChatID:    tu.ID(entry.ChatID),
		MessageID: entry.MessageID,
	})
	if err != nil {
		serverID := ""
		if c.tgCfg != nil {
			serverID = c.tgCfg.BaseURL
		}
		capability.GlobalNegativeCache.RecordFailure(
			"telegram",
			entry.Account,
			serverID,
			capability.FeaturePollStop,
			err,
		)
		return fmt.Errorf("telegram stop poll failed: %w", err)
	}
	c.pollRegistryMu.Lock()
	delete(c.pollRegistry, localHandle)
	if entry.TelegramPollID != "" {
		delete(c.pollByTgID, entry.TelegramPollID)
	}
	c.pollRegistryMu.Unlock()
	return nil
}

func (c *TelegramChannel) handlePollUpdate(_ *th.Context, poll *telego.Poll) error {
	if poll == nil {
		return nil
	}
	c.updatePollStateByTgPollID(poll.ID, poll.IsClosed)
	logger.DebugCF("telegram", "Received poll update", map[string]any{"is_closed": poll.IsClosed})
	return nil
}

func pollAnswerIdentity(answer *telego.PollAnswer) (string, bool) {
	if answer == nil {
		return "", false
	}
	if answer.User != nil && answer.User.ID > 0 {
		return "user:" + strconv.FormatInt(answer.User.ID, 10), true
	}
	if answer.VoterChat != nil && answer.VoterChat.ID != 0 {
		return "chat:" + strconv.FormatInt(answer.VoterChat.ID, 10), true
	}
	return "", false
}

func (c *TelegramChannel) recordPollAnswer(answer *telego.PollAnswer) (bool, bool) {
	identity, identifiable := pollAnswerIdentity(answer)
	if answer == nil || !identifiable {
		return false, false
	}
	c.pollRegistryMu.Lock()
	defer c.pollRegistryMu.Unlock()
	c.ensurePollRegistryLocked()
	handle, ok := c.pollByTgID[answer.PollID]
	if !ok {
		return false, false
	}
	entry, ok := c.pollRegistry[handle]
	if !ok {
		return false, false
	}
	if entry.Votes == nil {
		entry.Votes = make(map[string]telegramPollVote)
	}
	retracted := len(answer.OptionIDs) == 0 && len(answer.OptionPersistentIDs) == 0
	if retracted {
		delete(entry.Votes, identity)
	} else {
		entry.Votes[identity] = telegramPollVote{
			OptionIDs: append([]int(nil), answer.OptionIDs...),
			OptionPersistentIDs: append(
				[]string(nil),
				answer.OptionPersistentIDs...,
			),
			UpdatedAt: time.Now().UTC(),
		}
	}
	c.pollRegistry[handle] = entry
	return true, retracted
}

func (c *TelegramChannel) handlePollAnswerUpdate(_ *th.Context, answer *telego.PollAnswer) error {
	if answer == nil {
		return nil
	}
	found, retracted := c.recordPollAnswer(answer)
	if !found {
		logger.DebugCF(
			"telegram",
			"Ignored poll answer without registered poll and trusted voter identity",
			nil,
		)
		return nil
	}
	logger.DebugCF("telegram", "Recorded poll answer", map[string]any{
		"option_count":            len(answer.OptionIDs),
		"persistent_option_count": len(answer.OptionPersistentIDs),
		"retracted":               retracted,
	})
	return nil
}

func (c *TelegramChannel) claimQuizReveal(
	query *telego.CallbackQuery,
	handle string,
) (telegramPollEntry, error) {
	if query == nil || query.Message == nil || query.From.IsBot || query.From.ID <= 0 {
		return telegramPollEntry{}, fmt.Errorf("invalid callback envelope")
	}
	message := query.Message.Message()
	if message == nil {
		return telegramPollEntry{}, fmt.Errorf("callback message unavailable")
	}
	c.pollRegistryMu.Lock()
	defer c.pollRegistryMu.Unlock()
	c.ensurePollRegistryLocked()
	entry, ok := c.pollRegistry[handle]
	if !ok || entry.PollPayload == nil {
		return telegramPollEntry{}, fmt.Errorf("quiz not found or expired")
	}
	if entry.RevealConsumed || entry.RevealPending {
		return telegramPollEntry{}, fmt.Errorf("quiz reveal already processed")
	}
	if strings.TrimSpace(entry.Account) != "" &&
		strings.TrimSpace(entry.Account) != strings.TrimSpace(c.Name()) {
		return telegramPollEntry{}, fmt.Errorf("callback account mismatch")
	}
	if message.Chat.ID != entry.ChatID || message.MessageThreadID != entry.ThreadID {
		return telegramPollEntry{}, fmt.Errorf("callback chat or topic mismatch")
	}
	if entry.MessageID <= 0 || message.MessageID != entry.MessageID {
		return telegramPollEntry{}, fmt.Errorf("callback message mismatch")
	}
	if entry.AgentID == "" || entry.SessionKey == "" || entry.SenderID == "" {
		return telegramPollEntry{}, fmt.Errorf("callback ownership metadata incomplete")
	}
	if strconv.FormatInt(query.From.ID, 10) != entry.SenderID {
		return telegramPollEntry{}, fmt.Errorf("callback sender mismatch")
	}
	entry.RevealPending = true
	c.pollRegistry[handle] = entry
	return entry, nil
}

func (c *TelegramChannel) finishQuizReveal(handle string, success bool) {
	c.pollRegistryMu.Lock()
	defer c.pollRegistryMu.Unlock()
	entry, ok := c.pollRegistry[handle]
	if !ok {
		return
	}
	entry.RevealPending = false
	if success {
		entry.RevealConsumed = true
	}
	c.pollRegistry[handle] = entry
}

func (c *TelegramChannel) handleQuizRevealCallback(ctx context.Context, query *telego.CallbackQuery) error {
	if query == nil {
		return nil
	}
	handle := strings.TrimPrefix(strings.TrimSpace(query.Data), "quiz_reveal:")
	entry, err := c.claimQuizReveal(query, handle)
	if err != nil {
		answerErr := c.bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            "Quiz ini tidak dapat diungkap dari konteks ini atau sudah diproses.",
			ShowAlert:       true,
		})
		if answerErr != nil {
			return fmt.Errorf("answer rejected quiz reveal callback: %w", answerErr)
		}
		return nil
	}

	revealText := formatQuizRevealText(entry.PollPayload)
	message := query.Message.Message()
	if message == nil {
		c.finishQuizReveal(handle, false)
		return nil
	}
	if err := c.bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
	}); err != nil {
		c.finishQuizReveal(handle, false)
		return fmt.Errorf("answer quiz reveal callback: %w", err)
	}

	var sendErr error
	if message.Chat.Type == telego.ChatTypePrivate {
		_, sendErr = c.bot.SendMessage(ctx, &telego.SendMessageParams{
			ChatID:          tu.ID(entry.ChatID),
			MessageThreadID: entry.ThreadID,
			Text:            revealText,
			ReplyParameters: &telego.ReplyParameters{
				MessageID:                entry.MessageID,
				AllowSendingWithoutReply: true,
			},
		})
	} else {
		_, sendErr = c.bot.SendMessage(ctx, &telego.SendMessageParams{
			ChatID:          tu.ID(entry.ChatID),
			MessageThreadID: entry.ThreadID,
			ReceiverUserID:  query.From.ID,
			CallbackQueryID: query.ID,
			Text:            revealText,
		})
	}
	if sendErr != nil {
		c.finishQuizReveal(handle, false)
		return fmt.Errorf("deliver personal quiz reveal: %w", sendErr)
	}
	c.finishQuizReveal(handle, true)
	return nil
}
