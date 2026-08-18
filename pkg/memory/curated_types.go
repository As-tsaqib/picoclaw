package memory

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CuratedTargetWorkspace   = "workspace"
	CuratedTargetCurrentUser = "current_user"

	CuratedActionAdd     = "add"
	CuratedActionReplace = "replace"
	CuratedActionRemove  = "remove"
	CuratedActionPin     = "pin"
	CuratedActionUnpin   = "unpin"
	CuratedActionArchive = "archive"
	CuratedActionRestore = "restore"

	CuratedTypeIdentity                = "identity"
	CuratedTypeCommunicationPreference = "communication_preference"
	CuratedTypeWorkflowPreference      = "workflow_preference"
	CuratedTypeCorrection              = "correction"
	CuratedTypeEnvironment             = "environment"
	CuratedTypeProjectFact             = "project_fact"
	CuratedTypeRelationship            = "relationship"
	CuratedTypeEpisodicFact            = "episodic_fact"
	CuratedTypeOther                   = "other"

	CuratedStatusActive     = "active"
	CuratedStatusSuperseded = "superseded"
	CuratedStatusArchived   = "archived"

	CuratedEvidenceExplicit = "explicit"
	CuratedEvidenceObserved = "observed"
	CuratedEvidenceInferred = "inferred"
	CuratedEvidenceLegacy   = "legacy"

	CuratedVisibilityBehavioral = "behavioral"
	CuratedVisibilityPrivate    = "private"
	CuratedVisibilityShared     = "shared"
)

var (
	ErrCuratedDisabled             = errors.New("curated memory is disabled")
	ErrUserScopeUnavailable        = errors.New("trusted current-user scope is unavailable")
	ErrPrivateContextRequired      = errors.New("private context is required for current-user memory management")
	ErrCuratedDuplicate            = errors.New("duplicate curated memory entry")
	ErrCuratedEntryNotFound        = errors.New("curated memory entry not found")
	ErrCuratedUnsafeContent        = errors.New("curated memory content was rejected")
	ErrCuratedInvalidTarget        = errors.New("invalid curated memory target")
	ErrCuratedInvalidAction        = errors.New("invalid curated memory action")
	ErrCuratedInvalidType          = errors.New("invalid curated memory type")
	ErrCuratedInvalidStatus        = errors.New("invalid curated memory status")
	ErrCuratedInvalidEvidence      = errors.New("invalid curated memory evidence kind")
	ErrCuratedInvalidPreferenceKey = errors.New("invalid curated memory preference key")
	ErrCuratedInvalidPending       = errors.New("pending memory change not found")
	ErrCuratedSensitiveInference   = errors.New("unsupported sensitive or psychological inference")
	ErrCuratedUnsupportedVersion   = errors.New("unsupported curated memory schema version")
	ErrCuratedMalformedDocument    = errors.New("malformed curated memory document")
)

// CapacityError is returned when an atomic mutation would exceed a configured
// scope limit. Callers can distinguish this from ordinary validation errors
// and ask the model to consolidate or remove stale entries.
type CapacityError struct {
	Target    string `json:"target"`
	Resource  string `json:"resource,omitempty"`
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

// AllowsPrivateUserMemory reports whether trusted runtime scope is sufficient
// to load or mutate current-user data without exposing it to other chat
// participants. It is shared by prompt assembly, tools, and benchmarks so the
// fail-closed group boundary cannot drift between those paths.
func AllowsPrivateUserMemory(caller CallerScope) bool {
	return strings.TrimSpace(caller.UserKey) != "" && strings.TrimSpace(caller.GroupID) == ""
}

// HasCanonicalUserMemoryScope reports whether the runtime resolved a stable
// authenticated user identity. Group/topic location does not change ownership.
func HasCanonicalUserMemoryScope(caller CallerScope) bool {
	return strings.TrimSpace(caller.UserKey) != ""
}

// IsSharedMemoryContext reports whether a response is visible to other chat participants.
func IsSharedMemoryContext(caller CallerScope) bool {
	return strings.TrimSpace(caller.GroupID) != ""
}

func NormalizeCuratedVisibility(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CuratedVisibilityBehavioral:
		return CuratedVisibilityBehavioral
	case CuratedVisibilityPrivate:
		return CuratedVisibilityPrivate
	case CuratedVisibilityShared:
		return CuratedVisibilityShared
	default:
		return ""
	}
}

func ValidCuratedVisibility(value string) bool {
	return NormalizeCuratedVisibility(value) != ""
}

