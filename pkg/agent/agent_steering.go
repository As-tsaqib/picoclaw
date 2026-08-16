// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"context"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/logger"
)

func (al *AgentLoop) processMessageSync(ctx context.Context, msg bus.InboundMessage) {
	if al.channelManager != nil {
		defer al.channelManager.InvokeTypingStop(msg.Channel, msg.ChatID)
	}

	var structured *bus.StructuredContent
	response, err := al.processMessageWithStructured(ctx, msg, &structured)
	target, targetErr := al.buildContinuationTarget(msg)
	sessionKey := msg.SessionKey
	if targetErr == nil && target != nil {
		sessionKey = target.SessionKey
	}
	al.publishResponseOrErrorForInbound(
		ctx,
		msg.Channel,
		msg.ChatID,
		sessionKey,
		&msg.Context,
		response,
		err,
		structured,
	)
}

func (al *AgentLoop) runTurnWithSteering(ctx context.Context, initialMsg bus.InboundMessage) {
	// Process the initial message
	var structured *bus.StructuredContent
	response, err := al.processMessageWithStructured(ctx, initialMsg, &structured)
	if err != nil {
		if !al.maybePublishErrorForInbound(
			ctx,
			initialMsg.Channel,
			initialMsg.ChatID,
			initialMsg.SessionKey,
			&initialMsg.Context,
			err,
		) {
			return // context canceled
		}
		response = ""
	}
	finalResponse := response
	finalStructured := structured

	// Build continuation target
	target, targetErr := al.buildContinuationTarget(initialMsg)
	if targetErr != nil {
		logger.WarnCF("agent", "Failed to build steering continuation target",
			map[string]any{
				"channel": initialMsg.Channel,
				"error":   targetErr.Error(),
			})
		return
	}
	if target == nil {
		// System message or non-routable, response already published
		return
	}

	continued, continueErr := al.drainQueuedSteeringContinuations(ctx, target)
	if continueErr != nil {
		logFields := map[string]any{
			"channel": target.Channel,
			"error":   continueErr.Error(),
		}
		if target.InboundContext == nil || !target.InboundContext.PrivateResponse {
			logFields["chat_id"] = target.ChatID
		}
		logger.WarnCF("agent", "Failed to continue queued steering",
			logFields)
	} else if continued != "" {
		finalResponse = continued
		finalStructured = nil
	}

	// Publish final response
	if finalResponse != "" || finalStructured != nil {
		al.publishResponseIfNeededForInbound(
			ctx,
			target.Channel,
			target.ChatID,
			target.SessionKey,
			target.InboundContext,
			finalResponse,
			finalStructured,
		)
	}
}

func (al *AgentLoop) drainQueuedSteeringContinuations(
	ctx context.Context,
	target *continuationTarget,
) (string, error) {
	if target == nil {
		return "", nil
	}

	finalResponse := ""
	for al.pendingSteeringCountForScope(target.SessionKey) > 0 {
		if err := ctx.Err(); err != nil {
			return finalResponse, err
		}

		logFields := map[string]any{
			"channel":     target.Channel,
			"session_key": target.SessionKey,
			"queue_depth": al.pendingSteeringCountForScope(target.SessionKey),
		}
		if target.InboundContext == nil || !target.InboundContext.PrivateResponse {
			logFields["chat_id"] = target.ChatID
		}
		logger.InfoCF("agent", "Continuing queued steering after turn end", logFields)

		if target.InboundContext != nil && target.InboundContext.PrivateResponse {
			// Each queued private inbound was already verified and bound to this
			// isolated session. Resolve the final response through the latest
			// session binding instead of reusing an older, possibly expired callback.
			target.InboundContext.PrivateRouteToken = ""
		}
		continued, continueErr := al.Continue(ctx, target.SessionKey, target.Channel, target.ChatID)
		if continueErr != nil {
			return finalResponse, continueErr
		}
		if continued == "" {
			break
		}
		finalResponse = continued
	}

	return finalResponse, nil
}

func (al *AgentLoop) resolveSteeringTarget(msg bus.InboundMessage) (string, string, bool) {
	if msg.Channel == "system" {
		return "", "", false
	}

	route, agent, err := al.resolveMessageRoute(msg)
	if err != nil || agent == nil {
		return "", "", false
	}
	allocation := al.allocateRouteSession(route, msg)

	return resolveAllocatedSession(agent, allocation, msg.SessionKey), agent.ID, true
}
