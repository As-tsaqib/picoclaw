package session

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/routing"
)

// Allocation contains the concrete session keys selected for a routed turn.
// The current implementation intentionally preserves the legacy session-key
// layout while moving key construction out of the router.
type Allocation struct {
	Scope          SessionScope
	SessionKey     string
	SessionAliases []string
	MainSessionKey string
	MainAliases    []string
}

// AllocationInput contains the routing result and peer context needed to
// derive the session keys for a turn.
type AllocationInput struct {
	AgentID       string
	Context       bus.InboundContext
	SessionPolicy routing.SessionPolicy
}

// AllocateRouteSession maps a route decision onto a structured scope and the
// current opaque session-key format.
func AllocateRouteSession(input AllocationInput) Allocation {
	scope := buildSessionScope(input)
	legacySessionAliases := buildLegacySessionAliases(input)
	legacyMainSessionKey := strings.ToLower(BuildLegacyMainAlias(input.AgentID))
	return Allocation{
		Scope:          scope,
		SessionKey:     BuildSessionKey(scope),
		SessionAliases: legacySessionAliases,
		MainSessionKey: BuildOpaqueSessionKey(legacyMainSessionKey),
		MainAliases:    []string{legacyMainSessionKey},
	}
}

func buildSessionScope(input AllocationInput) SessionScope {
	inbound := input.Context
	includeTopicInChatDimension := shouldPreserveTelegramForumIsolation(input)
	scope := SessionScope{
		Version:           ScopeVersionV1,
		AgentID:           routing.NormalizeAgentID(input.AgentID),
		Channel:           strings.ToLower(strings.TrimSpace(inbound.Channel)),
		Account:           routing.NormalizeAccountID(inbound.Account),
		PrivateResponse:   inbound.PrivateResponse,
		PrivateRouteToken: inbound.PrivateRouteToken,
	}
	if scope.Channel == "" {
		scope.Channel = "unknown"
	}

	policyDimensions := append([]string(nil), input.SessionPolicy.Dimensions...)
	if inbound.PrivateSession {
		// A personal group session must always be scoped by both the verified
		// Telegram group and sender, regardless of a custom public-session
		// policy. Otherwise the same user could share private history across
		// groups, or two users could share one group history.
		policyDimensions = ensurePrivateDimension(policyDimensions, "chat")
		policyDimensions = ensurePrivateDimension(policyDimensions, "sender")
	}
	dimensions := make([]string, 0, len(policyDimensions))
	values := make(map[string]string, len(policyDimensions))

	for _, dimension := range policyDimensions {
		switch dimension {
		case "space":
			if spaceID := strings.TrimSpace(inbound.SpaceID); spaceID != "" {
				spaceType := strings.ToLower(strings.TrimSpace(inbound.SpaceType))
				if spaceType == "" {
					spaceType = "space"
				}
				dimensions = append(dimensions, "space")
				values["space"] = fmt.Sprintf("%s:%s", spaceType, strings.ToLower(spaceID))
			}
		case "chat":
			chatID := strings.TrimSpace(inbound.ChatID)
			if chatID == "" {
				continue
			}
			if includeTopicInChatDimension {
				if topicID := strings.TrimSpace(inbound.TopicID); topicID != "" {
					chatID = chatID + "/" + topicID
				}
			}
			chatType := strings.ToLower(strings.TrimSpace(inbound.ChatType))
			if chatType == "" {
				chatType = "direct"
			}
			dimensions = append(dimensions, "chat")
			values["chat"] = fmt.Sprintf("%s:%s", chatType, strings.ToLower(chatID))
		case "topic":
			if topicID := strings.TrimSpace(inbound.TopicID); topicID != "" {
				dimensions = append(dimensions, "topic")
				values["topic"] = "topic:" + strings.ToLower(topicID)
			}
		case "sender":
			senderID := privateOrCanonicalSenderID(input)
			if senderID == "" {
				continue
			}
			dimensions = append(dimensions, "sender")
			values["sender"] = senderID
		}
	}

	if len(dimensions) > 0 {
		scope.Dimensions = dimensions
		scope.Values = values
	}

	applySessionOriginMetadata(&scope, input, policyDimensions)
	return scope
}

func applySessionOriginMetadata(scope *SessionScope, input AllocationInput, dimensions []string) {
	if scope == nil {
		return
	}
	inbound := input.Context
	scope.OriginChannel = strings.ToLower(strings.TrimSpace(inbound.Channel))
	scope.OriginAccount = routing.NormalizeAccountID(inbound.Account)
	scope.OriginAgentID = routing.NormalizeAgentID(input.AgentID)
	scope.OriginChatID = strings.TrimSpace(inbound.ChatID)
	scope.OriginTopicID = strings.TrimSpace(inbound.TopicID)
	scope.OriginSenderID = strings.TrimSpace(inbound.SenderID)
	scope.OriginChatType = strings.ToLower(strings.TrimSpace(inbound.ChatType))
	scope.OriginRoute = sessionOriginRoute(scope)

	if !isTelegramInboundContext(inbound) {
		return
	}
	scope.Platform = "telegram"
	// Telegram Channel is the configured channel instance name and therefore
	// the bot-account boundary. Keep Account separately for routing policy.
	scope.BotAccount = strings.TrimSpace(inbound.Channel)
	scope.OriginChannel = "telegram"
	scope.OriginRoute = sessionOriginRoute(scope)

	if owner := trustedTelegramOwner(inbound, dimensions); owner != "" {
		scope.OwnerUserID = owner
	}
}

