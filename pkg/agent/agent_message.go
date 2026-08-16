// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
	"github.com/As-tsaqib/picoclaw/pkg/constants"
	"github.com/As-tsaqib/picoclaw/pkg/logger"
	"github.com/As-tsaqib/picoclaw/pkg/routing"
	"github.com/As-tsaqib/picoclaw/pkg/session"
	"github.com/As-tsaqib/picoclaw/pkg/utils"
)

func (al *AgentLoop) buildContinuationTarget(msg bus.InboundMessage) (*continuationTarget, error) {
	if msg.Channel == "system" {
		return nil, nil
	}

	route, agent, err := al.resolveMessageRoute(msg)
	if err != nil {
		return nil, err
	}
	allocation := al.allocateRouteSession(route, msg)
	sessionKey, dashboardAttached := al.resolveTurnSession(agent, allocation, &msg.Context, msg.SessionKey)
	inbound := cloneInboundContext(&msg.Context)
	if inbound != nil {
		inbound.SessionDashboard = dashboardAttached
	}

	return &continuationTarget{
		SessionKey:     sessionKey,
		Channel:        msg.Channel,
		ChatID:         msg.ChatID,
		InboundContext: inbound,
	}, nil
}

func (al *AgentLoop) ProcessDirect(
	ctx context.Context,
	content, sessionKey string,
) (string, error) {
	return al.ProcessDirectWithChannel(ctx, content, sessionKey, "cli", "direct")
}

func (al *AgentLoop) ProcessDirectWithChannel(
	ctx context.Context,
	content, sessionKey, channel, chatID string,
) (string, error) {
	if err := al.ensureHooksInitialized(ctx); err != nil {
		return "", err
	}
	if err := al.ensureMCPInitialized(ctx); err != nil {
		return "", err
	}

	msg := bus.InboundMessage{
		Context: bus.InboundContext{
			Channel:  channel,
			ChatID:   chatID,
			ChatType: "direct",
			SenderID: "cron",
		},
		Content:    content,
		SessionKey: sessionKey,
	}

	response, err := al.processMessage(ctx, msg)
	if err == nil && response != "" {
		if target, targetErr := al.buildContinuationTarget(msg); targetErr == nil && target != nil {
			al.acknowledgeDeferredMemoryDelivery(target.SessionKey, response, true)
		}
	}
	return response, err
}

func (al *AgentLoop) ProcessHeartbeat(
	ctx context.Context,
	content, channel, chatID string,
) (string, error) {
	if err := al.ensureHooksInitialized(ctx); err != nil {
		return "", err
	}
	if err := al.ensureMCPInitialized(ctx); err != nil {
		return "", err
	}

	agent := al.GetRegistry().GetDefaultAgent()
	if agent == nil {
		return "", fmt.Errorf("no default agent for heartbeat")
	}
	dispatch := DispatchRequest{
		SessionKey:  "heartbeat",
		UserMessage: content,
	}
	if channel != "" || chatID != "" {
		dispatch.InboundContext = &bus.InboundContext{
			Channel:  channel,
			ChatID:   chatID,
			ChatType: "direct",
			SenderID: "heartbeat",
		}
	}
	return al.runAgentLoop(ctx, agent, processOptions{
		Dispatch:             dispatch,
		DefaultResponse:      defaultResponse,
		EnableSummary:        false,
		SendResponse:         false,
		SuppressToolFeedback: true,
		NoHistory:            true, // Don't load session history for heartbeat
	})
}

func (al *AgentLoop) prepareInboundMessageForAgent(
	ctx context.Context,
	msg bus.InboundMessage,
) bus.InboundMessage {
	msg = bus.NormalizeInboundMessage(msg)

	var hadAudio bool
	msg, hadAudio = al.transcribeAudioInMessage(ctx, msg)

	// For audio messages the placeholder was deferred by the channel.
	// Now that transcription (and optional feedback) is done, send it.
	if hadAudio && !msg.Context.PrivateResponse && al.channelManager != nil {
		al.channelManager.SendPlaceholder(ctx, msg.Channel, msg.ChatID)
	}

	return msg
}

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	return al.processMessageWithStructured(ctx, msg, nil)
}

