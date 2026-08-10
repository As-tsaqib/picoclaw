package agent

import (
	"strings"

	"github.com/sipeed/picoclaw/pkg/constants"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/memory"
)

type deferredMemoryDelivery struct {
	agent            *AgentInstance
	caller           memory.CallerScope
	turnID           string
	userContent      string
	assistantContent string
	reviewEligible   bool
}

func (al *AgentLoop) deferTurnMemoryDelivery(ts *turnState, assistantContent string) {
	if al == nil || ts == nil || ts.agent == nil {
		return
	}
	if ts.opts.NoHistory || ts.depth > 0 || (ts.agent.RecallMemory == nil && ts.agent.Checkpoints == nil) {
		al.discardTurnMemory(ts)
		return
	}
	caller := callerScopeForTurn(ts.agent.ID, al.cfg, ts.opts)
	if strings.TrimSpace(caller.SessionKey) == "" {
		al.discardTurnMemory(ts)
		return
	}
	delivery := deferredMemoryDelivery{
		agent:            ts.agent,
		caller:           caller,
		turnID:           ts.turnID,
		userContent:      ts.userMessage,
		assistantContent: assistantContent,
		reviewEligible:   memoryReviewEligible(ts, caller),
	}
	if previous, loaded := al.pendingMemoryDeliveries.LoadOrStore(caller.SessionKey, delivery); loaded {
		if prior, ok := previous.(deferredMemoryDelivery); ok {
			if prior.agent != nil && prior.agent.Checkpoints != nil {
				prior.agent.Checkpoints.DiscardTurn(prior.turnID)
			}
		}
		al.pendingMemoryDeliveries.Store(caller.SessionKey, delivery)
	}
}

func (al *AgentLoop) acknowledgeDeferredMemoryDelivery(sessionKey, assistantContent string, delivered bool) {
	if al == nil {
		return
	}
	value, ok := al.pendingMemoryDeliveries.LoadAndDelete(strings.TrimSpace(sessionKey))
	if !ok {
		return
	}
	delivery, ok := value.(deferredMemoryDelivery)
	if !ok {
		return
	}
	delivery.assistantContent = assistantContent
	al.commitMemoryDelivery(delivery, delivered)
}

func (al *AgentLoop) finishTurnMemoryDelivery(ts *turnState, assistantContent string, delivered bool) {
	if ts == nil || ts.agent == nil {
		return
	}
	if ts.opts.NoHistory || ts.depth > 0 || (ts.agent.RecallMemory == nil && ts.agent.Checkpoints == nil) {
		al.discardTurnMemory(ts)
		return
	}
	caller := callerScopeForTurn(ts.agent.ID, al.cfg, ts.opts)
	if strings.TrimSpace(caller.SessionKey) == "" {
		al.discardTurnMemory(ts)
		return
	}
	delivery := deferredMemoryDelivery{
		agent:            ts.agent,
		caller:           caller,
		turnID:           ts.turnID,
		userContent:      ts.userMessage,
		assistantContent: assistantContent,
		reviewEligible:   memoryReviewEligible(ts, caller),
	}
	al.commitMemoryDelivery(delivery, delivered)
}

func (al *AgentLoop) commitMemoryDelivery(delivery deferredMemoryDelivery, delivered bool) {
	agent := delivery.agent
	if agent == nil {
		return
	}
	if !delivered {
		if agent.Checkpoints != nil {
			agent.Checkpoints.DiscardTurn(delivery.turnID)
		}
		return
	}
	if agent.Checkpoints != nil {
		if err := agent.Checkpoints.CommitDelivered(
			delivery.turnID,
			delivery.caller.SessionKey,
			delivery.assistantContent,
			"",
		); err != nil {
			logger.WarnCF("memory", "Failed to commit delivered task checkpoint", safeMemoryLogFields(err))
		}
	}
	if agent.RecallMemory == nil {
		return
	}
	sequence, err := agent.RecallMemory.AppendDeliveredTurn(
		delivery.caller,
		delivery.turnID,
		delivery.userContent,
		delivery.assistantContent,
		"",
	)
	if err != nil {
		logger.WarnCF("memory", "Failed to append delivered turn to recall index", safeMemoryLogFields(err))
		return
	}
	if delivery.reviewEligible && sequence > 0 {
		al.recordAndMaybeReviewMemory(agent, delivery.caller, sequence)
	}
}

func (al *AgentLoop) discardTurnMemory(ts *turnState) {
	if ts != nil && ts.agent != nil && ts.agent.Checkpoints != nil {
		ts.agent.Checkpoints.DiscardTurn(ts.turnID)
	}
}

func memoryReviewEligible(ts *turnState, caller memory.CallerScope) bool {
	if ts == nil || ts.opts.NoHistory || ts.opts.SuppressMemoryReview || ts.depth > 0 ||
		caller.UserKey == "" || constants.IsInternalChannel(caller.Channel) {
		return false
	}
	sender := strings.ToLower(strings.TrimSpace(ts.opts.Dispatch.SenderID()))
	return sender != "cron" && sender != "heartbeat" && sender != "system"
}

// clearSessionMemoryState mirrors /clear and /reset semantics for structured
// state: discard undelivered checkpoint mutations and the current session's
// transcript/review cursor, while preserving durable curated memory and
// committed task checkpoints.
func clearSessionMemoryState(agent *AgentInstance, caller memory.CallerScope) error {
	if agent == nil {
		return nil
	}
	if agent.RecallMemory != nil {
		if err := agent.RecallMemory.ForgetSession(caller.SessionRef); err != nil {
			return err
		}
	}
	if agent.MemoryReviewState != nil {
		if err := agent.MemoryReviewState.ForgetSession(caller.SessionRef); err != nil {
			return err
		}
	}
	if agent.Checkpoints != nil {
		agent.Checkpoints.DiscardSession(caller.SessionKey)
	}
	return nil
}
