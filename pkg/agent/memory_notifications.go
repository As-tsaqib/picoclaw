package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
	"github.com/As-tsaqib/picoclaw/pkg/tools"
)

const maxMemoryNotificationClaims = 1024

var memoryNotificationClaims = struct {
	sync.Mutex
	items map[string]time.Time
}{items: make(map[string]time.Time)}

func claimMemoryNotification(event tools.MemoryChangeEvent) bool {
	if event.Result.Pending == nil && len(event.Result.Applied) == 0 {
		return false
	}
	identity := strings.TrimSpace(event.Caller.MessageRef)
	if identity == "" {
		identity = strings.TrimSpace(event.TurnID)
	}
	if identity == "" {
		return true
	}
	key := strings.Join([]string{event.Caller.AgentID, event.Caller.UserKey, event.Caller.SessionRef, identity}, "|")
	now := time.Now().UTC()
	memoryNotificationClaims.Lock()
	defer memoryNotificationClaims.Unlock()
	if _, exists := memoryNotificationClaims.items[key]; exists {
		return false
	}
	if len(memoryNotificationClaims.items) >= maxMemoryNotificationClaims {
		cutoff := now.Add(-time.Hour)
		for candidate, seen := range memoryNotificationClaims.items {
			if seen.Before(cutoff) {
				delete(memoryNotificationClaims.items, candidate)
			}
		}
		if len(memoryNotificationClaims.items) >= maxMemoryNotificationClaims {
			var oldestKey string
			var oldest time.Time
			for candidate, seen := range memoryNotificationClaims.items {
				if oldestKey == "" || seen.Before(oldest) {
					oldestKey, oldest = candidate, seen
				}
			}
			delete(memoryNotificationClaims.items, oldestKey)
		}
	}
	memoryNotificationClaims.items[key] = now
	return true
}

func (al *AgentLoop) memoryChangeNotification(ctx context.Context, event tools.MemoryChangeEvent) {
	if al == nil || al.bus == nil || al.cfg == nil || !claimMemoryNotification(event) {
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
		return fmt.Sprintf(" (%d personal operation(s))", len(entries))
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