func (al *AgentLoop) processMessageWithStructured(
	ctx context.Context,
	msg bus.InboundMessage,
	structuredOut **bus.StructuredContent,
) (string, error) {
	msg = al.prepareInboundMessageForAgent(ctx, msg)

	// Add message preview to log (show full content for error messages)
	var logContent string
	if msg.Context.PrivateResponse {
		logContent = "[private Telegram interaction]"
	} else if strings.Contains(msg.Content, "Error:") || strings.Contains(msg.Content, "error") {
		logContent = msg.Content // Full content for errors
	} else {
		logContent = utils.Truncate(msg.Content, 80)
	}
	logMessage := fmt.Sprintf("Processing message from %s:%s: %s", msg.Channel, msg.SenderID, logContent)
	logFields := map[string]any{
		"channel":     msg.Channel,
		"chat_id":     msg.ChatID,
		"sender_id":   msg.SenderID,
		"session_key": msg.SessionKey,
	}
	if msg.Context.PrivateResponse {
		logMessage = "Processing private channel interaction"
		logFields = map[string]any{
			"channel":     msg.Channel,
			"content_len": len([]rune(msg.Content)),
			"session_key": msg.SessionKey,
		}
	}
	logger.InfoCF("agent", logMessage, logFields)

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		return al.processSystemMessage(ctx, msg)
	}

	route, agent, routeErr := al.resolveMessageRoute(msg)
	if routeErr != nil {
		return "", routeErr
	}

	allocation := al.allocateRouteSession(route, msg)

	// Resolve the normal route session first, then apply a durable private
	// Telegram dashboard attachment without mutating the origin route mapping.
	scopeKey := resolveAllocatedSession(agent, allocation, msg.SessionKey)
	sessionKey, dashboardAttached := al.resolveTurnSession(agent, allocation, &msg.Context, msg.SessionKey)
	msg.Context.SessionDashboard = dashboardAttached
	if err := al.bindPrivateInboundRoute(msg, sessionKey); err != nil {
		// Bind only the route capability issued by the Telegram update. If the
		// channel rejects it, propagate the error through the verified private
		// inbound context. Private delivery failures are permanent and never fall
		// back to a public send.
		logger.WarnCF("agent", "Private route binding rejected; dropping turn", map[string]any{
			"channel": msg.Channel,
			"session": sessionKey,
			"error":   err.Error(),
		})
		return "", err
	}

	// Reset message-tool state for this round so we don't skip publishing due to a previous round.
	if tool, ok := agent.Tools.Get("message"); ok {
		if resetter, ok := tool.(interface{ ResetSentInRound(sessionKey string) }); ok {
			resetter.ResetSentInRound(sessionKey)
		}
	}

	logger.InfoCF("agent", "Routed message",
		map[string]any{
			"agent_id":           agent.ID,
			"scope_key":          scopeKey,
			"session_key":        sessionKey,
			"matched_by":         route.MatchedBy,
			"route_agent":        route.AgentID,
			"route_channel":      route.Channel,
			"route_main_session": allocation.MainSessionKey,
		})

	sessionAliases := []string(nil)
	if sessionKey == allocation.SessionKey {
		sessionAliases = buildSessionAliases(sessionKey, append(allocation.SessionAliases, msg.SessionKey)...)
	}
	opts := processOptions{
		Dispatch: DispatchRequest{
			SessionKey:       sessionKey,
			RouteSessionKey:  scopeKey,
			SessionDashboard: dashboardAttached,
			SessionAliases:   sessionAliases,
			InboundContext:   cloneInboundContext(&msg.Context),
			RouteResult:      cloneResolvedRoute(&route),
			SessionScope:     session.CloneScope(&allocation.Scope),
			UserMessage:      msg.Content,
			Media:            append([]string(nil), msg.Media...),
		},
		SenderID:                msg.SenderID,
		SenderDisplayName:       msg.Sender.DisplayName,
		DefaultResponse:         defaultResponse,
		EnableSummary:           true,
		SendResponse:            false,
		AllowInterimPicoPublish: true,
	}
	var err error
	opts, err = resolveTurnProfileOptions(al.GetConfig(), opts)
	if err != nil {
		return "", err
	}

	if !commands.HasCommandPrefix(msg.Content) {
		if !opts.Dispatch.SessionDashboard {
			ensureSessionMetadata(agent.Sessions, sessionKey, opts.Dispatch.SessionScope, opts.Dispatch.SessionAliases)
		}
		if catalog, ok := agent.Sessions.(session.ScopedSessionStore); ok {
			if nameErr := catalog.SetAutomaticSessionName(sessionKey, msg.Content); nameErr != nil {
				logger.WarnCF("session", "Failed to assign automatic session name", map[string]any{"error": nameErr.Error()})
			}
		}
	}

	// context-dependent commands check their own Runtime fields and report
	// "unavailable" when the required capability is nil.
	if response, structured, handled := al.handleCommandWithStructured(ctx, msg, agent, &opts); handled {
		if structuredOut != nil {
			*structuredOut = structured
		}
		return response, nil
	}

	if pending := al.takePendingSkills(opts.Dispatch.SessionKey); len(pending) > 0 {
		opts.ForcedSkills = append(opts.ForcedSkills, pending...)
		logger.InfoCF("agent", "Applying pending skill override",
			map[string]any{
				"session_key": opts.Dispatch.SessionKey,
				"skills":      strings.Join(pending, ","),
			})
	}

	return al.runAgentLoop(ctx, agent, opts)
}

