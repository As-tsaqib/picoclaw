package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/memory"
)

func initializeAgentMemoryStores(
	workspace string,
	agentID string,
	cfg config.MemoryConfig,
) (*memory.CuratedStore, *memory.RecallStore, *memory.CheckpointStore, *memory.ReviewStateStore) {
	needsCurated := cfg.Enabled
	needsRecall := cfg.Enabled || cfg.Recall.EffectiveMode() != config.MemoryRecallIsolated ||
		(cfg.Enabled && cfg.BackgroundReview.Enabled)
	needsCheckpoints := cfg.Checkpoints.Enabled
	needsReviewState := cfg.Enabled
	if !needsCurated && !needsRecall && !needsCheckpoints && !needsReviewState {
		return nil, nil, nil, nil
	}

	root := structuredMemoryRoot(workspace, agentID)
	var curated *memory.CuratedStore
	var recall *memory.RecallStore
	var checkpoints *memory.CheckpointStore
	var reviewState *memory.ReviewStateStore
	var err error

	if needsCurated {
		curated, err = memory.NewCuratedStore(filepath.Join(root, "curated"), memory.CuratedStoreOptions{
			WorkspaceCharLimit: cfg.EffectiveWorkspaceCharLimit(),
			PerUserCharLimit:   cfg.EffectivePerUserCharLimit(),
		})
		if err != nil {
			logger.WarnCF("memory", "Structured curated memory is unavailable", safeMemoryLogFields(err))
			curated = nil
		}
	}
	if needsRecall {
		recall, err = memory.NewRecallStore(filepath.Join(root, "recall"), cfg.Recall.EffectiveMaxRecords())
		if err != nil {
			logger.WarnCF("memory", "Cross-session recall index is unavailable", safeMemoryLogFields(err))
			recall = nil
		}
	}
	if needsCheckpoints {
		checkpoints, err = memory.NewCheckpointStore(filepath.Join(root, "checkpoints"), memory.CheckpointStoreOptions{
			MaxCount:           cfg.Checkpoints.EffectiveMaxCount(),
			MaxContextChars:    cfg.Checkpoints.EffectiveMaxContextChars(),
			CompletedRetention: time.Duration(cfg.Checkpoints.EffectiveCompletedRetentionDays()) * 24 * time.Hour,
		})
		if err != nil {
			logger.WarnCF("memory", "Task checkpoints are unavailable", safeMemoryLogFields(err))
			checkpoints = nil
		}
	}
	if needsReviewState && curated != nil && recall != nil {
		reviewState, err = memory.NewReviewStateStore(filepath.Join(root, "review"))
		if err != nil {
			logger.WarnCF("memory", "Background memory review state is unavailable", safeMemoryLogFields(err))
			reviewState = nil
		}
	}
	return curated, recall, checkpoints, reviewState
}

// safeMemoryLogFields deliberately excludes error text because provider and
// storage errors may contain transcript fragments, memory contents, or other
// sensitive values. The concrete error type is sufficient for diagnostics
// without copying private data into logs.
func safeMemoryLogFields(err error) map[string]any {
	if err == nil {
		return nil
	}
	return map[string]any{"error_type": fmt.Sprintf("%T", err)}
}

// structuredMemoryRoot separates structured state from legacy MEMORY.md and
// hashes the agent ID so two agents sharing a workspace cannot read each
// other's personal memory, recall index, or checkpoints.
func structuredMemoryRoot(workspace, agentID string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(agentID))))
	name := "agent_" + hex.EncodeToString(digest[:8])
	return filepath.Join(workspace, "memory", "structured", name)
}

// StructuredMemoryRoot returns the backend-owned structured state root for a
// trusted workspace and agent ID. Dashboard code uses this only with values
// loaded from the authenticated local configuration, never request input.
func StructuredMemoryRoot(workspace, agentID string) string {
	return structuredMemoryRoot(workspace, agentID)
}
