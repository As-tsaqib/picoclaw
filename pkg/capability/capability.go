package capability

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// State represents the availability state of a capability.
type State string

const (
	StateSupported   State = "supported"
	StateUnsupported State = "unsupported"
	StateConditional State = "conditional"
)

// Feature represents a semantic capability identifier.
type Feature string

const (
	FeatureMessageText           Feature = "message.text"
	FeatureMessageEdit           Feature = "message.edit"
	FeatureMessageStreamText     Feature = "message.stream.text"
	FeatureMessageStructuredRich Feature = "message.structured.rich"
	FeatureMessageStreamRich     Feature = "message.stream.rich"
	FeatureKeyboardInline        Feature = "keyboard.inline"
	FeatureKeyboardReply         Feature = "keyboard.reply"
	FeaturePollRegular           Feature = "poll.regular"
	FeaturePollQuiz              Feature = "poll.quiz"
	FeaturePollMultipleCorrect   Feature = "poll.multiple_correct"
	FeaturePollMedia             Feature = "poll.media"
	FeaturePollStop              Feature = "poll.stop"
	FeaturePollAnswers           Feature = "poll.answers"
	FeatureMessageEphemeral      Feature = "message.ephemeral"
	FeatureMediaImage            Feature = "media.image"
	FeatureMediaVideo            Feature = "media.video"
	FeatureMediaAudio            Feature = "media.audio"
	FeatureMediaVoice            Feature = "media.voice"
	FeatureMediaDocument         Feature = "media.document"
	FeatureMediaGroup            Feature = "media.group"
	FeatureMediaAnimation        Feature = "media.animation"
	FeatureMediaSticker          Feature = "media.sticker"
	FeatureMediaVideoNote        Feature = "media.video_note"
	FeatureMediaLivePhoto        Feature = "media.live_photo"
	FeatureLocationPoint         Feature = "location.point"
	FeatureLocationVenue         Feature = "location.venue"
	FeatureContactCard           Feature = "contact.card"
	FeatureDiceAnimated          Feature = "dice.animated"
	FeatureChecklistNative       Feature = "checklist.native"
)

// CapabilityInfo describes the state and conditions for a feature.
type CapabilityInfo struct {
	State     State  `json:"state"`
	Condition string `json:"condition,omitempty"`
}

// CapabilitySet is a collection of features mapped to their state.
type CapabilitySet map[Feature]CapabilityInfo

// IsSupported returns true if the feature is explicitly supported.
func (cs CapabilitySet) IsSupported(f Feature) bool {
	if cs == nil {
		return false
	}
	info, ok := cs[f]
	return ok && info.State == StateSupported
}

// RouteContext holds runtime information about the active route.
type RouteContext struct {
	Channel            string
	Account            string
	ChatID             string
	ChatType           string
	TopicID            string
	SenderID           string
	ServerID           string
	HasBusinessContext bool
	IsEphemeral        bool
}

const (
	defaultNegativeCacheTTL = 10 * time.Minute
	maxNegativeCacheEntries = 500
)

type negativeCacheEntry struct {
	ExpiresAt time.Time
	Reason    string
}

// NegativeCapabilityCache tracks runtime method failures to downgrade capabilities.
type NegativeCapabilityCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]negativeCacheEntry
}

// NewNegativeCapabilityCache creates a new negative capability cache.
func NewNegativeCapabilityCache(ttl time.Duration) *NegativeCapabilityCache {
	if ttl <= 0 {
		ttl = defaultNegativeCacheTTL
	}
	return &NegativeCapabilityCache{
		ttl:     ttl,
		entries: make(map[string]negativeCacheEntry),
	}
}

// GlobalNegativeCache is the shared runtime cache for capability downgrades.
var GlobalNegativeCache = NewNegativeCapabilityCache(defaultNegativeCacheTTL)

