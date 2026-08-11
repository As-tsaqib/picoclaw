package config

import (
	"errors"
	"fmt"
	"strings"
)

const (
	MemoryNotificationOff     = "off"
	MemoryNotificationOn      = "on"
	MemoryNotificationVerbose = "verbose"

	MemoryRecallIsolated    = "isolated"
	MemoryRecallUserRecall  = "user_recall"
	MemoryRecallGroupRecall = "group_recall"

	MemoryApprovalOff            = "off"
	MemoryApprovalBackgroundOnly = "background_only"
	MemoryApprovalAllWrites      = "all_writes"

	MemoryRetrievalHybridLexical = "hybrid_lexical"

	DefaultWorkspaceMemoryCharLimit = 12_000
	DefaultPerUserMemoryCharLimit   = 8_000
	DefaultMemoryReviewInterval     = 10
	DefaultMemoryReviewTimeout      = 30
	DefaultMemoryReviewIterations   = 2
	DefaultMemoryRecallResults      = 5
	DefaultMemoryRecallChars        = 4_000
	DefaultMemoryRecallRecords      = 2_000
	DefaultCheckpointCount          = 100
	DefaultCheckpointContextChars   = 2_000
	DefaultCheckpointRetentionDays  = 90
	DefaultMemoryWorkspaceResults   = 6
	DefaultMemoryUserResults        = 6
	DefaultMemoryRetrievalChars     = 4_000
	DefaultMemoryPinnedChars        = 1_200
	DefaultMemoryMinRelevance       = 0.35
	DefaultMemoryRecencyWeight      = 0.25
	DefaultMemoryRecencyHalfLife    = 90
	DefaultMemoryFuzzyWeight        = 0.75
	DefaultMemoryRecentFallback     = 2
	DefaultMemoryArchivedRetention  = 365
	DefaultMemoryStaleThreshold     = 180

	MaxMemoryReviewIterations = 4
	MaxMemoryRecallResults    = 20
	MaxMemoryRecallChars      = 20_000
	MaxMemoryRecallRecords    = 20_000
	MaxCheckpointCount        = 1_000
	MaxCheckpointContextChars = 20_000
	MaxMemoryRetrievalResults = 50
	MaxMemoryRetrievalChars   = 20_000
	MaxMemoryPinnedChars      = 10_000
	MaxMemoryLifecycleDays    = 3_650
)

// MemoryConfig controls structured curated memory, bounded transcript recall,
// and resumable task checkpoints. Curated memory and its bounded background
// reviewer default to enabled; operators should account for the reviewer's
// additional provider token/API usage.
type MemoryConfig struct {
	Enabled            bool                   `json:"enabled"                        env:"PICOCLAW_MEMORY_ENABLED"`
	WorkspaceCharLimit int                    `json:"workspace_char_limit,omitempty" env:"PICOCLAW_MEMORY_WORKSPACE_CHAR_LIMIT"`
	PerUserCharLimit   int                    `json:"per_user_char_limit,omitempty"  env:"PICOCLAW_MEMORY_PER_USER_CHAR_LIMIT"`
	WriteApproval      bool                   `json:"write_approval,omitempty"       env:"PICOCLAW_MEMORY_WRITE_APPROVAL"`
	ApprovalMode       string                 `json:"approval_mode,omitempty"        env:"PICOCLAW_MEMORY_APPROVAL_MODE"`
	Notifications      string                 `json:"notifications,omitempty"        env:"PICOCLAW_MEMORY_NOTIFICATIONS"`
	BackgroundReview   MemoryReviewConfig     `json:"background_review,omitempty"`
	Retrieval          MemoryRetrievalConfig  `json:"retrieval,omitempty"`
	Lifecycle          MemoryLifecycleConfig  `json:"lifecycle,omitempty"`
	Recall             MemoryRecallConfig     `json:"recall,omitempty"`
	Checkpoints        MemoryCheckpointConfig `json:"checkpoints,omitempty"`
}

