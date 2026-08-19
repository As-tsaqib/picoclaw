package telegram

import (
	"context"
	"fmt"
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

	// 1. Delete items older than TTL or already closed over 24h
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

	// 2. If still over max capacity, evict oldest created entries
	if len(c.pollRegistry) >= maxPollRegistryEntries {
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
}

func (c *TelegramChannel) registerPollEntry(entry telegramPollEntry) {
	c.pollRegistryMu.Lock()
	defer c.pollRegistryMu.Unlock()

	now := time.Now().UTC()
	c.prunePollsLocked(now)

	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
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

func (c *TelegramChannel) StopPoll(
	ctx context.Context,
	localHandle string,
	callerAgentID string,
	callerSessionKey string,
	callerSenderID string,
) error {
	entry, ok := c.resolvePollByLocalHandle(localHandle)
	if !ok {
		return fmt.Errorf("poll %q not found or expired", localHandle)
	}

	// Verify caller / session ownership to prevent cross-session / cross-agent / cross-user stop
	if callerAgentID != "" && entry.AgentID != "" && callerAgentID != entry.AgentID {
		return fmt.Errorf(
			"caller agent %q not authorized to stop poll created by agent %q",
			callerAgentID,
			entry.AgentID,
		)
	}
	if callerSessionKey != "" && entry.SessionKey != "" && callerSessionKey != entry.SessionKey {
		return fmt.Errorf(
			"caller session %q not authorized to stop poll in session %q",
			callerSessionKey,
			entry.SessionKey,
		)
	}
	if callerSenderID != "" && entry.SenderID != "" && callerSenderID != entry.SenderID {
		return fmt.Errorf(
			"caller sender %q not authorized to stop poll created by %q",
			callerSenderID,
			entry.SenderID,
		)
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
	logger.DebugCF("telegram", "Received poll update", map[string]any{
		"poll_id":   poll.ID,
		"is_closed": poll.IsClosed,
	})
	return nil
}

func (c *TelegramChannel) handlePollAnswerUpdate(_ *th.Context, answer *telego.PollAnswer) error {
	if answer == nil {
		return nil
	}
	entry, found := c.resolvePollByTgPollID(answer.PollID)
	if !found {
		logger.DebugCF("telegram", "Received answer for unregistered poll", map[string]any{
			"poll_id": answer.PollID,
		})
		return nil
	}
	logger.DebugCF("telegram", "Received poll answer", map[string]any{
		"poll_id":      answer.PollID,
		"local_handle": entry.LocalHandle,
		"user_id":      answer.User.ID,
		"option_ids":   answer.OptionIDs,
	})
	return nil
}

func (c *TelegramChannel) handleQuizRevealCallback(ctx context.Context, query *telego.CallbackQuery) error {
	if query == nil || query.Message == nil {
		return nil
	}
	handle := strings.TrimPrefix(query.Data, "quiz_reveal:")
	entry, ok := c.resolvePollByLocalHandle(handle)
	if !ok || entry.PollPayload == nil {
		_ = c.bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            "Detail quiz tidak ditemukan atau sudah kedaluwarsa.",
			ShowAlert:       true,
		})
		return nil
	}

	_ = c.bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
		Text:            "Jawaban quiz terungkap",
	})

	revealText := formatQuizRevealText(entry.PollPayload)
	msg := query.Message.Message()
	if msg != nil {
		_, _ = c.bot.EditMessageText(ctx, &telego.EditMessageTextParams{
			ChatID:    tu.ID(entry.ChatID),
			MessageID: msg.MessageID,
			Text:      revealText,
		})
	}
	return nil
}
