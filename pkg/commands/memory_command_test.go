package commands

import (
	"context"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestMemoryAndCheckpointCommandsDispatchRuntimeControls(t *testing.T) {
	runtime := &Runtime{
		MemoryStatus:  func() string { return "memory status" },
		MemoryProfile: func() (string, error) { return "user profile", nil },
		MemoryList:    func() (string, error) { return "memory list", nil },
		MemorySearch:  func(query string) (string, error) { return "searched " + query, nil },
		MemoryEdit: func(id, content string) (string, error) {
			return "edited " + id + " to " + content, nil
		},
		MemoryEntryAction: func(action, id string) (string, error) {
			return action + " " + id, nil
		},
		MemoryForget:     func(id string) (string, error) { return "forgot " + id, nil },
		MemoryPending:    func() (string, error) { return "pending", nil },
		MemoryApprove:    func(id string) (string, error) { return "approved " + id, nil },
		MemoryReject:     func(id string) (string, error) { return "rejected " + id, nil },
		MemoryReview:     func(context.Context) (string, error) { return "review started", nil },
		CheckpointList:   func() (string, error) { return "checkpoint list", nil },
		CheckpointResume: func(id string) (string, error) { return "resumed " + id, nil },
		CheckpointForget: func(id string) (string, error) { return "archived " + id, nil },
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
		MemoryCommand: func(ctx context.Context, req MemoryCommandRequest) (*bus.StructuredContent, error) {
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

func TestClearAndResetUseSameHistorySemantics(t *testing.T) {
	calls := 0
	executor := NewExecutor(NewRegistry(BuiltinDefinitions()), &Runtime{
		ClearHistory: func() error {
			calls++
			return nil
		},
	})
	for _, command := range []string{"/clear", "/reset", "/new"} {
		result := executor.Execute(context.Background(), Request{
			Channel: "telegram", Text: command, Reply: func(string) error { return nil },
		})
		if result.Outcome != OutcomeHandled || result.Err != nil {
			t.Fatalf("Execute(%q) outcome=%v err=%v", command, result.Outcome, result.Err)
		}
	}
	if calls != 3 {
		t.Fatalf("ClearHistory calls = %d, want 3", calls)
	}
}