func (al *AgentLoop) bindPrivateInboundRoute(msg bus.InboundMessage, sessionKey string) error {
	if !msg.Context.PrivateResponse || al.channelManager == nil {
		return nil
	}
	return al.channelManager.BindPrivateRoute(msg.Channel, sessionKey, msg.Context)
}

func (al *AgentLoop) resolveMessageRoute(msg bus.InboundMessage) (routing.ResolvedRoute, *AgentInstance, error) {
	registry := al.GetRegistry()
	inboundCtx := normalizedInboundContext(msg)
	route := registry.ResolveRoute(inboundCtx)

	agent, ok := registry.GetAgent(route.AgentID)
	if !ok {
		agent = registry.GetDefaultAgent()
	}
	if agent == nil {
		return routing.ResolvedRoute{}, nil, fmt.Errorf("no agent available for route (agent_id=%s)", route.AgentID)
	}

	return route, agent, nil
}

func (al *AgentLoop) allocateRouteSession(route routing.ResolvedRoute, msg bus.InboundMessage) session.Allocation {
	return session.AllocateRouteSession(session.AllocationInput{
		AgentID:       route.AgentID,
		Context:       normalizedInboundContext(msg),
		SessionPolicy: route.SessionPolicy,
	})
}

func resolveAllocatedSession(agent *AgentInstance, allocation session.Allocation, explicit string) string {
	if session.IsSessionInstanceKey(explicit) {
		if agent != nil {
			if catalog, ok := agent.Sessions.(session.ScopedSessionStore); ok &&
				catalogSessionInScope(catalog, &allocation.Scope, allocation.SessionAliases, strings.TrimSpace(explicit)) {
				return strings.TrimSpace(explicit)
			}
		}
		// A session-instance key is meaningful only together with matching
		// durable scope metadata. Ignore an injected or stale key and resolve the
		// current route normally.
		explicit = ""
	}
	if resolved := resolveScopeKey(allocation.SessionKey, explicit); resolved != allocation.SessionKey ||
		isExplicitSessionKey(explicit) {
		return resolved
	}
	if agent != nil {
		if catalog, ok := agent.Sessions.(session.ScopedSessionStore); ok {
			if active := strings.TrimSpace(catalog.ActiveScopedSession(&allocation.Scope, allocation.SessionAliases)); active != "" {
				return active
			}
		}
	}
	return allocation.SessionKey
}

