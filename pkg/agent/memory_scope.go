package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
	"github.com/As-tsaqib/picoclaw/pkg/routing"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func callerScopeForTurn(agentID string, cfg *config.Config, opts processOptions) memory.CallerScope {
	normalizeProcessOptionsInPlace(&opts)
	return callerScopeFromInbound(
		agentID,
		opts.Dispatch.SessionKey,
		opts.Dispatch.InboundContext,
		opts.Dispatch.SessionScope,
		cfg,
	)
}

func callerScopeFromInbound(
	agentID string,
	sessionKey string,
	inbound *bus.InboundContext,
	sessionScope *session.SessionScope,
	cfg *config.Config,
) memory.CallerScope {
	caller := memory.CallerScope{
		AgentID:    routing.NormalizeAgentID(agentID),
		SessionKey: strings.TrimSpace(sessionKey),
		SessionRef: memorySessionRef(sessionKey),
	}
	if inbound != nil {
		caller.Channel = strings.ToLower(strings.TrimSpace(inbound.Channel))
		caller.Account = routing.NormalizeAccountID(inbound.Account)
		caller.ChatID = strings.TrimSpace(inbound.ChatID)
		caller.TopicID = strings.TrimSpace(inbound.TopicID)
		caller.MessageRef = strings.TrimSpace(inbound.MessageID)
		caller.GroupID = memoryGroupID(*inbound)
		if inbound.Raw != nil {
			caller.TopicName = strings.TrimSpace(inbound.Raw["topic_name"])
		}
	}
	if sessionScope != nil {
		if caller.Channel == "" {
			caller.Channel = strings.ToLower(strings.TrimSpace(sessionScope.Channel))
		}
		if caller.Account == "" || caller.Account == routing.DefaultAccountID {
			caller.Account = routing.NormalizeAccountID(sessionScope.Account)
		}
	}
	if caller.Account == "" {
		caller.Account = routing.DefaultAccountID
	}
	if inbound != nil {
		identityLinks := map[string][]string(nil)
		if cfg != nil {
			identityLinks = cfg.Session.IdentityLinks
		}
		caller.UserKey = session.CanonicalUserScopeKey(
			caller.Channel,
			caller.Account,
			inbound.SenderID,
			identityLinks,
		)
	}
	return caller
}

func memorySessionRef(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(sessionKey))
	return "session_" + hex.EncodeToString(digest[:8])
}

func memoryGroupID(inbound bus.InboundContext) string {
	chatID := strings.TrimSpace(inbound.ChatID)
	if chatID == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(inbound.ChatType), "direct") {
		return ""
	}
	if topicID := strings.TrimSpace(inbound.TopicID); topicID != "" {
		suffix := "/" + topicID
		chatID = strings.TrimSuffix(chatID, suffix)
	}
	return strings.ToLower(strings.TrimSpace(chatID))
}