type MemoryRetrievalConfig struct {
	Enabled             bool    `json:"enabled"`
	Engine              string  `json:"engine,omitempty"`
	MaxWorkspaceResults int     `json:"max_workspace_results,omitempty"`
	MaxUserResults      int     `json:"max_user_results,omitempty"`
	MaxTotalChars       int     `json:"max_total_chars,omitempty"`
	PinnedCharBudget    int     `json:"pinned_char_budget,omitempty"`
	MinimumScore        float64 `json:"minimum_relevance_score,omitempty"`
	RecencyWeight       float64 `json:"recency_weight,omitempty"`
	RecencyHalfLifeDays int     `json:"recency_half_life_days,omitempty"`
	FuzzyWeight         float64 `json:"fuzzy_weight,omitempty"`
	RecentFallbackCount int     `json:"recent_fallback_count,omitempty"`
}

type MemoryLifecycleConfig struct {
	ArchivedRetentionDays int  `json:"archived_retention_days,omitempty"`
	StaleThresholdDays    int  `json:"stale_threshold_days,omitempty"`
	AutoArchiveExpired    bool `json:"auto_archive_expired,omitempty"`
}

// MemoryReviewConfig controls the bounded best-effort reviewer that may run
// after successful delivered turns. Each review consumes additional provider
// tokens/API calls even though it never blocks delivery of the main response.
type MemoryReviewConfig struct {
	Enabled        bool   `json:"enabled"                   env:"PICOCLAW_MEMORY_BACKGROUND_REVIEW_ENABLED"`
	Interval       int    `json:"interval,omitempty"        env:"PICOCLAW_MEMORY_BACKGROUND_REVIEW_INTERVAL"`
	Provider       string `json:"provider,omitempty"        env:"PICOCLAW_MEMORY_BACKGROUND_REVIEW_PROVIDER"`
	Model          string `json:"model,omitempty"           env:"PICOCLAW_MEMORY_BACKGROUND_REVIEW_MODEL"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" env:"PICOCLAW_MEMORY_BACKGROUND_REVIEW_TIMEOUT_SECONDS"`
	MaxIterations  int    `json:"max_iterations,omitempty"  env:"PICOCLAW_MEMORY_BACKGROUND_REVIEW_MAX_ITERATIONS"`
}

// MemoryRecallConfig controls backend-enforced cross-session lexical search.
type MemoryRecallConfig struct {
	Mode       string `json:"mode,omitempty"        env:"PICOCLAW_MEMORY_RECALL_MODE"`
	MaxResults int    `json:"max_results,omitempty" env:"PICOCLAW_MEMORY_RECALL_MAX_RESULTS"`
	MaxChars   int    `json:"max_chars,omitempty"   env:"PICOCLAW_MEMORY_RECALL_MAX_CHARS"`
	MaxRecords int    `json:"max_records,omitempty" env:"PICOCLAW_MEMORY_RECALL_MAX_RECORDS"`
}

// MemoryCheckpointConfig controls persistent task state. Checkpoints are
// session/topic state, not personal semantic memory.
type MemoryCheckpointConfig struct {
	Enabled         bool `json:"enabled"                     env:"PICOCLAW_MEMORY_CHECKPOINTS_ENABLED"`
	MaxCount        int  `json:"max_count,omitempty"         env:"PICOCLAW_MEMORY_CHECKPOINTS_MAX_COUNT"`
	MaxContextChars int  `json:"max_context_chars,omitempty" env:"PICOCLAW_MEMORY_CHECKPOINTS_MAX_CONTEXT_CHARS"`

	CompletedRetentionDays int `json:"completed_retention_days,omitempty" env:"PICOCLAW_MEMORY_CHECKPOINTS_COMPLETED_RETENTION_DAYS"`
}

func (c MemoryConfig) EffectiveWorkspaceCharLimit() int {
	if c.WorkspaceCharLimit > 0 {
		return c.WorkspaceCharLimit
	}
	return DefaultWorkspaceMemoryCharLimit
}

func (c MemoryConfig) EffectivePerUserCharLimit() int {
	if c.PerUserCharLimit > 0 {
		return c.PerUserCharLimit
	}
	return DefaultPerUserMemoryCharLimit
}