// NormalizeServerID returns a canonical representation of a server endpoint.
// Empty strings or the official Telegram API URL are normalized to "official".
func NormalizeServerID(serverID string) string {
	s := strings.ToLower(strings.TrimSpace(serverID))
	s = strings.TrimRight(s, "/")
	if s == "" || s == "https://api.telegram.org" || s == "http://api.telegram.org" || s == "official" {
		return "official"
	}
	return s
}

func cacheKey(channel, account, serverID string, feature Feature) string {
	channel = strings.ToLower(strings.TrimSpace(channel))
	account = strings.ToLower(strings.TrimSpace(account))
	serverID = NormalizeServerID(serverID)
	return fmt.Sprintf("%s|%s|%s|%s", channel, account, serverID, feature)
}

// RecordFailure checks if the error indicates an unsupported method/feature, and records it if so.
func (c *NegativeCapabilityCache) RecordFailure(channel, account, serverID string, feature Feature, err error) bool {
	if c == nil || err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())

	// Distinguish genuine unsupported method from transient network/auth/rate-limit errors
	// Do NOT downgrade on 401, 403, 429, timeout, network error
	if strings.Contains(errStr, "401") || strings.Contains(errStr, "403") ||
		strings.Contains(errStr, "429") || strings.Contains(errStr, "too many requests") ||
		strings.Contains(errStr, "timeout") || strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "forbidden") {
		return false
	}

	isUnsupported := strings.Contains(errStr, "method not found") ||
		strings.Contains(errStr, "not supported") ||
		strings.Contains(errStr, "unknown method") ||
		strings.Contains(errStr, "bad request: method") ||
		strings.Contains(errStr, "cannot be used") ||
		strings.Contains(errStr, "unsupported") ||
		strings.Contains(errStr, "not implemented")

	if !isUnsupported {
		return false
	}

	key := cacheKey(channel, account, serverID, feature)
	now := time.Now().UTC()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Prune if over capacity
	if len(c.entries) >= maxNegativeCacheEntries {
		for k, v := range c.entries {
			if now.After(v.ExpiresAt) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= maxNegativeCacheEntries {
			for k := range c.entries {
				delete(c.entries, k)
				break
			}
		}
	}

	c.entries[key] = negativeCacheEntry{
		ExpiresAt: now.Add(c.ttl),
		Reason:    err.Error(),
	}
	return true
}

// IsDowngraded checks whether a capability is currently recorded as unsupported.
func (c *NegativeCapabilityCache) IsDowngraded(channel, account, serverID string, feature Feature) bool {
	if c == nil {
		return false
	}
	key := cacheKey(channel, account, serverID, feature)

	c.mu.RLock()
	entry, exists := c.entries[key]
	c.mu.RUnlock()

	if !exists {
		return false
	}
	if time.Now().UTC().After(entry.ExpiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return false
	}
	return true
}

// Clear clears all cache entries.
func (c *NegativeCapabilityCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]negativeCacheEntry)
}