func isTelegramInboundContext(inbound bus.InboundContext) bool {
	if strings.EqualFold(strings.TrimSpace(inbound.Raw["platform"]), "telegram") {
		return true
	}
	// Preserve compatibility with the standard singleton channel name used by
	// older metadata before adapters started recording the platform explicitly.
	return strings.EqualFold(strings.TrimSpace(inbound.Channel), "telegram")
}

func trustedTelegramOwner(inbound bus.InboundContext, dimensions []string) string {
	userID := strings.TrimSpace(inbound.SenderID)
	parsed, err := strconv.ParseInt(userID, 10, 64)
	if err != nil || parsed <= 0 {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(inbound.ChatType), "direct") ||
		inbound.PrivateSession || inbound.PrivateResponse || containsSessionDimension(dimensions, "sender") {
		return strconv.FormatInt(parsed, 10)
	}
	return ""
}

func containsSessionDimension(dimensions []string, target string) bool {
	for _, dimension := range dimensions {
		if strings.EqualFold(strings.TrimSpace(dimension), target) {
			return true
		}
	}
	return false
}

func sessionOriginRoute(scope *SessionScope) string {
	if scope == nil {
		return ""
	}
	channel := strings.TrimSpace(scope.OriginChannel)
	if channel == "" {
		channel = strings.TrimSpace(scope.Channel)
	}
	parts := []string{channel}
	if account := strings.TrimSpace(scope.BotAccount); account != "" {
		parts = append(parts, account)
	} else if account := strings.TrimSpace(scope.OriginAccount); account != "" {
		parts = append(parts, account)
	}
	if chat := strings.TrimSpace(scope.OriginChatID); chat != "" {
		parts = append(parts, chat)
	}
	if topic := strings.TrimSpace(scope.OriginTopicID); topic != "" {
		parts = append(parts, "topic="+topic)
	}
	return strings.Join(parts, "/")
}

func buildLegacySessionAliases(input AllocationInput) []string {
	aliases := []string{strings.ToLower(BuildLegacyMainAlias(input.AgentID))}
	inbound := input.Context

	if strings.EqualFold(strings.TrimSpace(inbound.ChatType), "direct") {
		peerIDs := buildLegacyDirectPeerIDs(input)
		if len(peerIDs) == 0 {
			return uniqueAliases(aliases)
		}
		for _, peerID := range peerIDs {
			aliases = append(
				aliases,
				BuildLegacyDirectAliases(input.AgentID, inbound.Channel, inbound.Account, peerID)...,
			)
		}
		return uniqueAliases(aliases)
	}

	peerID := strings.TrimSpace(inbound.ChatID)
	if peerID == "" {
		return uniqueAliases(aliases)
	}
	if topicID := strings.TrimSpace(inbound.TopicID); topicID != "" {
		peerID = peerID + "/" + topicID
	}
	if inbound.PrivateSession {
		senderID := privateOrCanonicalSenderID(input)
		if senderID != "" {
			peerID = peerID + "/private/" + senderID
		}
	}
	aliases = append(aliases, BuildLegacyPeerAlias(
		input.AgentID,
		inbound.Channel,
		strings.ToLower(strings.TrimSpace(inbound.ChatType)),
		peerID,
	))

	return uniqueAliases(aliases)
}

func privateOrCanonicalSenderID(input AllocationInput) string {
	if input.Context.PrivateSession {
		// Identity links are intentionally ignored for private Telegram turns.
		// The verified platform user ID is the security boundary, even when an
		// operator has linked multiple public identities to one canonical user.
		return strings.ToLower(strings.TrimSpace(input.Context.SenderID))
	}
	return CanonicalSessionIdentityID(
		input.Context.Channel,
		input.Context.SenderID,
		input.SessionPolicy.IdentityLinks,
	)
}

func ensurePrivateDimension(dimensions []string, target string) []string {
	for i, dimension := range dimensions {
		if strings.EqualFold(strings.TrimSpace(dimension), target) {
			// The allocator's switch uses canonical lowercase names. Normalize a
			// case variant here so a custom policy cannot accidentally omit one of
			// the mandatory private security dimensions.
			dimensions[i] = target
			return dimensions
		}
	}
	return append(dimensions, target)
}

func shouldPreserveTelegramForumIsolation(input AllocationInput) bool {
	inbound := input.Context
	if !isTelegramInboundContext(inbound) {
		return false
	}
	if strings.TrimSpace(inbound.TopicID) == "" {
		return false
	}
	for _, dimension := range input.SessionPolicy.Dimensions {
		if strings.EqualFold(strings.TrimSpace(dimension), "topic") {
			return false
		}
	}
	return true
}

func buildLegacyDirectPeerIDs(input AllocationInput) []string {
	inbound := input.Context
	peerIDs := make([]string, 0, 3)

	rawSenderID := strings.TrimSpace(inbound.SenderID)
	if rawSenderID != "" {
		peerIDs = append(peerIDs, strings.ToLower(rawSenderID))
	}

	canonicalSenderID := CanonicalSessionIdentityID(
		inbound.Channel,
		inbound.SenderID,
		input.SessionPolicy.IdentityLinks,
	)
	if canonicalSenderID != "" {
		peerIDs = append(peerIDs, canonicalSenderID)
	}

	chatID := strings.TrimSpace(inbound.ChatID)
	if chatID != "" {
		peerIDs = append(peerIDs, strings.ToLower(chatID))
	}

	return uniqueAliases(peerIDs)
}

func uniqueAliases(aliases []string) []string {
	if len(aliases) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(aliases))
	seen := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		alias = strings.TrimSpace(strings.ToLower(alias))
		if alias == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		normalized = append(normalized, alias)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
