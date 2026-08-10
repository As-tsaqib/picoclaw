package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/commands"
	"github.com/sipeed/picoclaw/pkg/memory"
)

func configureMemoryCommandRuntime(
	rt *commands.Runtime,
	agent *AgentInstance,
	opts *processOptions,
	al *AgentLoop,
) {
	if rt == nil || agent == nil || opts == nil || al == nil {
		return
	}
	caller := callerScopeForTurn(agent.ID, rt.Config, *opts)
	if agent.CuratedMemory != nil {
		rt.MemoryStatus = func() string {
			workspace, workspaceErr := agent.CuratedMemory.Stats(memory.CuratedTargetWorkspace, caller)
			lines := []string{
				"Curated memory: enabled",
				fmt.Sprintf("Recall mode: %s", rt.Config.Memory.Recall.EffectiveMode()),
				fmt.Sprintf(
					"Background review: %t (interval %d)",
					rt.Config.Memory.BackgroundReview.Enabled,
					rt.Config.Memory.BackgroundReview.EffectiveInterval(),
				),
				fmt.Sprintf("Write approval: %t", rt.Config.Memory.WriteApproval),
				fmt.Sprintf("Notifications: %s", rt.Config.Memory.EffectiveNotificationMode()),
			}
			if workspaceErr == nil {
				lines = append(lines, formatMemoryStats(workspace))
			}
			if caller.GroupID != "" {
				lines = append(lines, "Current-user memory details are hidden in shared chats.")
			} else {
				user, userErr := agent.CuratedMemory.Stats(memory.CuratedTargetCurrentUser, caller)
				if userErr == nil {
					lines = append(lines, formatMemoryStats(user))
				} else if errors.Is(userErr, memory.ErrUserScopeUnavailable) {
					lines = append(lines, "Current-user scope: unavailable on this request")
				}
			}
			return strings.Join(lines, "\n")
		}
		rt.MemoryList = func() (string, error) {
			workspace, err := agent.CuratedMemory.List(memory.CuratedTargetWorkspace, caller)
			if err != nil {
				return "", err
			}
			user, userErr := agent.CuratedMemory.List(memory.CuratedTargetCurrentUser, caller)
			if userErr != nil && !errors.Is(userErr, memory.ErrUserScopeUnavailable) {
				return "", userErr
			}
			if caller.GroupID != "" {
				return formatMemoryEntries(workspace, nil) +
					"\nCurrent-user memory is hidden in shared chats; use a direct chat to list it.", nil
			}
			return formatMemoryEntries(workspace, user), nil
		}
		rt.MemoryForget = func(id string) (string, error) {
			target, err := findMemoryEntryTarget(agent.CuratedMemory, caller, id)
			if err != nil {
				return "", err
			}
			_, err = agent.CuratedMemory.ApplyBatch(target, caller, []memory.CuratedMutation{{
				Action: memory.CuratedActionRemove, ID: id,
				Provenance: memory.Provenance{Source: "user_command"},
			}}, false)
			if err != nil {
				return "", err
			}
			return "Forgot memory entry " + id + ".", nil
		}
		rt.MemoryPending = func() (string, error) {
			workspace, err := agent.CuratedMemory.Pending(memory.CuratedTargetWorkspace, caller)
			if err != nil {
				return "", err
			}
			if caller.GroupID != "" {
				return formatPendingMemory(workspace, nil) +
					"\nCurrent-user pending changes are hidden in shared chats; use a direct chat to manage them.", nil
			}
			user, userErr := agent.CuratedMemory.Pending(memory.CuratedTargetCurrentUser, caller)
			if userErr != nil && !errors.Is(userErr, memory.ErrUserScopeUnavailable) {
				return "", userErr
			}
			return formatPendingMemory(workspace, user), nil
		}
		rt.MemoryApprove = func(id string) (string, error) {
			count, err := resolvePendingMemory(
				agent.CuratedMemory,
				caller,
				id,
				true,
				caller.GroupID == "",
			)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Approved %d memory operation(s).", count), nil
		}
		rt.MemoryReject = func(id string) (string, error) {
			count, err := resolvePendingMemory(
				agent.CuratedMemory,
				caller,
				id,
				false,
				caller.GroupID == "",
			)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Rejected %d pending memory change(s).", count), nil
		}
		rt.MemoryReview = func(_ context.Context) (string, error) {
			started, err := al.startMemoryReview(agent, caller, true)
			if err != nil {
				return "", err
			}
			if !started {
				return "A memory review is already running, or there is nothing eligible to start.", nil
			}
			return "Started a bounded memory review in the background.", nil
		}
	}

	if agent.Checkpoints != nil {
		rt.CheckpointList = func() (string, error) {
			checkpoints, err := agent.Checkpoints.List(caller, false)
			if err != nil {
				return "", err
			}
			return formatCheckpointList(checkpoints), nil
		}
		rt.CheckpointResume = func(id string) (string, error) {
			checkpoint, err := agent.Checkpoints.Apply(caller, "", memory.CheckpointMutation{
				Action: memory.CheckpointActionResume, ID: id,
			})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(
				"Resumed %s (%s). Next: %s",
				checkpoint.Title,
				checkpoint.ID,
				checkpoint.NextStep,
			), nil
		}
		rt.CheckpointForget = func(id string) (string, error) {
			checkpoint, err := agent.Checkpoints.Apply(caller, "", memory.CheckpointMutation{
				Action: memory.CheckpointActionArchive, ID: id,
			})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Archived checkpoint %s (%s).", checkpoint.Title, checkpoint.ID), nil
		}
	}
}