func (c MemoryConfig) EffectiveNotificationMode() string {
	switch strings.ToLower(strings.TrimSpace(c.Notifications)) {
	case MemoryNotificationOn:
		return MemoryNotificationOn
	case MemoryNotificationVerbose:
		return MemoryNotificationVerbose
	default:
		return MemoryNotificationOff
	}
}

func (c MemoryConfig) EffectiveApprovalMode() string {
	switch strings.ToLower(strings.TrimSpace(c.ApprovalMode)) {
	case MemoryApprovalOff:
		return MemoryApprovalOff
	case MemoryApprovalBackgroundOnly:
		return MemoryApprovalBackgroundOnly
	case MemoryApprovalAllWrites:
		return MemoryApprovalAllWrites
	default:
		if c.WriteApproval {
			return MemoryApprovalBackgroundOnly
		}
		return MemoryApprovalOff
	}
}

func (c MemoryConfig) ShouldStageMemoryWrite(background bool) bool {
	switch c.EffectiveApprovalMode() {
	case MemoryApprovalAllWrites:
		return true
	case MemoryApprovalBackgroundOnly:
		return background
	default:
		return false
	}
}

func (c MemoryRetrievalConfig) EffectiveEngine() string {
	if strings.EqualFold(strings.TrimSpace(c.Engine), MemoryRetrievalHybridLexical) {
		return MemoryRetrievalHybridLexical
	}
	return MemoryRetrievalHybridLexical
}

func (c MemoryRetrievalConfig) EffectiveMaxWorkspaceResults() int {
	return boundedPositive(c.MaxWorkspaceResults, DefaultMemoryWorkspaceResults, MaxMemoryRetrievalResults)
}

func (c MemoryRetrievalConfig) EffectiveMaxUserResults() int {
	return boundedPositive(c.MaxUserResults, DefaultMemoryUserResults, MaxMemoryRetrievalResults)
}

func (c MemoryRetrievalConfig) EffectiveMaxTotalChars() int {
	return boundedPositive(c.MaxTotalChars, DefaultMemoryRetrievalChars, MaxMemoryRetrievalChars)
}

func (c MemoryRetrievalConfig) EffectivePinnedCharBudget() int {
	return boundedPositive(c.PinnedCharBudget, DefaultMemoryPinnedChars, MaxMemoryPinnedChars)
}

func (c MemoryRetrievalConfig) EffectiveMinimumScore() float64 {
	if c.MinimumScore < 0 {
		return 0
	}
	if c.MinimumScore > 10 {
		return 10
	}
	return c.MinimumScore
}

func (c MemoryRetrievalConfig) EffectiveRecencyWeight() float64 {
	if c.RecencyWeight < 0 {
		return 0
	}
	if c.RecencyWeight > 5 {
		return 5
	}
	return c.RecencyWeight
}

func (c MemoryRetrievalConfig) EffectiveRecencyHalfLifeDays() int {
	return boundedPositive(c.RecencyHalfLifeDays, DefaultMemoryRecencyHalfLife, MaxMemoryLifecycleDays)
}

func (c MemoryRetrievalConfig) EffectiveFuzzyWeight() float64 {
	if c.FuzzyWeight < 0 {
		return 0
	}
	if c.FuzzyWeight > 5 {
		return 5
	}
	return c.FuzzyWeight
}

func (c MemoryRetrievalConfig) EffectiveRecentFallbackCount() int {
	if c.RecentFallbackCount < 0 {
		return 0
	}
	if c.RecentFallbackCount > MaxMemoryRetrievalResults {
		return MaxMemoryRetrievalResults
	}
	return c.RecentFallbackCount
}

func (c MemoryLifecycleConfig) EffectiveArchivedRetentionDays() int {
	return boundedPositive(c.ArchivedRetentionDays, DefaultMemoryArchivedRetention, MaxMemoryLifecycleDays)
}

func (c MemoryLifecycleConfig) EffectiveStaleThresholdDays() int {
	return boundedPositive(c.StaleThresholdDays, DefaultMemoryStaleThreshold, MaxMemoryLifecycleDays)
}