// ResolveRouteCapabilities returns the capability set for the given route.
func ResolveRouteCapabilities(route RouteContext, cache *NegativeCapabilityCache) CapabilitySet {
	channel := strings.ToLower(strings.TrimSpace(route.Channel))
	set := make(CapabilitySet)

	if channel == "telegram" {
		features := []Feature{
			FeatureMessageText,
			FeatureMessageEdit,
			FeatureMessageStreamText,
			FeatureMessageStructuredRich,
			FeatureKeyboardInline,
			FeaturePollRegular,
			FeaturePollQuiz,
			FeaturePollMultipleCorrect,
			FeaturePollStop,
			FeaturePollAnswers,
			FeatureMediaImage,
			FeatureMediaVideo,
			FeatureMediaAudio,
			FeatureMediaVoice,
			FeatureMediaDocument,
			FeatureMediaGroup,
			FeatureMediaAnimation,
			FeatureMediaSticker,
			FeatureMediaVideoNote,
			FeatureLocationPoint,
			FeatureLocationVenue,
			FeatureContactCard,
			FeatureDiceAnimated,
		}

		for _, f := range features {
			if cache != nil && cache.IsDowngraded(route.Channel, route.Account, route.ServerID, f) {
				set[f] = CapabilityInfo{State: StateUnsupported, Condition: "downgraded_by_server"}
			} else {
				set[f] = CapabilityInfo{State: StateSupported}
			}
		}

		// Ephemeral delivery is conditional
		if route.IsEphemeral {
			set[FeatureMessageEphemeral] = CapabilityInfo{State: StateSupported}
		} else {
			set[FeatureMessageEphemeral] = CapabilityInfo{State: StateConditional, Condition: "ephemeral_route"}
		}

		// Checklist requires business connection
		if route.HasBusinessContext {
			set[FeatureChecklistNative] = CapabilityInfo{State: StateSupported}
		} else {
			set[FeatureChecklistNative] = CapabilityInfo{State: StateConditional, Condition: "business_connection"}
		}
		set[FeatureMediaLivePhoto] = CapabilityInfo{State: StateUnsupported, Condition: "not_implemented"}
		set[FeaturePollMedia] = CapabilityInfo{State: StateUnsupported, Condition: "not_implemented"}

		// Rich stream draft
		if cache != nil && cache.IsDowngraded(route.Channel, route.Account, route.ServerID, FeatureMessageStreamRich) {
			set[FeatureMessageStreamRich] = CapabilityInfo{State: StateUnsupported}
		} else {
			set[FeatureMessageStreamRich] = CapabilityInfo{State: StateUnsupported, Condition: "text_draft_only"}
		}
	} else if channel == "pico" {
		set[FeatureMessageText] = CapabilityInfo{State: StateSupported}
		set[FeatureMessageStreamText] = CapabilityInfo{State: StateSupported}
		set[FeatureMediaImage] = CapabilityInfo{State: StateSupported}
		set[FeatureMediaDocument] = CapabilityInfo{State: StateSupported}
	} else {
		// Generic default channel
		set[FeatureMessageText] = CapabilityInfo{State: StateSupported}
		set[FeatureMessageStreamText] = CapabilityInfo{State: StateSupported}
	}

	return set
}

// FormatCapabilityPrompt constructs the system prompt section describing route capabilities.
func FormatCapabilityPrompt(set CapabilitySet, channel string) string {
	var sb strings.Builder
	sb.WriteString("# Delivery capabilities\n")
	sb.WriteString(fmt.Sprintf("channel=%s\n", strings.ToLower(strings.TrimSpace(channel))))

	keys := []struct {
		feat Feature
		name string
	}{
		{FeaturePollQuiz, "native_quiz"},
		{FeaturePollRegular, "native_poll"},
		{FeaturePollStop, "stop_poll"},
		{FeatureMessageStructuredRich, "rich_message"},
		{FeatureMessageStreamRich, "rich_stream"},
		{FeatureKeyboardInline, "inline_keyboard"},
		{FeatureMediaAnimation, "animation"},
		{FeatureMediaSticker, "sticker"},
		{FeatureMediaVideoNote, "video_note"},
		{FeatureLocationPoint, "location"},
		{FeatureLocationVenue, "venue"},
		{FeatureContactCard, "contact"},
		{FeatureDiceAnimated, "dice"},
		{FeatureChecklistNative, "checklist"},
	}

	for _, k := range keys {
		info, ok := set[k.feat]
		if !ok {
			sb.WriteString(fmt.Sprintf("%s=unsupported\n", k.name))
			continue
		}
		if info.State == StateConditional && info.Condition != "" {
			sb.WriteString(fmt.Sprintf("%s=conditional:%s\n", k.name, info.Condition))
		} else {
			sb.WriteString(fmt.Sprintf("%s=%s\n", k.name, info.State))
		}
	}

	sb.WriteString("\nNote: Never invent raw Telegram API calls. Use semantic tools when available. " +
		"Native rendering is preferred when user preference requests it and capability is supported. " +
		"If capability is unavailable, use declared fallback.")

	return sb.String()
}
