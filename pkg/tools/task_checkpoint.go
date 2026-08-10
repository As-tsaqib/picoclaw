package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/memory"
)

const TaskCheckpointToolName = "task_checkpoint"

type TaskCheckpointTool struct {
	store *memory.CheckpointStore
}

func NewTaskCheckpointTool(store *memory.CheckpointStore) *TaskCheckpointTool {
	return &TaskCheckpointTool{store: store}
}

func (t *TaskCheckpointTool) Name() string { return TaskCheckpointToolName }

func (t *TaskCheckpointTool) Description() string {
	return "Create and maintain resumable lesson, debugging, coding, research, or setup task checkpoints for the " +
		"current session/topic. Progress mutations are staged until the final assistant response is successfully " +
		"delivered. Use resolve when the user asks to continue an earlier task in this topic."
}

func (t *TaskCheckpointTool) PromptMetadata() PromptMetadata {
	return PromptMetadata{
		Layer:  ToolPromptLayerCapability,
		Slot:   ToolPromptSlotTooling,
		Source: ToolPromptSourceRegistry,
	}
}

func (t *TaskCheckpointTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{
					"create", "update", "suspend", "resume", "complete",
					"list", "get", "resolve", "archive", "delete",
				},
			},
			"id":        map[string]any{"type": "string"},
			"kind":      map[string]any{"type": "string"},
			"title":     map[string]any{"type": "string"},
			"objective": map[string]any{"type": "string"},
			"completed_items": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 50,
			},
			"current_step":      map[string]any{"type": "string"},
			"next_step":         map[string]any{"type": "string"},
			"important_context": map[string]any{"type": "string"},
			"query":             map[string]any{"type": "string"},
			"include_completed": map[string]any{"type": "boolean"},
		},
		"required": []string{"action"},
	}
}

func (t *TaskCheckpointTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	if t == nil || t.store == nil || IsBackgroundMemoryReview(ctx) {
		return checkpointToolError(memory.ErrCheckpointInvalid)
	}
	caller, ok := ToolCallerScope(ctx)
	if !ok || strings.TrimSpace(caller.SessionKey) == "" {
		return checkpointToolError(memory.ErrCheckpointInvalid)
	}
	action := lowerStringArg(args, "action")
	turnID := ToolTurnID(ctx)
	switch action {
	case "list":
		checkpoints, err := t.store.ListForTurn(caller, turnID, boolArg(args, "include_completed"))
		if err != nil {
			return checkpointToolError(err)
		}
		return checkpointToolJSON(map[string]any{"ok": true, "checkpoints": checkpoints})
	case "get":
		checkpoint, err := t.store.GetForTurn(caller, turnID, stringArg(args, "id"))
		if err != nil {
			return checkpointToolError(err)
		}
		return checkpointToolJSON(map[string]any{"ok": true, "checkpoint": checkpoint})
	case "resolve":
		checkpoint, err := t.store.ResolveContinuationForTurn(caller, turnID, stringArg(args, "query"))
		if err != nil {
			return checkpointToolError(err)
		}
		return checkpointToolJSON(map[string]any{"ok": true, "checkpoint": checkpoint})
	case memory.CheckpointActionCreate,
		memory.CheckpointActionUpdate,
		memory.CheckpointActionSuspend,
		memory.CheckpointActionResume,
		memory.CheckpointActionComplete,
		memory.CheckpointActionArchive,
		memory.CheckpointActionDelete:
		checkpoint, err := t.store.Apply(caller, turnID, checkpointMutationFromArgs(args, action))
		if err != nil {
			return checkpointToolError(err)
		}
		return checkpointToolJSON(map[string]any{
			"ok": true, "checkpoint": checkpoint, "staged_until_delivery": turnID != "",
		})
	default:
		return checkpointToolError(memory.ErrCheckpointInvalid)
	}
}

func (t *TaskCheckpointTool) ArgumentsForLog(args map[string]any) map[string]any {
	out := map[string]any{"action": lowerStringArg(args, "action")}
	if id := stringArg(args, "id"); id != "" {
		out["id"] = id
	}
	keys := []string{
		"kind", "title", "objective", "current_step", "next_step", "important_context", "query",
	}
	for _, key := range keys {
		if value := stringArg(args, key); value != "" {
			out[key+"_chars"] = utf8.RuneCountInString(value)
		}
	}
	if items, ok := stringSliceArg(args["completed_items"]); ok {
		out["completed_item_count"] = len(items)
	}
	return out
}

func checkpointMutationFromArgs(args map[string]any, action string) memory.CheckpointMutation {
	return memory.CheckpointMutation{
		Action:           action,
		ID:               stringArg(args, "id"),
		Kind:             optionalStringArg(args, "kind"),
		Title:            optionalStringArg(args, "title"),
		Objective:        optionalStringArg(args, "objective"),
		CompletedItems:   optionalStringSliceArg(args, "completed_items"),
		CurrentStep:      optionalStringArg(args, "current_step"),
		NextStep:         optionalStringArg(args, "next_step"),
		ImportantContext: optionalStringArg(args, "important_context"),
		Query:            stringArg(args, "query"),
	}
}

func checkpointToolError(err error) *ToolResult {
	code := "checkpoint_error"
	payload := map[string]any{"ok": false}
	var ambiguous *memory.AmbiguousCheckpointError
	switch {
	case errors.As(err, &ambiguous):
		code = "ambiguous"
		payload["candidates"] = ambiguous.Candidates
	case errors.Is(err, memory.ErrCheckpointNotFound):
		code = "not_found"
	case errors.Is(err, memory.ErrCheckpointCapacity):
		code = "full"
	case errors.Is(err, memory.ErrCheckpointNotResumable):
		code = "not_resumable"
	case errors.Is(err, memory.ErrCuratedUnsafeContent):
		code = "unsafe_content"
	case errors.Is(err, memory.ErrCheckpointInvalid):
		code = "invalid_action"
	}
	payload["error"] = map[string]any{"code": code}
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return ErrorResult("{\"ok\":false,\"error\":{\"code\":\"checkpoint_error\"}}")
	}
	return ErrorResult(string(data)).WithError(err)
}

func checkpointToolJSON(value any) *ToolResult {
	data, err := json.Marshal(value)
	if err != nil {
		return checkpointToolError(err)
	}
	return SilentResult(string(data))
}

func optionalStringArg(args map[string]any, key string) *string {
	raw, exists := args[key]
	if !exists {
		return nil
	}
	value, ok := raw.(string)
	if !ok {
		return nil
	}
	value = strings.TrimSpace(value)
	return &value
}

func optionalStringSliceArg(args map[string]any, key string) *[]string {
	raw, exists := args[key]
	if !exists {
		return nil
	}
	values, ok := stringSliceArg(raw)
	if !ok {
		return nil
	}
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}
	return &values
}

func stringSliceArg(raw any) ([]string, bool) {
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...), true
	case []any:
		out := make([]string, 0, len(values))
		for _, rawValue := range values {
			value, ok := rawValue.(string)
			if !ok {
				return nil, false
			}
			out = append(out, value)
		}
		return out, true
	default:
		return nil, false
	}
}

func boolArg(args map[string]any, key string) bool {
	value, _ := args[key].(bool)
	return value
}
