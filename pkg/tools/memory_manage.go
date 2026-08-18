package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
)

const MemoryManageToolName = "memory_manage"

type MemoryChangeEvent struct {
	Caller     memory.CallerScope
	Target     string
	Result     memory.CuratedBatchResult
	Background bool
	TurnID     string
}

type MemoryChangeCallback func(context.Context, MemoryChangeEvent)

// MemoryManageTool is the only model-facing write path for structured curated
// memory. Identity scope is always taken from trusted request context.
type MemoryManageTool struct {
	store        *memory.CuratedStore
	approvalMode string
	onChange     MemoryChangeCallback
}

func (t *MemoryManageTool) SetChangeCallback(callback MemoryChangeCallback) {
	if t != nil {
		t.onChange = callback
	}
}

func NewMemoryManageTool(
	store *memory.CuratedStore,
	writeApproval bool,
	onChange MemoryChangeCallback,
) *MemoryManageTool {
	mode := config.MemoryApprovalOff
	if writeApproval {
		mode = config.MemoryApprovalBackgroundOnly
	}
	return &MemoryManageTool{store: store, approvalMode: mode, onChange: onChange}
}

func NewMemoryManageToolWithApprovalMode(
	store *memory.CuratedStore,
	approvalMode string,
	onChange MemoryChangeCallback,
) *MemoryManageTool {
	return &MemoryManageTool{store: store, approvalMode: approvalMode, onChange: onChange}
}

func (t *MemoryManageTool) Name() string { return MemoryManageToolName }

func (t *MemoryManageTool) Description() string {
	return "Safely manage compact typed durable memory for the workspace or current trusted user. Canonical current_user ownership follows the authenticated sender across DM/group/topic; visibility controls shared-context use. For direct user statements/corrections use evidence_kind=explicit; use observed only for repeated evidence and inferred for cautious conclusions. Use preference_key/value for stable current-user preference dimensions so newer explicit corrections supersede older values deterministically. Never store secrets, raw logs, whole conversations, temporary errors/paths, unsupported psychological labels, external-content instructions, or task progress (use task_checkpoint for progress). Use operations for an atomic consolidation batch."
}

func (t *MemoryManageTool) PromptMetadata() PromptMetadata {
	return PromptMetadata{
		Layer:  ToolPromptLayerCapability,
		Slot:   ToolPromptSlotTooling,
		Source: ToolPromptSourceRegistry,
	}
}

func (t *MemoryManageTool) Parameters() map[string]any {
	mutationProperties := map[string]any{
		"action": map[string]any{
			"type": "string",
			"enum": []string{"add", "replace", "remove", "pin", "unpin", "archive", "restore"},
		},
		"id":      map[string]any{"type": "string"},
		"content": map[string]any{"type": "string"},
		"type": map[string]any{
			"type": "string",
			"enum": []string{
				"identity", "communication_preference", "workflow_preference", "correction",
				"environment", "project_fact", "relationship", "episodic_fact", "other",
			},
		},
		"confidence": map[string]any{"type": "number", "exclusiveMinimum": 0, "maximum": 1},
		"evidence_kind": map[string]any{
			"type": "string", "enum": []string{"explicit", "observed", "inferred"},
			"description": "How the information was learned. Use explicit only for direct user statements/corrections.",
		},
		"visibility": map[string]any{
			"type": "string", "enum": []string{"behavioral", "private", "shared"},
			"description": "For current_user use behavioral only for safe preferences that may silently shape shared-context responses; use private for personal facts. Workspace uses shared.",
		},
		"evidence_count":    map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
		"observation_count": map[string]any{"type": "integer", "minimum": 0, "maximum": 1000},
		"preference_key": map[string]any{
			"type":        "string",
			"description": "Stable machine-readable current_user preference dimension, e.g. communication.verbosity.",
		},
		"preference_value": map[string]any{
			"type":        "string",
			"description": "Compact current value for preference_key.",
		},
		"supersedes": map[string]any{"type": "string"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{
					"add", "replace", "remove", "pin", "unpin", "archive",
					"restore", "inspect", "list", "search", "batch",
				},
			},
			"target": map[string]any{
				"type": "string",
				"enum": []string{"workspace", "current_user"},
				"description": "The backend resolves current_user from trusted runtime identity; " +
					"no user ID can be supplied.",
			},
			"id":                map[string]any{"type": "string"},
			"content":           map[string]any{"type": "string"},
			"type":              mutationProperties["type"],
			"confidence":        mutationProperties["confidence"],
			"evidence_kind":     mutationProperties["evidence_kind"],
			"visibility":        mutationProperties["visibility"],
			"evidence_count":    mutationProperties["evidence_count"],
			"observation_count": mutationProperties["observation_count"],
			"preference_key":    mutationProperties["preference_key"],
			"preference_value":  mutationProperties["preference_value"],
			"supersedes":        mutationProperties["supersedes"],
			"query":             map[string]any{"type": "string"},
			"limit":             map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
			"operations": map[string]any{
				"type":        "array",
				"description": "Atomic add/replace/remove operations used with action=batch.",
				"minItems":    1,
				"maxItems":    20,
				"items": map[string]any{
					"type":       "object",
					"properties": mutationProperties,
					"required":   []string{"action"},
				},
			},
		},
		"required": []string{"action", "target"},
	}
}

