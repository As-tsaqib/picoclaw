package commands

import (
	"context"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestMemoryAndCheckpointCommandsDispatchRuntimeControls(t *testing.T) {
	runtime := &Runtime{
		MemoryCommand: func(_ context.Context, req MemoryCommandRequest) (*bus.StructuredContent, error) {
			var text string
			switch req.Operation {
			case "status":
				text = "memory status"
			case "profile":
				text = "user profile"
			case "list":
				text = "memory list"
			case "search":
				text = "searched " + req.Query
			case "edit":
				text = "edited " + req.ID + " to " + req.Content
			case "pin":
				text = "pin " + req.ID
			case "unpin":
				text = "unpin " + req.ID
			case "archive":
				text = "archive " + req.ID
			case "restore":
				text = "restore " + req.ID
			case "forget":
				text = "forgot " + req.ID
			case "pending":
				text = "pending"
			case "approve":
				text = "approved " + req.ID
			case "reject":
				text = "rejected " + req.ID
			case "review":
				text = "review started"
			default:
				text = req.Operation
			}
			return &bus.StructuredContent{Fallback: text}, nil
		},
		CheckpointCommand: func(_ context.Context, req CheckpointCommandRequest) (*bus.StructuredContent, error) {
			var text string
			switch req.Operation {
			case "list":
				text = "checkpoint list"
			case "resume":
				text = "resumed " + req.ID
			case "archive":
				text = "archived " + req.ID
			default:
				text = req.Operation
			}
			return &bus.StructuredContent{Fallback: text}, nil
		},
	}
	executor := NewExecutor(NewRegistry(BuiltinDefinitions()), runtime)
	tests := []struct {
		command string
		want    string
	}{
		{"/memory status", "memory status"},
		{"/memory profile", "user profile"},
		{"/memory list", "memory list"},
		{"/memory search Go workflow", "searched Go workflow"},
		{"/memory edit mem_0000000000000000 concise replies", "edited mem_0000000000000000 to concise replies"},
		{"/memory pin mem_0000000000000000", "pin mem_0000000000000000"},
		{"/memory unpin mem_0000000000000000", "unpin mem_0000000000000000"},
		{"/memory archive mem_0000000000000000", "archive mem_0000000000000000"},
		{"/memory restore mem_0000000000000000", "restore mem_0000000000000000"},
		{"/memory forget mem_0000000000000000", "forgot mem_0000000000000000"},
		{"/memory pending", "pending"},
		{"/memory approve all", "approved all"},
		{"/memory reject pm_0000000000000000", "rejected pm_0000000000000000"},
		{"/memory review", "review started"},
		{"/checkpoint list", "checkpoint list"},
		{"/checkpoint resume cp_0000000000000000", "resumed cp_0000000000000000"},
		{"/checkpoint forget cp_0000000000000000", "archived cp_0000000000000000"},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			var reply string
			result := executor.Execute(context.Background(), Request{
				Channel: "telegram", Text: test.command,
				Reply: func(value string) error {
					reply = value
					return nil
				},
			})
			if result.Outcome != OutcomeHandled || result.Err != nil || reply != test.want {
				t.Fatalf(
					"Execute(%q) outcome=%v err=%v reply=%q, want %q",
					test.command,
					result.Outcome,
					result.Err,
					reply,
					test.want,
				)
			}
		})
	}
}

func TestMemoryRootCommand_DispatchesStructuredDashboard(t *testing.T) {
	called := false
	runtime := &Runtime{
		MemoryCommand: func(_ context.Context, req MemoryCommandRequest) (*bus.StructuredContent, error) {
			if req.Operation != "dashboard" {
				t.Fatalf("unexpected operation: %s", req.Operation)
			}
			called = true
			return &bus.StructuredContent{
				Title:      "Personal Memory",
				Paragraphs: []string{"Active: 10 entries"},
			}, nil
		},
	}
	executor := NewExecutor(NewRegistry(BuiltinDefinitions()), runtime)
	var structured bus.StructuredContent
	result := executor.Execute(context.Background(), Request{
		Channel: "telegram", Text: "/memory",
		ReplyStructured: func(content bus.StructuredContent) error {
			structured = content
			return nil
		},
	})
	if result.Outcome != OutcomeHandled || result.Err != nil {
		t.Fatalf("Execute(/memory) outcome=%v err=%v", result.Outcome, result.Err)
	}
	if !called {
		t.Fatalf("expected MemoryCommand handler to be called")
	}
	if structured.Title != "Personal Memory" {
		t.Fatalf("expected title 'Personal Memory', got %q", structured.Title)
	}
}

// TestClearAndResetUseSameHistorySemantics verifies /clear and /reset both
// call ClearHistory, while /new uses session-new semantics (not clear).
func TestClearAndResetUseSameHistorySemantics(t *testing.T) {
	clearCalls := 0
	executor := NewExecutor(NewRegistry(BuiltinDefinitions()), &Runtime{
		ClearHistory: func() error {
			clearCalls++
			return nil
		},
		SessionCommand: func(_ context.Context, req SessionCommandRequest) (*bus.StructuredContent, error) {
			if req.Operation != "new" {
				t.Fatalf("unexpected session operation: %s", req.Operation)
			}
			return &bus.StructuredContent{Fallback: "new session"}, nil
		},
	})
	// /clear and /reset should call ClearHistory
	for _, command := range []string{"/clear", "/reset"} {
		result := executor.Execute(context.Background(), Request{
			Channel: "telegram", Text: command, Reply: func(string) error { return nil },
		})
		if result.Outcome != OutcomeHandled || result.Err != nil {
			t.Fatalf("Execute(%q) outcome=%v err=%v", command, result.Outcome, result.Err)
		}
	}
	if clearCalls != 2 {
		t.Fatalf("ClearHistory calls = %d, want 2", clearCalls)
	}

	// /new should NOT call ClearHistory
	clearCalls = 0
	result := executor.Execute(context.Background(), Request{
		Channel: "telegram", Text: "/new", Reply: func(string) error { return nil },
	})
	if result.Outcome != OutcomeHandled || result.Err != nil {
		t.Fatalf("Execute(/new) outcome=%v err=%v", result.Outcome, result.Err)
	}
	if clearCalls != 0 {
		t.Fatalf("/new must not call ClearHistory, but it was called %d times", clearCalls)
	}
}
