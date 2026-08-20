package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/routing"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

// handleInternalCallback is deliberately interaction-owned: it is the single
// allowlisted router for platform callbacks. Individual command handlers own
// semantics; this function never turns callback state into slash-command text.
func (al *AgentLoop) handleInternalCallback(
	ctx context.Context,
	req bus.InternalCallbackRequest,
) (*bus.InternalCallbackResponse, error) {
	switch strings.ToLower(strings.TrimSpace(req.Kind)) {
	case "session":
		return al.handleInternalSessionCallback(ctx, req)
	case "model":
		return al.handleInternalModelCallback(ctx, req)
	case "memory":
		return al.handleInternalMemoryCallback(ctx, req)
	case "skill":
		return al.handleInternalSkillCallback(ctx, req)
	case "checkpoint":
		return al.handleInternalCheckpointCallback(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported internal callback")
	}
}

type boundInteractionContext struct {
	inbound    bus.InboundContext
	agent      *AgentInstance
	allocation session.Allocation
	catalog    session.ScopedSessionStore
}

func (al *AgentLoop) resolveBoundInteraction(req bus.InternalCallbackRequest) (boundInteractionContext, error) {
	inbound := bus.NormalizeInboundMessage(bus.InboundMessage{Context: req.Inbound}).Context
	if strings.TrimSpace(req.OwnerID) == "" || inbound.SenderID != strings.TrimSpace(req.OwnerID) ||
		inbound.Channel != strings.TrimSpace(req.Channel) ||
		routing.NormalizeAccountID(inbound.Account) != routing.NormalizeAccountID(req.Account) ||
		inbound.ChatID != strings.TrimSpace(req.ChatID) || inbound.TopicID != strings.TrimSpace(req.TopicID) {
		return boundInteractionContext{}, fmt.Errorf("callback scope validation failed")
	}
	route, agent, routeErr := al.resolveMessageRoute(bus.InboundMessage{Context: inbound})
	if routeErr != nil || agent == nil || !strings.EqualFold(agent.ID, strings.TrimSpace(req.AgentID)) {
		return boundInteractionContext{}, fmt.Errorf("callback agent validation failed")
	}
	allocation := session.AllocateRouteSession(session.AllocationInput{
		AgentID: route.AgentID, Context: inbound, SessionPolicy: route.SessionPolicy,
	})
	if strings.TrimSpace(req.Scope) == "" || session.CanonicalScopeSignature(allocation.Scope) != req.Scope {
		return boundInteractionContext{}, fmt.Errorf("callback session scope validation failed")
	}
	catalog, ok := agent.Sessions.(session.ScopedSessionStore)
	if !ok {
		return boundInteractionContext{}, session.ErrSessionCatalogUnavailable
	}
	target := strings.TrimSpace(req.SessionKey)
	if target == "" || !catalogSessionInScope(catalog, &allocation.Scope, allocation.SessionAliases, target) {
		return boundInteractionContext{}, fmt.Errorf("callback session binding validation failed")
	}
	return boundInteractionContext{inbound: inbound, agent: agent, allocation: allocation, catalog: catalog}, nil
}