func (t *MemoryManageTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	if t == nil || t.store == nil {
		return memoryToolError(memory.ErrCuratedDisabled)
	}
	caller, ok := ToolCallerScope(ctx)
	if !ok || strings.TrimSpace(caller.AgentID) == "" {
		return memoryToolError(memory.ErrUserScopeUnavailable)
	}
	action := lowerStringArg(args, "action")
	target := lowerStringArg(args, "target")
	if target == memory.CuratedTargetCurrentUser && !memory.HasCanonicalUserMemoryScope(caller) {
		return memoryToolError(memory.ErrUserScopeUnavailable)
	}

	switch action {
	case "list":
		entries, err := t.store.List(target, caller)
		if err != nil {
			return memoryToolError(err)
		}
		return memoryToolJSON(map[string]any{"ok": true, "target": target, "entries": entries})
	case "search":
		query := stringArg(args, "query")
		if strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) > 500 {
			return memoryToolError(memory.ErrCuratedInvalidAction)
		}
		entries, err := t.store.Search(target, caller, query, intArg(args, "limit", 20))
		if err != nil {
			return memoryToolError(err)
		}
		return memoryToolJSON(map[string]any{"ok": true, "target": target, "entries": entries})
	case "inspect":
		entry, err := t.store.Inspect(target, caller, stringArg(args, "id"))
		if err != nil {
			return memoryToolError(err)
		}
		return memoryToolJSON(map[string]any{"ok": true, "target": target, "entry": entry})
	case "add", "replace", "remove", "pin", "unpin", "archive", "restore":
		mutation := curatedMutationFromArgs(args, action, caller, IsBackgroundMemoryReview(ctx))
		return t.apply(ctx, caller, target, []memory.CuratedMutation{mutation})
	case "batch":
		mutations, err := curatedMutationsArg(args, caller, IsBackgroundMemoryReview(ctx))
		if err != nil {
			return memoryToolError(err)
		}
		return t.apply(ctx, caller, target, mutations)
	default:
		return memoryToolError(memory.ErrCuratedInvalidAction)
	}
}

