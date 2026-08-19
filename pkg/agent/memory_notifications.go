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
	"github.com/As-tsaqib/picoclaw/pkg/logger"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
	"github.com/As-tsaqib/picoclaw/pkg/tools"
)

const (
	memoryNotificationStoragePrefix = "\x00memory-notification:"
	memoryNotificationAdhocFallback = 50 * time.Millisecond
	memoryNotificationSafetyMargin  = 5 * time.Second
)

type turnNotificationEntry struct {
	mu      sync.Mutex
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
	closed  bool
}

// resetMemoryTurnAggregatorForTest is retained for source compatibility with
// older tests. Notification state is now owned by each AgentLoop, so there is
// no package-global accumulator to reset.
func resetMemoryTurnAggregatorForTest() {}

func (al *AgentLoop) memoryChangeNotification(_ context.Context, event tools.MemoryChangeEvent) {
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
	stableLogicalTurn := turnID != "" && strings.TrimSpace(event.Caller.SessionKey) != ""
	if turnID == "" {
		turnID = fmt.Sprintf("adhoc-%d", time.Now().UnixNano())
	}

	key := strings.Join([]string{event.Caller.AgentID, event.Caller.UserKey, event.Caller.SessionRef, turnID}, "|")
	storageKey := memoryNotificationStorageKey(key)

	for {
		candidate := &turnNotificationEntry{
			turnKey: key,
			caller:  event.Caller,
			channel: event.Caller.Channel,
			account: event.Caller.Account,
			chatID:  event.Caller.ChatID,
			topicID: event.Caller.TopicID,
			target:  event.Target,
		}
		actual, loaded := al.pendingMemoryDeliveries.LoadOrStore(storageKey, candidate)
		entry := candidate
		if loaded {
			var ok bool
			entry, ok = actual.(*turnNotificationEntry)
			if !ok {
				// The NUL-prefixed key is reserved for notification state, so a
				// different value indicates an internal collision. Fail closed.
				return
			}
		}

		entry.mu.Lock()
		if entry.closed {
			entry.mu.Unlock()
			al.pendingMemoryDeliveries.CompareAndDelete(storageKey, entry)
			continue
		}
		if event.Result.Pending != nil {
			entry.pending = append(entry.pending, event.Result.Pending.Mutations...)
		}
		entry.applied = append(entry.applied, event.Result.Applied...)
		if entry.timer == nil {
			delay := memoryNotificationSafetyDelay(al.cfg, stableLogicalTurn)
			entry.timer = time.AfterFunc(delay, func() {
				al.flushTurnNotification(key)
			})
		}
		entry.mu.Unlock()
		return
	}
}

func memoryNotificationStorageKey(key string) string {
	return memoryNotificationStoragePrefix + key
}

func memoryNotificationSafetyDelay(cfg *config.Config, stableLogicalTurn bool) time.Duration {
	if !stableLogicalTurn || cfg == nil {
		return memoryNotificationAdhocFallback
	}
	// Correctness is driven by recordAndMaybeReviewMemory/startMemoryReview,
	// which flush when the reviewer is not scheduled or when it completes.
	// This timer exists only to bound orphaned state if that lifecycle is
	// interrupted unexpectedly, and intentionally exceeds the reviewer timeout.
	return time.Duration(cfg.Memory.BackgroundReview.EffectiveTimeoutSeconds())*time.Second +
		memoryNotificationSafetyMargin
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
	if al == nil {
		return
	}
	storageKey := memoryNotificationStorageKey(key)
	value, exists := al.pendingMemoryDeliveries.Load(storageKey)
	if !exists {
		return
	}
	entry, ok := value.(*turnNotificationEntry)
	if !ok {
		return
	}

	entry.mu.Lock()
	if entry.closed {
		entry.mu.Unlock()
		return
	}
	entry.closed = true
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	snapshot := &turnNotificationEntry{
		turnKey: entry.turnKey,
		caller:  entry.caller,
		channel: entry.channel,
		account: entry.account,
		chatID:  entry.chatID,
		topicID: entry.topicID,
		target:  entry.target,
		pending: append([]memory.CuratedMutation(nil), entry.pending...),
		applied: append([]memory.CuratedEntry(nil), entry.applied...),
	}
	entry.mu.Unlock()
	al.pendingMemoryDeliveries.CompareAndDelete(storageKey, entry)

	if len(snapshot.applied) == 0 && len(snapshot.pending) == 0 {
		return
	}

	mode := al.cfg.Memory.EffectiveNotificationMode()
	if mode == config.MemoryNotificationOff {
		return
	}

	message := "💾 Memory updated"
	if len(snapshot.applied) == 0 && len(snapshot.pending) > 0 {
		message = "💾 Memory change pending approval"
	}

	if mode == config.MemoryNotificationVerbose {
		message += formatAggregatedMemoryPreview(snapshot)
	}

	outboundCtx := bus.InboundContext{
		Channel: snapshot.channel,
		Account: snapshot.account,
		ChatID:  snapshot.chatID,
		TopicID: snapshot.topicID,
		Raw:     map[string]string{"memory_notification": "true"},
	}

	pubCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := al.bus.PublishOutbound(pubCtx, bus.OutboundMessage{
		Context:    outboundCtx,
		AgentID:    snapshot.caller.AgentID,
		SessionKey: snapshot.caller.SessionKey,
		Content:    message,
	}); err != nil {
		logger.WarnCF("memory", "Memory notification publish failed", map[string]any{"error": err.Error()})
	}
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
