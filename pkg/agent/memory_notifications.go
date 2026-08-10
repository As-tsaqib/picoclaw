package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/memory"
	"github.com/sipeed/picoclaw/pkg/tools"
)

func (al *AgentLoop) memoryChangeNotification(ctx context.Context, event tools.MemoryChangeEvent) {
	if al == nil || al.bus == nil || al.cfg == nil {
		return
	}
	mode := al.cfg.Memory.EffectiveNotificationMode()
	if mode == config.MemoryNotificationOff || strings.TrimSpace(event.Caller.Channel) == "" ||
		strings.TrimSpace(event.Caller.ChatID) == "" {
		return
	}
	message := "💾 Memory updated"
	if event.Result.Pending != nil {
		message = "💾 Memory change pending approval"
	}
	if mode == config.MemoryNotificationVerbose {
		message += formatMemoryChangePreview(event)
	}
	outboundCtx := bus.InboundContext{
		Channel: event.Caller.Channel,
		Account: event.Caller.Account,
		ChatID:  event.Caller.ChatID,
		TopicID: event.Caller.TopicID,
		Raw:     map[string]string{"memory_notification": "true"},
	}
	go func() {
		pubCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = al.bus.PublishOutbound(pubCtx, bus.OutboundMessage{
			Context:    outboundCtx,
			AgentID:    event.Caller.AgentID,
			SessionKey: event.Caller.SessionKey,
			Content:    message,
		})
	}()
}

func formatMemoryChangePreview(event tools.MemoryChangeEvent) string {
	result := event.Result
	if result.Pending != nil {
		return fmt.Sprintf(" (%d operation(s) staged)", len(result.Pending.Mutations))
	}
	entries := result.Applied
	if len(entries) == 0 {
		return ""
	}
	if event.Target == memory.CuratedTargetCurrentUser && strings.TrimSpace(event.Caller.GroupID) != "" {
		return fmt.Sprintf(" (%d private operation(s))", len(entries))
	}
	parts := make([]string, 0, minInt(len(entries), 3))
	for _, entry := range entries {
		preview := memory.RedactMemoryText(entry.Content)
		preview = truncateNotification(preview, 96)
		if preview == "" {
			parts = append(parts, entry.ID)
		} else {
			parts = append(parts, entry.ID+": "+preview)
		}
	}
	if len(entries) > len(parts) {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-len(parts)))
	}
	return " — " + strings.Join(parts, "; ")
}

func truncateNotification(value string, maxChars int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if utf8.RuneCountInString(value) <= maxChars {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxChars-1]) + "…"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