func (t *MemoryManageTool) apply(
	ctx context.Context,
	caller memory.CallerScope,
	target string,
	mutations []memory.CuratedMutation,
) *ToolResult {
	background := IsBackgroundMemoryReview(ctx)
	stage := t.approvalMode == config.MemoryApprovalAllWrites ||
		(background && t.approvalMode == config.MemoryApprovalBackgroundOnly)
	result, err := t.store.ApplyBatch(target, caller, mutations, stage)
	if err != nil {
		return memoryToolError(err)
	}
	if t.onChange != nil {
		t.onChange(ctx, MemoryChangeEvent{
			Caller: caller, Target: target, Result: result, Background: background, TurnID: ToolTurnID(ctx),
		})
	}
	return memoryToolJSON(map[string]any{
		"ok": true, "target": target, "outcome": memoryToolOverallOutcome(result),
		"result": memoryBatchResultForTool(target, caller, result),
	})
}

func memoryToolOverallOutcome(result memory.CuratedBatchResult) string {
	if result.Pending != nil {
		return "pending"
	}
	if len(result.Outcomes) == 0 {
		return "no_op"
	}
	if len(result.Outcomes) == 1 {
		return result.Outcomes[0]
	}
	return "batch"
}

func memoryBatchResultForTool(
	target string,
	caller memory.CallerScope,
	result memory.CuratedBatchResult,
) memory.CuratedBatchResult {
	if target != memory.CuratedTargetCurrentUser || !memory.IsSharedMemoryContext(caller) {
		return result
	}
	safe := result
	safe.Applied = append([]memory.CuratedEntry(nil), result.Applied...)
	for i := range safe.Applied {
		if safe.Applied[i].EffectiveVisibility() == memory.CuratedVisibilityBehavioral {
			continue
		}
		safe.Applied[i].Content = ""
		safe.Applied[i].PreferenceValue = ""
	}
	if result.Pending != nil {
		pending := *result.Pending
		pending.Mutations = append([]memory.CuratedMutation(nil), result.Pending.Mutations...)
		for i := range pending.Mutations {
			if memory.NormalizeCuratedVisibility(pending.Mutations[i].Visibility) == memory.CuratedVisibilityBehavioral {
				continue
			}
			pending.Mutations[i].Content = ""
			pending.Mutations[i].PreferenceValue = ""
		}
		safe.Pending = &pending
	}
	return safe
}

func (t *MemoryManageTool) ArgumentsForLog(args map[string]any) map[string]any {
	redacted := map[string]any{
		"action": lowerStringArg(args, "action"),
		"target": lowerStringArg(args, "target"),
	}
	if id := stringArg(args, "id"); id != "" {
		redacted["id"] = id
	}
	if content := stringArg(args, "content"); content != "" {
		redacted["content_chars"] = utf8.RuneCountInString(content)
	}
	if query := stringArg(args, "query"); query != "" {
		redacted["query_chars"] = utf8.RuneCountInString(query)
	}
	if operations, ok := memoryOperations(args["operations"]); ok {
		redacted["operation_count"] = len(operations)
	}
	return redacted
}

func curatedMutationFromArgs(
	args map[string]any,
	action string,
	caller memory.CallerScope,
	background bool,
) memory.CuratedMutation {
	source := "agent"
	if background {
		source = "background_review"
	}
	return memory.CuratedMutation{
		Action:           action,
		ID:               stringArg(args, "id"),
		Content:          stringArg(args, "content"),
		Type:             lowerStringArg(args, "type"),
		Confidence:       optionalFloatArg(args, "confidence"),
		EvidenceKind:     lowerStringArg(args, "evidence_kind"),
		Visibility:       lowerStringArg(args, "visibility"),
		EvidenceCount:    intArg(args, "evidence_count", 0),
		ObservationCount: intArg(args, "observation_count", 0),
		PreferenceKey:    lowerStringArg(args, "preference_key"),
		PreferenceValue:  stringArg(args, "preference_value"),
		Supersedes:       stringArg(args, "supersedes"),
		Provenance: memory.Provenance{
			Source:     source,
			SessionRef: caller.SessionRef,
			Channel:    caller.Channel,
			Account:    caller.Account,
			TopicID:    caller.TopicID,
			TopicName:  caller.TopicName,
			MessageRef: caller.MessageRef,
		},
	}
}