// EffectiveVisibility is deliberately conservative for legacy records. Stable
// communication/workflow preferences are safe behavioral defaults; all other
// personal entries default private until explicitly classified.
func (entry CuratedEntry) EffectiveVisibility() string {
	if visibility := NormalizeCuratedVisibility(entry.Visibility); visibility != "" {
		return visibility
	}
	key := NormalizePreferenceKey(entry.PreferenceKey)
	switch entry.EffectiveType() {
	case CuratedTypeCommunicationPreference, CuratedTypeWorkflowPreference:
		return CuratedVisibilityBehavioral
	}
	if strings.HasPrefix(key, "communication.") || strings.HasPrefix(key, "workflow.") ||
		strings.HasPrefix(key, "formatting.") || strings.HasPrefix(key, "coding.") ||
		strings.HasPrefix(key, "language.") || strings.HasPrefix(key, "tooling.") {
		return CuratedVisibilityBehavioral
	}
	return CuratedVisibilityPrivate
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
	ID               string     `json:"id"`
	Content          string     `json:"content"`
	Type             string     `json:"type,omitempty"`
	Status           string     `json:"status,omitempty"`
	Pinned           bool       `json:"pinned,omitempty"`
	Confidence       float64    `json:"confidence,omitempty"`
	EvidenceKind     string     `json:"evidence_kind,omitempty"`
	Visibility       string     `json:"visibility,omitempty"`
	EvidenceCount    int        `json:"evidence_count,omitempty"`
	ObservationCount int        `json:"observation_count,omitempty"`
	PreferenceKey    string     `json:"preference_key,omitempty"`
	PreferenceValue  string     `json:"preference_value,omitempty"`
	Supersedes       string     `json:"supersedes,omitempty"`
	Provenance       Provenance `json:"provenance"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	LastVerifiedAt   *time.Time `json:"last_verified_at,omitempty"` // legacy alias for confirmed evidence
	LastConfirmedAt  *time.Time `json:"last_confirmed_at,omitempty"`
	LastPresentedAt  *time.Time `json:"last_presented_at,omitempty"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"` // deprecated compatibility field
	ArchivedAt       *time.Time `json:"archived_at,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
}

type CuratedMutation struct {
	Action           string     `json:"action"`
	ID               string     `json:"id,omitempty"`
	Content          string     `json:"content,omitempty"`
	Type             string     `json:"type,omitempty"`
	Confidence       *float64   `json:"confidence,omitempty"`
	EvidenceKind     string     `json:"evidence_kind,omitempty"`
	Visibility       string     `json:"visibility,omitempty"`
	EvidenceCount    int        `json:"evidence_count,omitempty"`
	ObservationCount int        `json:"observation_count,omitempty"`
	PreferenceKey    string     `json:"preference_key,omitempty"`
	PreferenceValue  string     `json:"preference_value,omitempty"`
	Supersedes       string     `json:"supersedes,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	LastVerifiedAt   *time.Time `json:"last_verified_at,omitempty"`
	LastConfirmedAt  *time.Time `json:"last_confirmed_at,omitempty"`
	Provenance       Provenance `json:"provenance"`
}

type PendingCuratedChange struct {
	ID        string            `json:"id"`
	Mutations []CuratedMutation `json:"mutations"`
	CreatedAt time.Time         `json:"created_at"`
}

type CuratedBatchResult struct {
	Applied   []CuratedEntry        `json:"applied,omitempty"`
	Pending   *PendingCuratedChange `json:"pending,omitempty"`
	Conflicts []CuratedConflict     `json:"conflicts,omitempty"`
	Outcomes  []string              `json:"outcomes,omitempty"`
}

// CuratedConflict is a bounded, non-destructive near-duplicate hint. It lets
// an interactive caller consolidate or explicitly supersede an existing fact
// without the store silently overwriting ambiguous information.
type CuratedConflict struct {
	MutationIndex int     `json:"mutation_index"`
	EntryID       string  `json:"entry_id"`
	Similarity    float64 `json:"similarity"`
	Reason        string  `json:"reason"`
}

type CuratedRetrievalOptions struct {
	Query               string
	MaxResults          int
	MaxChars            int
	PinnedChars         int
	MinimumScore        float64
	RecencyWeight       float64
	RecencyHalfLifeDays float64
	StaleAfterDays      float64
	FuzzyWeight         float64
	RecentFallbackCount int
	Now                 time.Time
	// SemanticScore is an optional bounded local reranker. It returns 0..1 and
	// is deliberately not serialized or persisted. Lexical retrieval remains
	// the dependency-free default when it is nil.
	SemanticScore  func(query, candidate string) float64
	SemanticWeight float64
}

type CuratedRetrievalResult struct {
	Entries    []CuratedEntry `json:"entries"`
	Characters int            `json:"characters"`
}

type CuratedUsage struct {
	Target string   `json:"target"`
	IDs    []string `json:"ids"`
}

type CuratedStats struct {
	Target               string `json:"target"`
	Entries              int    `json:"entries"`
	EntryCapacity        int    `json:"entry_capacity"`
	Characters           int    `json:"characters"`
	Capacity             int    `json:"capacity"`
	SerializedCharacters int    `json:"serialized_characters"`
	SerializedCapacity   int    `json:"serialized_capacity"`
	PendingCount         int    `json:"pending_count"`
	PendingCapacity      int    `json:"pending_capacity"`
}
