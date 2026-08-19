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

type turnNotificationEntry struct {
	turnKey string
	caller  memory.CallerScope
	channel string
	account string
	chatID  string
	topicID string
	target  string
	pending []memory.CuratedMutation
	applied []memory.CuratedEntry
	timer   *time.Timer
}

var memoryTurnAggregator = struct {
	sync.Mutex
	items map[string]*turnNotificationEntry
}{items: make(map[string]*turnNotificationEntry)}

func resetMemoryTurnAggregatorForTest() {
	memoryTurnAggregator.Lock()
	defer memoryTurnAggregator.Unlock()
	for _, entry := range memoryTurnAggregator.items {
		if entry.timer != nil {
			entry.timer.Stop()
		}
	}
	memoryTurnAggregator.items = make(map[string]*turnNotificationEntry)
}

func (al *AgentLoop) memoryChangeNotification(ctx context.Context, event tools.MemoryChangeEvent) {
	if al == nil || al.bus == nil || al.cfg == nil {
		return
	}
	if event.Result.Pending == nil && len(event.Result.Applied) == 0 {
		return
	}

	mode := al.cfg.Memory.EffectiveNotificationMode()
	if mode == config.MemoryNotificationOff || strings.TrimSpace(event.Caller.Channel) == "" ||
		strings.TrimSpace(event.Caller.ChatID) == "" {
		return
	}

	turnID := strings.TrimSpace(event.TurnID)
	if turnID == "" {
		turnID = strings.TrimSpace(event.Caller.MessageRef)
	}
	if turnID == "" {
		turnID = fmt.Sprintf("adhoc-%d", time.Now().UnixNano())
	}

	key := strings.Join([]string{event.Caller.AgentID, event.Caller.UserKey, event.Caller.SessionRef, turnID}, "|")

	memoryTurnAggregator.Lock()
	defer memoryTurnAggregator.Unlock()

	entry, exists := memoryTurnAggregator.items[key]
	if !exists {
		entry = &turnNotificationEntry{
			turnKey: key,
			caller:  event.Caller,
			channel: event.Caller.Channel,
			account: event.Caller.Account,
			chatID:  event.Caller.ChatID,
			topicID: event.Caller.TopicID,
			target:  event.Target,
		}
		memoryTurnAggregator.items[key] = entry
	}

	if event.Result.Pending != nil {
		entry.pending = append(entry.pending, event.Result.Pending.Mutations...)
	}
	entry.applied = append(entry.applied, event.Result.Applied...)

	if entry.timer != nil {
		entry.timer.Stop()
	}

	entry.timer = time.AfterFunc(50*time.Millisecond, func() {
		al.flushTurnNotification(key)
	})
}

func (al *AgentLoop) flushTurnNotificationByTurnID(agentID string, caller memory.CallerScope, turnID string) {
	if al == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		turnID = strings.TrimSpace(caller.MessageRef)
	}
	if turnID == "" {
		return
	}
	key := strings.Join([]string{agentID, caller.UserKey, caller.SessionRef, turnID}, "|")
	al.flushTurnNotification(key)
}

func (al *AgentLoop) flushTurnNotification(key string) {
	memoryTurnAggregator.Lock()
	entry, exists := memoryTurnAggregator.items[key]
	if !exists {
		memoryTurnAggregator.Unlock()
		return
	}
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	delete(memoryTurnAggregator.items, key)
	memoryTurnAggregator.Unlock()

	if len(entry.applied) == 0 && len(entry.pending) == 0 {
		return
	}

	mode := al.cfg.Memory.EffectiveNotificationMode()
	if mode == config.MemoryNotificationOff {
		return
	}

	message := "💾 Memory updated"
	if len(entry.applied) == 0 && len(entry.pending) > 0 {
		message = "💾 Memory change pending approval"
	}

	if mode == config.MemoryNotificationVerbose {
		message += formatAggregatedMemoryPreview(entry)
	}

	outboundCtx := bus.InboundContext{
		Channel: entry.channel,
		Account: entry.account,
		ChatID:  entry.chatID,
		TopicID: entry.topicID,
		Raw:     map[string]string{"memory_notification": "true"},
	}

	go func() {
		pubCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = al.bus.PublishOutbound(pubCtx, bus.OutboundMessage{
			Context:    outboundCtx,
			AgentID:    entry.caller.AgentID,
			SessionKey: entry.caller.SessionKey,
			Content:    message,
		})
	}()
}

func formatAggregatedMemoryPreview(entry *turnNotificationEntry) string {
	if len(entry.applied) == 0 && len(entry.pending) > 0 {
		return fmt.Sprintf(" (%d operation(s) staged)", len(entry.pending))
	}
	if len(entry.applied) == 0 {
		return ""
	}

	isGroup := strings.TrimSpace(entry.caller.GroupID) != ""
	if entry.target == memory.CuratedTargetCurrentUser && isGroup {
		return fmt.Sprintf(" (%d personal operation(s))", len(entry.applied))
	}

	var parts []string
	seen := make(map[string]struct{})
	for _, e := range entry.applied {
		var desc string
		if isGroup &&
			(entry.target == memory.CuratedTargetCurrentUser ||
				e.EffectiveVisibility() != memory.CuratedVisibilityShared) {
			desc = "• personal preference updated"
		} else if e.PreferenceKey != "" && e.PreferenceValue != "" {
			desc = fmt.Sprintf("• %s → %s", e.PreferenceKey, e.PreferenceValue)
		} else {
			preview := memory.RedactMemoryText(e.Content)
			preview = truncateNotification(preview, 80)
			if preview != "" {
				desc = fmt.Sprintf("• %s: %s", e.ID, preview)
			} else {
				desc = fmt.Sprintf("• %s", e.ID)
			}
		}
		if _, ok := seen[desc]; !ok {
			seen[desc] = struct{}{}
			parts = append(parts, desc)
		}
		if len(parts) >= 5 {
			break
		}
	}

	if len(entry.applied) > len(parts) {
		parts = append(parts, fmt.Sprintf("• +%d more", len(entry.applied)-len(parts)))
	}

	return "\n" + strings.Join(parts, "\n")
}

func truncateNotification(value string, maxChars int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if utf8.RuneCountInString(value) <= maxChars {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxChars-1]) + "…"
}
