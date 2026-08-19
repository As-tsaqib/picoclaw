package bus

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

const pollStopRouteTokenPrefix = "pcpollstop:v1:"

// PollPayload represents a channel-neutral native poll or quiz.
type PollPayload struct {
	ID                     string    `json:"id,omitempty"` // local opaque handle
	Mode                   string    `json:"mode"`         // "regular" or "quiz"
	Question               string    `json:"question"`
	Options                []string  `json:"options"`
	IsAnonymous            bool      `json:"is_anonymous,omitempty"`
	AllowsMultipleAnswers  bool      `json:"allows_multiple_answers,omitempty"`
	AllowsRevoting         bool      `json:"allows_revoting,omitempty"`
	ShuffleOptions         bool      `json:"shuffle_options,omitempty"`
	AllowAddingOptions     bool      `json:"allow_adding_options,omitempty"`
	HideResultsUntilCloses bool      `json:"hide_results_until_closes,omitempty"`
	MembersOnly            bool      `json:"members_only,omitempty"`
	CountryCodes           []string  `json:"country_codes,omitempty"`
	CorrectOptionIDs       []int     `json:"correct_option_ids,omitempty"`
	Explanation            string    `json:"explanation,omitempty"`
	OpenPeriodSeconds      int       `json:"open_period_seconds,omitempty"`
	CloseAt                time.Time `json:"close_at,omitempty"`
	IsClosed               bool      `json:"is_closed,omitempty"`
	Description            string    `json:"description,omitempty"`
	FallbackText           string    `json:"fallback_text,omitempty"`
}

// PollStopRouteDigest returns a one-way proof of the trusted route that issued a
// stop-poll action. The digest intentionally includes every isolation boundary
// used by the poll registry while keeping raw account/chat/session/sender values
// out of the internal stop token and logs.
func PollStopRouteDigest(account, chatID, topicID, agentID, senderID, sessionKey string) string {
	canonicalChatID := strings.TrimSpace(chatID)
	canonicalTopicID := strings.TrimSpace(topicID)
	if canonicalTopicID != "" {
		canonicalChatID = strings.TrimSuffix(canonicalChatID, "/"+canonicalTopicID)
	}
	parts := []string{
		strings.ToLower(strings.TrimSpace(account)),
		canonicalChatID,
		canonicalTopicID,
		strings.TrimSpace(agentID),
		strings.TrimSpace(senderID),
		strings.TrimSpace(sessionKey),
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

// NewPollStopRouteToken binds an opaque poll handle to trusted current-route
// metadata. The model still supplies only the poll handle; route authority is
// injected by PicoClaw after tool argument parsing.
func NewPollStopRouteToken(handle, account, chatID, topicID, agentID, senderID, sessionKey string) string {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return ""
	}
	return pollStopRouteTokenPrefix + handle + ":" +
		PollStopRouteDigest(account, chatID, topicID, agentID, senderID, sessionKey)
}

// ParsePollStopRouteToken returns the original opaque handle and trusted-route
// digest. It rejects malformed values instead of treating them as raw handles.
func ParsePollStopRouteToken(value string) (handle, digest string, ok bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, pollStopRouteTokenPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(value, pollStopRouteTokenPrefix)
	separator := strings.LastIndexByte(rest, ':')
	if separator <= 0 || separator == len(rest)-1 {
		return "", "", false
	}
	handle = strings.TrimSpace(rest[:separator])
	digest = strings.TrimSpace(rest[separator+1:])
	if handle == "" || len(digest) != sha256.Size*2 {
		return "", "", false
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", "", false
	}
	return handle, strings.ToLower(digest), true
}