func boundedPositive(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func (c MemoryReviewConfig) EffectiveInterval() int {
	if c.Interval > 0 {
		return c.Interval
	}
	return DefaultMemoryReviewInterval
}

func (c MemoryReviewConfig) EffectiveTimeoutSeconds() int {
	if c.TimeoutSeconds > 0 {
		return c.TimeoutSeconds
	}
	return DefaultMemoryReviewTimeout
}

func (c MemoryReviewConfig) EffectiveMaxIterations() int {
	if c.MaxIterations <= 0 {
		return DefaultMemoryReviewIterations
	}
	if c.MaxIterations > MaxMemoryReviewIterations {
		return MaxMemoryReviewIterations
	}
	return c.MaxIterations
}

func (c MemoryRecallConfig) EffectiveMode() string {
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case MemoryRecallUserRecall:
		return MemoryRecallUserRecall
	case MemoryRecallGroupRecall:
		return MemoryRecallGroupRecall
	default:
		return MemoryRecallIsolated
	}
}

func (c MemoryRecallConfig) EffectiveMaxResults() int {
	if c.MaxResults <= 0 {
		return DefaultMemoryRecallResults
	}
	if c.MaxResults > MaxMemoryRecallResults {
		return MaxMemoryRecallResults
	}
	return c.MaxResults
}

func (c MemoryRecallConfig) EffectiveMaxChars() int {
	if c.MaxChars <= 0 {
		return DefaultMemoryRecallChars
	}
	if c.MaxChars > MaxMemoryRecallChars {
		return MaxMemoryRecallChars
	}
	return c.MaxChars
}

func (c MemoryRecallConfig) EffectiveMaxRecords() int {
	if c.MaxRecords <= 0 {
		return DefaultMemoryRecallRecords
	}
	if c.MaxRecords > MaxMemoryRecallRecords {
		return MaxMemoryRecallRecords
	}
	return c.MaxRecords
}

func (c MemoryCheckpointConfig) EffectiveMaxCount() int {
	if c.MaxCount <= 0 {
		return DefaultCheckpointCount
	}
	if c.MaxCount > MaxCheckpointCount {
		return MaxCheckpointCount
	}
	return c.MaxCount
}

func (c MemoryCheckpointConfig) EffectiveMaxContextChars() int {
	if c.MaxContextChars <= 0 {
		return DefaultCheckpointContextChars
	}
	if c.MaxContextChars > MaxCheckpointContextChars {
		return MaxCheckpointContextChars
	}
	return c.MaxContextChars
}

func (c MemoryCheckpointConfig) EffectiveCompletedRetentionDays() int {
	if c.CompletedRetentionDays <= 0 {
		return DefaultCheckpointRetentionDays
	}
	return c.CompletedRetentionDays
}