func formatMemoryStats(stats memory.CuratedStats) string {
	return fmt.Sprintf(
		"%s: %d entries, %d/%d characters, %d pending",
		stats.Target,
		stats.Entries,
		stats.Characters,
		stats.Capacity,
		stats.PendingCount,
	)
}

func formatMemoryEntries(workspace, user []memory.CuratedEntry) string {
	var lines []string
	appendEntries := func(title string, entries []memory.CuratedEntry) {
		lines = append(lines, title)
		if len(entries) == 0 {
			lines = append(lines, "- (empty)")
			return
		}
		for _, entry := range entries {
			lines = append(
				lines,
				fmt.Sprintf("- `%s` — %s", entry.ID, memory.RedactMemoryText(entry.Content)),
			)
		}
	}
	appendEntries("Workspace memory:", workspace)
	appendEntries("Current-user memory:", user)
	return strings.Join(lines, "\n")
}

func findMemoryEntryTarget(store *memory.CuratedStore, caller memory.CallerScope, id string) (string, error) {
	if entries, err := store.List(memory.CuratedTargetCurrentUser, caller); err == nil {
		for _, entry := range entries {
			if entry.ID == id {
				return memory.CuratedTargetCurrentUser, nil
			}
		}
	}
	entries, err := store.List(memory.CuratedTargetWorkspace, caller)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.ID == id {
			return memory.CuratedTargetWorkspace, nil
		}
	}
	return "", memory.ErrCuratedEntryNotFound
}

func formatPendingMemory(workspace, user []memory.PendingCuratedChange) string {
	var lines []string
	appendPending := func(target string, changes []memory.PendingCuratedChange) {
		for _, change := range changes {
			lines = append(lines, fmt.Sprintf(
				"- `%s` (%s, %d operation(s), %s)",
				change.ID,
				target,
				len(change.Mutations),
				change.CreatedAt.UTC().Format("2006-01-02 15:04Z"),
			))
		}
	}
	appendPending("workspace", workspace)
	appendPending("current_user", user)
	if len(lines) == 0 {
		return "No pending memory changes."
	}
	return "Pending memory changes:\n" + strings.Join(lines, "\n")
}

func resolvePendingMemory(
	store *memory.CuratedStore,
	caller memory.CallerScope,
	id string,
	approve bool,
	includeCurrentUser bool,
) (int, error) {
	count := 0
	found := false
	targets := []string{memory.CuratedTargetWorkspace}
	if includeCurrentUser {
		targets = append([]string{memory.CuratedTargetCurrentUser}, targets...)
	}
	for _, target := range targets {
		pending, err := store.Pending(target, caller)
		if errors.Is(err, memory.ErrUserScopeUnavailable) {
			continue
		}
		if err != nil {
			return 0, err
		}
		matches := false
		for _, change := range pending {
			if id == "all" || change.ID == id {
				matches = true
				found = true
				count += len(change.Mutations)
			}
		}
		if !matches {
			continue
		}
		if approve {
			if _, err := store.Approve(target, caller, id); err != nil {
				return 0, err
			}
		} else if _, err := store.Reject(target, caller, id); err != nil {
			return 0, err
		}
	}
	if !found {
		return 0, memory.ErrCuratedInvalidPending
	}
	return count, nil
}

func formatCheckpointList(checkpoints []memory.TaskCheckpoint) string {
	if len(checkpoints) == 0 {
		return "No active or suspended checkpoints in this session."
	}
	lines := []string{"Task checkpoints:"}
	for _, checkpoint := range checkpoints {
		line := fmt.Sprintf("- `%s` [%s] %s", checkpoint.ID, checkpoint.Status, checkpoint.Title)
		if strings.TrimSpace(checkpoint.NextStep) != "" {
			line += " — next: " + checkpoint.NextStep
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