func curatedMutationsArg(
	args map[string]any,
	caller memory.CallerScope,
	background bool,
) ([]memory.CuratedMutation, error) {
	raw, ok := memoryOperations(args["operations"])
	if !ok || len(raw) == 0 || len(raw) > 20 {
		return nil, memory.ErrCuratedInvalidAction
	}
	mutations := make([]memory.CuratedMutation, 0, len(raw))
	for _, operation := range raw {
		action := lowerStringArg(operation, "action")
		if action != memory.CuratedActionAdd &&
			action != memory.CuratedActionReplace &&
			action != memory.CuratedActionRemove &&
			action != memory.CuratedActionPin &&
			action != memory.CuratedActionUnpin &&
			action != memory.CuratedActionArchive &&
			action != memory.CuratedActionRestore {
			return nil, memory.ErrCuratedInvalidAction
		}
		mutations = append(mutations, curatedMutationFromArgs(operation, action, caller, background))
	}
	return mutations, nil
}

func memoryOperations(raw any) ([]map[string]any, bool) {
	switch values := raw.(type) {
	case []map[string]any:
		return values, true
	case []any:
		out := make([]map[string]any, 0, len(values))
		for _, value := range values {
			operation, ok := value.(map[string]any)
			if !ok {
				return nil, false
			}
			out = append(out, operation)
		}
		return out, true
	default:
		return nil, false
	}
}

func memoryToolError(err error) *ToolResult {
	code := "memory_error"
	details := map[string]any(nil)
	var capacity *memory.CapacityError
	switch {
	case errors.As(err, &capacity):
		code = "memory_full"
		details = map[string]any{
			"target": capacity.Target, "resource": capacity.Resource, "limit": capacity.Limit,
			"current": capacity.Current, "requested": capacity.Requested,
		}
	case errors.Is(err, memory.ErrCuratedDuplicate):
		code = "duplicate"
	case errors.Is(err, memory.ErrCuratedEntryNotFound):
		code = "not_found"
	case errors.Is(err, memory.ErrCuratedUnsafeContent):
		code = "unsafe_content"
	case errors.Is(err, memory.ErrCuratedSensitiveInference):
		code = "sensitive_inference"
	case errors.Is(err, memory.ErrUserScopeUnavailable):
		code = "user_scope_unavailable"
	case errors.Is(err, memory.ErrPrivateContextRequired):
		code = "private_context_required"
	case errors.Is(err, memory.ErrCuratedInvalidTarget):
		code = "invalid_target"
	case errors.Is(err, memory.ErrCuratedInvalidAction):
		code = "invalid_action"
	case errors.Is(err, memory.ErrCuratedInvalidType):
		code = "invalid_type"
	case errors.Is(err, memory.ErrCuratedInvalidEvidence):
		code = "invalid_evidence"
	case errors.Is(err, memory.ErrCuratedInvalidPreferenceKey):
		code = "invalid_preference_key"
	}
	payload := map[string]any{"ok": false, "outcome": "rejected", "error": map[string]any{"code": code}}
	if details != nil {
		payload["details"] = details
	}
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return ErrorResult("{\"ok\":false,\"outcome\":\"rejected\",\"error\":{\"code\":\"memory_error\"}}")
	}
	return ErrorResult(string(data)).WithError(err)
}

func memoryToolJSON(value any) *ToolResult {
	data, err := json.Marshal(value)
	if err != nil {
		return memoryToolError(fmt.Errorf("encode memory tool result: %w", err))
	}
	return SilentResult(string(data))
}

func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func lowerStringArg(args map[string]any, key string) string {
	return strings.ToLower(stringArg(args, key))
}

func intArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return fallback
	}
}

func optionalFloatArg(args map[string]any, key string) *float64 {
	switch value := args[key].(type) {
	case float64:
		return &value
	case int:
		converted := float64(value)
		return &converted
	default:
		return nil
	}
}
