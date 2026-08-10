package tools

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/memory"
)

const SessionRecallToolName = "session_recall"

type SessionRecallTool struct {
	store      *memory.RecallStore
	mode       string
	maxResults int
	maxChars   int
}

func NewSessionRecallTool(store *memory.RecallStore, mode string, maxResults, maxChars int) *SessionRecallTool {
	return &SessionRecallTool{store: store, mode: mode, maxResults: maxResults, maxChars: maxChars}
}

func (t *SessionRecallTool) Name() string { return SessionRecallToolName }

func (t *SessionRecallTool) Description() string {
	return "Search bounded excerpts from other sessions/topics only when the user explicitly refers to earlier work or another topic. Backend policy enforces isolated, same-canonical-user, or same-group scope; no session key or user ID can be requested."
}

func (t *SessionRecallTool) PromptMetadata() PromptMetadata {
	return PromptMetadata{Layer: ToolPromptLayerCapability, Slot: ToolPromptSlotTooling, Source: ToolPromptSourceRegistry}
}

func (t *SessionRecallTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Terms describing the prior discussion, topic, task, or error. Session/user identifiers are intentionally unsupported.",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SessionRecallTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	if t == nil || t.store == nil || IsBackgroundMemoryReview(ctx) {
		return ErrorResult("{\"ok\":false,\"error\":{\"code\":\"recall_unavailable\"}}")
	}
	caller, ok := ToolCallerScope(ctx)
	if !ok || strings.TrimSpace(caller.SessionRef) == "" {
		return ErrorResult("{\"ok\":false,\"error\":{\"code\":\"scope_unavailable\"}}")
	}
	query := stringArg(args, "query")
	if query == "" || utf8.RuneCountInString(query) > 500 {
		return ErrorResult("{\"ok\":false,\"error\":{\"code\":\"invalid_query\"}}")
	}
	results, err := t.store.Search(caller, query, memory.RecallSearchOptions{
		Mode: t.mode, MaxResults: t.maxResults, MaxChars: t.maxChars,
	})
	if err != nil {
		return ErrorResult("{\"ok\":false,\"error\":{\"code\":\"recall_error\"}}").WithError(err)
	}
	data, err := json.Marshal(map[string]any{"ok": true, "mode": t.mode, "results": results})
	if err != nil {
		return ErrorResult("{\"ok\":false,\"error\":{\"code\":\"recall_error\"}}").WithError(err)
	}
	return SilentResult(string(data))
}

func (t *SessionRecallTool) ArgumentsForLog(args map[string]any) map[string]any {
	return map[string]any{"query_chars": utf8.RuneCountInString(stringArg(args, "query"))}
}
