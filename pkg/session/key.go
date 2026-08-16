package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/routing"
)

const (
	sessionKeyV1Prefix          = "sk_v1_"
	sessionInstanceV1Prefix     = "si_v1_"
	legacyAgentSessionKeyPrefix = "agent:"
)

type ParsedLegacySessionKey struct {
	AgentID string
	Rest    string
}

// BuildOpaqueSessionKey returns a stable opaque session key derived from a
// canonical alias string. The alias remains available through metadata for
// compatibility and migration purposes.
func BuildOpaqueSessionKey(alias string) string {
	normalized := strings.TrimSpace(strings.ToLower(alias))
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return sessionKeyV1Prefix + hex.EncodeToString(sum[:])
}

// IsOpaqueSessionKey returns true when the key matches the current opaque
// session-key format.
func IsOpaqueSessionKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	return strings.HasPrefix(normalized, sessionKeyV1Prefix) ||
		strings.HasPrefix(normalized, sessionInstanceV1Prefix)
}

// IsSessionInstanceKey reports whether key is a user-created multi-session
// instance. Callers must still validate its persisted scope before use.
func IsSessionInstanceKey(key string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), sessionInstanceV1Prefix)
}

func IsLegacyAgentSessionKey(key string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), legacyAgentSessionKeyPrefix)
}

func IsExplicitSessionKey(key string) bool {
	return IsOpaqueSessionKey(key) || IsLegacyAgentSessionKey(key)
}

func ParseLegacyAgentSessionKey(sessionKey string) *ParsedLegacySessionKey {
	raw := strings.TrimSpace(sessionKey)
	if raw == "" {
		return nil
	}
	parts := strings.SplitN(raw, ":", 3)
	if len(parts) < 3 || parts[0] != "agent" {
		return nil
	}
	agentID := strings.TrimSpace(parts[1])
	rest := parts[2]
	if agentID == "" || rest == "" {
		return nil
	}
	return &ParsedLegacySessionKey{AgentID: agentID, Rest: rest}
}

// ResolveAgentID returns the routed agent ID associated with a session. It
// prefers structured session scope metadata when available and falls back to
// legacy agent-scoped session keys for compatibility.
func ResolveAgentID(store any, sessionKey string) string {
	if scopeReader, ok := store.(interface {
		GetSessionScope(sessionKey string) *SessionScope
	}); ok {
		scope := scopeReader.GetSessionScope(sessionKey)
		if scope != nil && strings.TrimSpace(scope.AgentID) != "" {
			return routing.NormalizeAgentID(scope.AgentID)
		}
	}

	if parsed := ParseLegacyAgentSessionKey(sessionKey); parsed != nil {
		return routing.NormalizeAgentID(parsed.AgentID)
	}

	return ""
}

func BuildLegacyMainAlias(agentID string) string {
	return fmt.Sprintf("agent:%s:main", routing.NormalizeAgentID(agentID))
}

// BuildMainSessionKey returns the canonical opaque main-session key for an
// agent. The corresponding legacy alias remains available via
// BuildLegacyMainAlias for compatibility and migration logic.
func BuildMainSessionKey(agentID string) string {
	return BuildOpaqueSessionKey(BuildLegacyMainAlias(agentID))
}

// BuildSessionInstanceKey returns a fresh opaque key for a user-created
// session instance. It deliberately does not encode the route scope in the
// key; scope is persisted in metadata and validated server-side.
func BuildSessionInstanceKey() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("generate session instance key: %w", err)
	}
	return sessionInstanceV1Prefix + hex.EncodeToString(token[:]), nil
}

// ShortSessionID is a stable, non-sensitive selector suitable for displaying
// in diagnostics or accepting as a user-entered shorthand. It is not used as
// a capability and must always be resolved against a validated scope.
func ShortSessionID(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])[:8]
}

func BuildLegacyDirectAliases(agentID, channel, account, peerID string) []string {
	agentID = routing.NormalizeAgentID(agentID)
	channel = normalizeLegacyChannel(channel)
	account = routing.NormalizeAccountID(account)
	peerID = strings.ToLower(strings.TrimSpace(peerID))
	if peerID == "" {
		return nil
	}
	return []string{
		fmt.Sprintf("agent:%s:direct:%s", agentID, peerID),
		fmt.Sprintf("agent:%s:%s:direct:%s", agentID, channel, peerID),
		fmt.Sprintf("agent:%s:%s:%s:direct:%s", agentID, channel, account, peerID),
	}
}

func BuildLegacyPeerAlias(agentID, channel, peerKind, peerID string) string {
	agentID = routing.NormalizeAgentID(agentID)
	channel = normalizeLegacyChannel(channel)
	peerKind = strings.ToLower(strings.TrimSpace(peerKind))
	if peerKind == "" {
		peerKind = "unknown"
	}
	peerID = strings.ToLower(strings.TrimSpace(peerID))
	if peerID == "" {
		peerID = "unknown"
	}
	return fmt.Sprintf("agent:%s:%s:%s:%s", agentID, channel, peerKind, peerID)
}

// CanonicalSessionIdentityID collapses an identity using identity_links when
// possible, then returns a normalized lowercase identifier.
func CanonicalSessionIdentityID(channel, rawID string, identityLinks map[string][]string) string {
	normalizedID := strings.TrimSpace(rawID)
	if normalizedID == "" {
		return ""
	}
	if linked := resolveLinkedPeerID(identityLinks, channel, normalizedID); linked != "" {
		normalizedID = linked
	}
	return strings.ToLower(normalizedID)
}