func (al *AgentLoop) resolveTurnSession(
	agent *AgentInstance,
	allocation session.Allocation,
	inbound *bus.InboundContext,
	explicit string,
) (string, bool) {
	// SessionDashboard is process-local and can only be set after PicoClaw has
	// already authorized and frozen a private-dashboard selection. Never
	// re-read the durable mapping for that turn: a concurrent callback may
	// change the next-turn selection, but it must not retarget this one.
	if inbound != nil && inbound.SessionDashboard && strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit), true
	}

	routeSessionKey := resolveAllocatedSession(agent, allocation, explicit)
	if agent == nil {
		return routeSessionKey, false
	}
	dashboardCatalog, ok := agent.Sessions.(session.DashboardSessionStore)
	if !ok {
		return routeSessionKey, false
	}
	_, query, dashboard := al.telegramSessionDashboard(
		inbound, agent.ID, routeSessionKey, &allocation.Scope, allocation.SessionAliases,
	)
	if !dashboard {
		return routeSessionKey, false
	}
	active := strings.TrimSpace(dashboardCatalog.ActiveDashboardSession(query))
	if active == "" || active == routeSessionKey {
		return routeSessionKey, false
	}
	return active, true
}

func (al *AgentLoop) processSystemMessage(
	ctx context.Context,
	msg bus.InboundMessage,
) (string, error) {
	if msg.Channel != "system" {
		return "", fmt.Errorf(
			"processSystemMessage called with non-system message channel: %s",
			msg.Channel,
		)
	}

	logger.InfoCF("agent", "Processing system message",
		map[string]any{
			"sender_id": msg.SenderID,
			"chat_id":   msg.ChatID,
		})

	// Parse origin channel from chat_id (format: "channel:chat_id")
	var originChannel, originChatID string
	if idx := strings.Index(msg.ChatID, ":"); idx > 0 {
		originChannel = msg.ChatID[:idx]
		originChatID = msg.ChatID[idx+1:]
	} else {
		originChannel = "cli"
		originChatID = msg.ChatID
	}

	// Extract subagent result from message content
	// Format: "Task 'label' completed.\n\nResult:\n<actual content>"
	content := msg.Content
	if idx := strings.Index(content, "Result:\n"); idx >= 0 {
		content = content[idx+8:] // Extract just the result part
	}

	// Skip internal channels - only log, don't send to user
	if constants.IsInternalChannel(originChannel) {
		logger.InfoCF("agent", "Subagent completed (internal channel)",
			map[string]any{
				"sender_id":   msg.SenderID,
				"content_len": len(content),
				"channel":     originChannel,
			})
		return "", nil
	}

	// Use default agent for system messages
	agent := al.GetRegistry().GetDefaultAgent()
	if agent == nil {
		return "", fmt.Errorf("no default agent for system message")
	}

	// Use the origin session for context
	sessionKey := session.BuildMainSessionKey(agent.ID)
	dispatch := DispatchRequest{
		SessionKey:  sessionKey,
		UserMessage: fmt.Sprintf("[System: %s] %s", msg.SenderID, msg.Content),
	}
	if originChannel != "" || originChatID != "" {
		dispatch.InboundContext = &bus.InboundContext{
			Channel:  originChannel,
			ChatID:   originChatID,
			ChatType: "direct",
			SenderID: msg.SenderID,
		}
	}

	return al.runAgentLoop(ctx, agent, processOptions{
		Dispatch:             dispatch,
		DefaultResponse:      "Background task completed.",
		EnableSummary:        false,
		SendResponse:         true,
		SuppressMemoryReview: true,
	})
}
