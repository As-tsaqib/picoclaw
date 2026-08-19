package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/As-tsaqib/picoclaw/pkg/capability"
	"github.com/As-tsaqib/picoclaw/pkg/channels"
	"github.com/As-tsaqib/picoclaw/pkg/logger"
)

// BeginPreferredStreamForSession opts Telegram into the rich-aware runtime
// streaming path while leaving BeginStream/BeginStreamForSession intact for
// compatibility. Ephemeral sessions still fail closed because Telegram draft
// methods do not accept receiver_user_id/callback_query_id.
func (c *TelegramChannel) BeginPreferredStreamForSession(
	ctx context.Context,
	chatID, sessionKey string,
) (channels.Streamer, error) {
	if c == nil || c.tgCfg == nil || !c.tgCfg.Streaming.Enabled {
		return nil, fmt.Errorf("streaming disabled in config")
	}
	if c.sessionHasEphemeralRoute(sessionKey) {
		return nil, fmt.Errorf("telegram streaming is disabled for ephemeral sessions")
	}
	cid, threadID, err := parseTelegramChatID(chatID)
	if err != nil {
		return nil, err
	}
	streamCfg := c.tgCfg.Streaming.WithDefaults(3, 200)
	return &telegramRichStreamer{
		bot:              c.bot,
		account:          c.Name(),
		serverID:         c.tgCfg.BaseURL,
		chatID:           cid,
		threadID:         threadID,
		draftID:          cryptoRandInt(),
		throttleInterval: time.Duration(streamCfg.ThrottleSeconds) * time.Second,
		minGrowth:        streamCfg.MinGrowthChars,
	}, nil
}

type telegramRichStreamer struct {
	bot              *telego.Bot
	account          string
	serverID         string
	chatID           int64
	threadID         int
	draftID          int
	throttleInterval time.Duration
	minGrowth        int
	lastLen          int
	lastAt           time.Time
	draftTouched     bool
	draftDisabled    bool
	richTouched      bool
	mu               sync.Mutex
}

func (s *telegramRichStreamer) Update(ctx context.Context, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s == nil || s.bot == nil || s.draftDisabled || strings.TrimSpace(content) == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	now := time.Now()
	growth := len(content) - s.lastLen
	if s.lastLen > 0 && now.Sub(s.lastAt) < s.throttleInterval && growth < s.minGrowth {
		return nil
	}

	useRich := s.richTouched || telegramRichStreamCandidate(content)
	if useRich && !capability.GlobalNegativeCache.IsDowngraded(
		"telegram",
		s.account,
		s.serverID,
		capability.FeatureMessageStreamRich,
	) {
		err := s.bot.SendRichMessageDraft(ctx, &telego.SendRichMessageDraftParams{
			ChatID:          s.chatID,
			MessageThreadID: s.threadID,
			DraftID:         s.draftID,
			RichMessage: telego.InputRichMessage{
				Markdown: content,
			},
		})
		if err == nil {
			s.richTouched = true
			s.draftTouched = true
			s.lastLen = len(content)
			s.lastAt = now
			return nil
		}
		capability.GlobalNegativeCache.RecordFailure(
			"telegram",
			s.account,
			s.serverID,
			capability.FeatureMessageStreamRich,
			err,
		)
		logger.WarnCF("telegram", "sendRichMessageDraft failed; using text draft fallback", map[string]any{
			"error": err.Error(),
		})
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}

	if err := s.sendTextDraft(ctx, content); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		// A draft is only a temporary preview. Disable further previews but keep
		// the streamer alive so Finalize still performs persistent delivery.
		logger.WarnCF("telegram", "sendMessageDraft failed; continuing to final delivery", map[string]any{
			"error": err.Error(),
		})
		s.draftDisabled = true
		return nil
	}

	s.draftTouched = true
	s.lastLen = len(content)
	s.lastAt = now
	return nil
}

func (s *telegramRichStreamer) sendTextDraft(ctx context.Context, content string) error {
	return s.bot.SendMessageDraft(ctx, &telego.SendMessageDraftParams{
		ChatID:          s.chatID,
		MessageThreadID: s.threadID,
		DraftID:         s.draftID,
		Text:            markdownToTelegramHTML(content),
		ParseMode:       telego.ModeHTML,
	})
}

func (s *telegramRichStreamer) Finalize(ctx context.Context, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s == nil || s.bot == nil {
		return fmt.Errorf("telegram rich streamer is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	plainFallback := content
	preferRich := s.richTouched || telegramRichStreamCandidate(content)
	if preferRich && !capability.GlobalNegativeCache.IsDowngraded(
		"telegram",
		s.account,
		s.serverID,
		capability.FeatureMessageStructuredRich,
	) {
		_, richErr := s.bot.SendRichMessage(ctx, &telego.SendRichMessageParams{
			ChatID:          tu.ID(s.chatID),
			MessageThreadID: s.threadID,
			RichMessage: telego.InputRichMessage{
				Markdown: content,
			},
		})
		if richErr == nil {
			s.clearDraftLocked(ctx)
			return nil
		}
		unsupported := capability.GlobalNegativeCache.RecordFailure(
			"telegram",
			s.account,
			s.serverID,
			capability.FeatureMessageStructuredRich,
			richErr,
		)
		if unsupported {
			capability.GlobalNegativeCache.RecordFailure(
				"telegram",
				s.account,
				s.serverID,
				capability.FeatureMessageStreamRich,
				richErr,
			)
		}
		logger.WarnCF("telegram", "final SendRichMessage failed; using persistent text fallback", map[string]any{
			"error": richErr.Error(),
		})
	}

	textContent := content
	if hasTelegramRichTable(content) {
		textContent = telegramTableFallbackMarkdown(content)
		plainFallback = telegramTableFallbackPlainText(plainFallback)
	}
	tgMsg := tu.Message(tu.ID(s.chatID), markdownToTelegramHTML(textContent))
	tgMsg.MessageThreadID = s.threadID
	tgMsg.ParseMode = telego.ModeHTML
	if _, err := s.bot.SendMessage(ctx, tgMsg); err != nil {
		tgMsg.Text = plainFallback
		tgMsg.ParseMode = ""
		if _, plainErr := s.bot.SendMessage(ctx, tgMsg); plainErr != nil {
			return fmt.Errorf("telegram rich-stream finalize: %w", plainErr)
		}
	}
	s.clearDraftLocked(ctx)
	return nil
}

func (s *telegramRichStreamer) Cancel(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearDraftLocked(ctx)
}

func (s *telegramRichStreamer) clearDraftLocked(ctx context.Context) {
	if !s.draftTouched || s.bot == nil {
		return
	}
	if err := s.bot.SendMessageDraft(ctx, &telego.SendMessageDraftParams{
		ChatID:          s.chatID,
		MessageThreadID: s.threadID,
		DraftID:         s.draftID,
		Text:            " ",
	}); err != nil {
		logger.DebugCF("telegram", "failed to clear rich streaming draft", map[string]any{
			"chat_id": s.chatID,
			"error":   err.Error(),
		})
	}
	s.lastLen = 0
	s.draftTouched = false
}

func telegramRichStreamCandidate(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	if hasTelegramRichTable(content) || reHeading.MatchString(content) || reCodeBlock.MatchString(content) ||
		reLink.MatchString(content) || strings.Contains(content, "**") || strings.Contains(content, "__") {
		return true
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") || reListItem.MatchString(trimmed) {
			return true
		}
	}
	return false
}