// CanonicalUserScopeKey returns the stable channel/account/canonical-user key
// used by private per-user stores. Callers must supply a raw identity obtained
// from trusted runtime authentication metadata; this helper deliberately does
// not accept request- or model-selected scope information.
func CanonicalUserScopeKey(channel, account, rawID string, identityLinks map[string][]string) string {
	canonical := CanonicalSessionIdentityID(channel, rawID, identityLinks)
	if canonical == "" {
		return ""
	}
	return fmt.Sprintf(
		"channel:%s|account:%s|user:%s",
		strings.ToLower(strings.TrimSpace(channel)),
		routing.NormalizeAccountID(account),
		canonical,
	)
}

func normalizeLegacyChannel(channel string) string {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel == "" {
		return "unknown"
	}
	return channel
}

func resolveLinkedPeerID(identityLinks map[string][]string, channel, peerID string) string {
	if len(identityLinks) == 0 {
		return ""
	}
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return ""
	}

	candidates := make(map[string]bool)
	rawCandidate := strings.ToLower(peerID)
	if rawCandidate != "" {
		candidates[rawCandidate] = true
	}
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel != "" {
		candidates[fmt.Sprintf("%s:%s", channel, rawCandidate)] = true
	}
	if idx := strings.Index(rawCandidate, ":"); idx > 0 && idx < len(rawCandidate)-1 {
		candidates[rawCandidate[idx+1:]] = true
	}

	for canonical, ids := range identityLinks {
		canonicalName := strings.TrimSpace(canonical)
		if canonicalName == "" {
			continue
		}
		for _, id := range ids {
			normalized := strings.ToLower(strings.TrimSpace(id))
			if normalized != "" && candidates[normalized] {
				return canonicalName
			}
		}
	}
	return ""
}

// CanonicalScopeSignature returns a stable serialized representation of scope.
func CanonicalScopeSignature(scope SessionScope) string {
	parts := []string{
		fmt.Sprintf("v=%d", scope.Version),
		fmt.Sprintf("agent=%s", strings.TrimSpace(strings.ToLower(scope.AgentID))),
		fmt.Sprintf("channel=%s", strings.TrimSpace(strings.ToLower(scope.Channel))),
		fmt.Sprintf("account=%s", strings.TrimSpace(strings.ToLower(scope.Account))),
	}
	if scope.PrivateResponse {
		// Keep ephemeral history separate from a user's normal public group
		// history even when the configured public session dimensions happen to
		// include the sender. The route token itself is intentionally excluded
		// so the personal private session remains stable across turns.
		parts = append(parts, "private=ephemeral")
	}
	for _, dimension := range scope.Dimensions {
		dimension = strings.TrimSpace(strings.ToLower(dimension))
		if dimension == "" {
			continue
		}
		value := strings.TrimSpace(strings.ToLower(scope.Values[dimension]))
		parts = append(parts, fmt.Sprintf("%s=%s", dimension, value))
	}
	return strings.Join(parts, "|")
}

// BuildSessionKey returns the current opaque key for a structured session scope.
func BuildSessionKey(scope SessionScope) string {
	return BuildOpaqueSessionKey(CanonicalScopeSignature(scope))
}

// LegacyAliasesProveScope returns only aliases whose shape carries enough
// trusted route information to bind a metadata-free legacy session to scope.
// Generic aliases (for example agent:<id>:direct:<peer>) are intentionally
// rejected because they cannot distinguish Telegram accounts or channels.
func LegacyAliasesProveScope(scope *SessionScope, aliases []string) []string {
	if scope == nil {
		return nil
	}
	agent := routing.NormalizeAgentID(scope.AgentID)
	channel := strings.ToLower(strings.TrimSpace(scope.Channel))
	account := routing.NormalizeAccountID(scope.Account)
	if agent == "" || channel == "" {
		return nil
	}
	prefix := "agent:" + agent + ":"
	proven := make([]string, 0, len(aliases))
	seen := make(map[string]struct{}, len(aliases))
	for _, raw := range aliases {
		alias := strings.ToLower(strings.TrimSpace(raw))
		if alias == "" || isLegacyMainAlias(alias) || !strings.HasPrefix(alias, prefix) {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(alias, prefix), ":")
		if len(parts) < 2 || parts[0] != channel {
			continue
		}
		// Only the channel/account/direct form proves every required boundary.
		// Account-less direct aliases and legacy group aliases cannot prove the
		// originating bot account, even when the current account is "default".
		if len(parts) >= 4 && parts[1] == account && parts[2] == "direct" {
			if _, ok := seen[alias]; !ok {
				seen[alias] = struct{}{}
				proven = append(proven, alias)
			}
		}
	}
	return proven
}

// metadataAliasesForScope preserves the legacy alias contract for existing
// non-Telegram adapters while applying Telegram's stricter account boundary.
// Ambiguous Telegram aliases must never be persisted or promoted into a scoped
// session that can later appear in the multi-session catalog.
func metadataAliasesForScope(scope *SessionScope, aliases []string) []string {
	if scope == nil {
		return uniqueAliases(aliases)
	}
	isTelegram := strings.EqualFold(strings.TrimSpace(scope.Platform), "telegram") ||
		strings.TrimSpace(scope.BotAccount) != "" ||
		strings.EqualFold(strings.TrimSpace(scope.Channel), "telegram")
	if !isTelegram {
		return uniqueAliases(aliases)
	}
	return LegacyAliasesProveScope(scope, aliases)
}

func isLegacyMainAlias(alias string) bool {
	parts := strings.Split(alias, ":")
	return len(parts) == 3 && parts[0] == "agent" && parts[2] == "main"
}
