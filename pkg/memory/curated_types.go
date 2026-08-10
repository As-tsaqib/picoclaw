package memory

import (
	"errors"
	"fmt"
	"time"
)

const (
	CuratedTargetWorkspace   = "workspace"
	CuratedTargetCurrentUser = "current_user"

	CuratedActionAdd     = "add"
	CuratedActionReplace = "replace"
	CuratedActionRemove  = "remove"
)

var (
	ErrCuratedDisabled       = errors.New("curated memory is disabled")
	ErrUserScopeUnavailable  = errors.New("trusted current-user scope is unavailable")
	ErrCuratedDuplicate      = errors.New("duplicate curated memory entry")
	ErrCuratedEntryNotFound  = errors.New("curated memory entry not found")
	ErrCuratedUnsafeContent  = errors.New("curated memory content was rejected")
	ErrCuratedInvalidTarget  = errors.New("invalid curated memory target")
	ErrCuratedInvalidAction  = errors.New("invalid curated memory action")
	ErrCuratedInvalidPending = errors.New("pending memory change not found")
)

// CapacityError is returned when an atomic mutation would exceed a configured
// scope limit. Callers can distinguish this from ordinary validation errors
// and ask the model to consolidate or remove stale entries.
type CapacityError struct {
	Target    string `json:"target"`
	Limit     int    `json:"limit"`
	Current   int    `json:"current"`
	Requested int    `json:"requested"`
}

func (e *CapacityError) Error() string {
	if e == nil {
		return "curated memory capacity exceeded"
	}
	return fmt.Sprintf(
		"curated memory capacity exceeded for %s (limit=%d, current=%d, requested=%d)",
		e.Target,
		e.Limit,
		e.Current,
		e.Requested,
	)
}

// CallerScope contains trusted runtime identity and topic provenance. UserKey
// is resolved by backend code from inbound channel/account identity and is
// never accepted as a model tool argument.
type CallerScope struct {
	AgentID    string `json:"agent_id"`
	UserKey    string `json:"-"`
	Channel    string `json:"channel,omitempty"`
	Account    string `json:"account,omitempty"`
	ChatID     string `json:"chat_id,omitempty"`
	GroupID    string `json:"group_id,omitempty"`
	TopicID    string `json:"topic_id,omitempty"`
	TopicName  string `json:"topic_name,omitempty"`
	SessionKey string `json:"-"`
	SessionRef string `json:"session_ref,omitempty"`
	MessageRef string `json:"message_ref,omitempty"`
}

// Provenance records where a curated fact came from without retaining a full
// transcript. Source is a compact category such as user_request, agent, or
// background_review.
type Provenance struct {
	Source     string    `json:"source"`
	SessionRef string    `json:"session_ref,omitempty"`
	Channel    string    `json:"channel,omitempty"`
	Account    string    `json:"account,omitempty"`
	TopicID    string    `json:"topic_id,omitempty"`
	TopicName  string    `json:"topic_name,omitempty"`
	MessageRef string    `json:"message_ref,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

type CuratedEntry struct {
	ID         string     `json:"id"`
	Content    string     `json:"content"`
	Provenance Provenance `json:"provenance"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type CuratedMutation struct {
	Action     string     `json:"action"`
	ID         string     `json:"id,omitempty"`
	Content    string     `json:"content,omitempty"`
	Provenance Provenance `json:"provenance"`
}

type PendingCuratedChange struct {
	ID        string            `json:"id"`
	Mutations []CuratedMutation `json:"mutations"`
	CreatedAt time.Time         `json:"created_at"`
}

type CuratedBatchResult struct {
	Applied []CuratedEntry        `json:"applied,omitempty"`
	Pending *PendingCuratedChange `json:"pending,omitempty"`
}

type CuratedStats struct {
	Target       string `json:"target"`
	Entries      int    `json:"entries"`
	Characters   int    `json:"characters"`
	Capacity     int    `json:"capacity"`
	PendingCount int    `json:"pending_count"`
}
