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

	MaxMemoryReviewIterations = 4
	MaxMemoryRecallResults    = 20
	MaxMemoryRecallChars      = 20_000
	MaxMemoryRecallRecords    = 20_000
	MaxCheckpointCount        = 1_000
	MaxCheckpointContextChars = 20_000
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
	Notifications      string                 `json:"notifications,omitempty"        env:"PICOCLAW_MEMORY_NOTIFICATIONS"`
	BackgroundReview   MemoryReviewConfig     `json:"background_review,omitempty"`
	Recall             MemoryRecallConfig     `json:"recall,omitempty"`
	Checkpoints        MemoryCheckpointConfig `json:"checkpoints,omitempty"`
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

	if len(validationErrors) == 0 {
		return nil
	}
	return errors.New(strings.Join(validationErrors, "; "))
}