// Validate rejects values that the dashboard cannot safely represent. Config
// loading overlays legacy files on DefaultConfig first, so omitted fields have
// already received their backward-compatible active defaults before this runs.
func (c MemoryConfig) Validate() error {
	var validationErrors []string
	positive := func(name string, value int) {
		if value < 1 {
			validationErrors = append(validationErrors, fmt.Sprintf("%s must be >= 1", name))
		}
	}
	bounded := func(name string, value, maximum int) {
		positive(name, value)
		if value > maximum {
			validationErrors = append(
				validationErrors,
				fmt.Sprintf("%s must be <= %d", name, maximum),
			)
		}
	}

	positive("memory.workspace_char_limit", c.WorkspaceCharLimit)
	positive("memory.per_user_char_limit", c.PerUserCharLimit)
	switch strings.ToLower(strings.TrimSpace(c.Notifications)) {
	case MemoryNotificationOff, MemoryNotificationOn, MemoryNotificationVerbose:
	default:
		validationErrors = append(validationErrors, "memory.notifications must be off, on, or verbose")
	}
	if strings.TrimSpace(c.ApprovalMode) != "" {
		switch strings.ToLower(strings.TrimSpace(c.ApprovalMode)) {
		case MemoryApprovalOff, MemoryApprovalBackgroundOnly, MemoryApprovalAllWrites:
		default:
			validationErrors = append(
				validationErrors,
				"memory.approval_mode must be off, background_only, or all_writes",
			)
		}
	}
	positive("memory.background_review.interval", c.BackgroundReview.Interval)
	positive("memory.background_review.timeout_seconds", c.BackgroundReview.TimeoutSeconds)
	bounded(
		"memory.background_review.max_iterations",
		c.BackgroundReview.MaxIterations,
		MaxMemoryReviewIterations,
	)
	switch strings.ToLower(strings.TrimSpace(c.Recall.Mode)) {
	case MemoryRecallIsolated, MemoryRecallUserRecall, MemoryRecallGroupRecall:
	default:
		validationErrors = append(
			validationErrors,
			"memory.recall.mode must be isolated, user_recall, or group_recall",
		)
	}
	bounded("memory.recall.max_results", c.Recall.MaxResults, MaxMemoryRecallResults)
	bounded("memory.recall.max_chars", c.Recall.MaxChars, MaxMemoryRecallChars)
	bounded("memory.recall.max_records", c.Recall.MaxRecords, MaxMemoryRecallRecords)
	bounded("memory.checkpoints.max_count", c.Checkpoints.MaxCount, MaxCheckpointCount)
	bounded(
		"memory.checkpoints.max_context_chars",
		c.Checkpoints.MaxContextChars,
		MaxCheckpointContextChars,
	)
	positive(
		"memory.checkpoints.completed_retention_days",
		c.Checkpoints.CompletedRetentionDays,
	)
	if !strings.EqualFold(strings.TrimSpace(c.Retrieval.Engine), MemoryRetrievalHybridLexical) {
		validationErrors = append(validationErrors, "memory.retrieval.engine must be hybrid_lexical")
	}
	bounded("memory.retrieval.max_workspace_results", c.Retrieval.MaxWorkspaceResults, MaxMemoryRetrievalResults)
	bounded("memory.retrieval.max_user_results", c.Retrieval.MaxUserResults, MaxMemoryRetrievalResults)
	bounded("memory.retrieval.max_total_chars", c.Retrieval.MaxTotalChars, MaxMemoryRetrievalChars)
	bounded("memory.retrieval.pinned_char_budget", c.Retrieval.PinnedCharBudget, MaxMemoryPinnedChars)
	if c.Retrieval.MinimumScore < 0 || c.Retrieval.MinimumScore > 10 {
		validationErrors = append(validationErrors, "memory.retrieval.minimum_relevance_score must be between 0 and 10")
	}
	if c.Retrieval.RecencyWeight < 0 || c.Retrieval.RecencyWeight > 5 {
		validationErrors = append(validationErrors, "memory.retrieval.recency_weight must be between 0 and 5")
	}
	bounded("memory.retrieval.recency_half_life_days", c.Retrieval.RecencyHalfLifeDays, MaxMemoryLifecycleDays)
	if c.Retrieval.FuzzyWeight < 0 || c.Retrieval.FuzzyWeight > 5 {
		validationErrors = append(validationErrors, "memory.retrieval.fuzzy_weight must be between 0 and 5")
	}
	if c.Retrieval.RecentFallbackCount < 0 || c.Retrieval.RecentFallbackCount > MaxMemoryRetrievalResults {
		validationErrors = append(
			validationErrors,
			fmt.Sprintf(
				"memory.retrieval.recent_fallback_count must be between 0 and %d",
				MaxMemoryRetrievalResults,
			),
		)
	}
	bounded("memory.lifecycle.archived_retention_days", c.Lifecycle.ArchivedRetentionDays, MaxMemoryLifecycleDays)
	bounded("memory.lifecycle.stale_threshold_days", c.Lifecycle.StaleThresholdDays, MaxMemoryLifecycleDays)

	if len(validationErrors) == 0 {
		return nil
	}
	return errors.New(strings.Join(validationErrors, "; "))
}
