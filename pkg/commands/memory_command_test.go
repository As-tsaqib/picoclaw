package commands

import (
	"context"
	"testing"
)

func TestMemoryAndCheckpointCommandsDispatchRuntimeControls(t *testing.T) {
	runtime := &Runtime{
		MemoryStatus:     func() string { return "memory status" },
		MemoryList:       func() (string, error) { return "memory list", nil },
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
		{"/memory list", "memory list"},
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

func TestClearAndResetUseSameHistorySemantics(t *testing.T) {
	calls := 0
	executor := NewExecutor(NewRegistry(BuiltinDefinitions()), &Runtime{
		ClearHistory: func() error {
			calls++
			return nil
		},
	})
	for _, command := range []string{"/clear", "/reset"} {
		result := executor.Execute(context.Background(), Request{
			Channel: "telegram", Text: command, Reply: func(string) error { return nil },
		})
		if result.Outcome != OutcomeHandled || result.Err != nil {
			t.Fatalf("Execute(%q) outcome=%v err=%v", command, result.Outcome, result.Err)
		}
	}
	if calls != 2 {
		t.Fatalf("ClearHistory calls = %d, want 2", calls)
	}
}
