package tools

import (
	"context"
	"testing"
)

func TestSendPollTool_ValidatesPollInputs(t *testing.T) {
	tool := NewSendPollTool()

	// Missing options
	result := tool.Execute(context.Background(), map[string]any{
		"question": "Test?",
		"options":  []any{"only_one"},
	})
	if !result.IsError {
		t.Fatal("expected error for < 2 options")
	}

	// Question too long (>300 chars)
	longQ := make([]byte, 301)
	for i := range longQ {
		longQ[i] = 'a'
	}
	result = tool.Execute(context.Background(), map[string]any{
		"question": string(longQ),
		"options":  []any{"A", "B"},
	})
	if !result.IsError {
		t.Fatal("expected error for question > 300 chars")
	}

	// Option too long (>100 chars)
	longOpt := make([]byte, 101)
	for i := range longOpt {
		longOpt[i] = 'b'
	}
	result = tool.Execute(context.Background(), map[string]any{
		"question": "Test?",
		"options":  []any{"A", string(longOpt)},
	})
	if !result.IsError {
		t.Fatal("expected error for option > 100 chars")
	}

	// Too many options (>12)
	manyOpts := make([]any, 13)
	for i := range manyOpts {
		manyOpts[i] = "opt"
	}
	result = tool.Execute(context.Background(), map[string]any{
		"question": "Test?",
		"options":  manyOpts,
	})
	if !result.IsError {
		t.Fatal("expected error for > 12 options")
	}

	// Valid poll
	result = tool.Execute(context.Background(), map[string]any{
		"question": "Favorite color?",
		"options":  []any{"Red", "Blue", "Green"},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if result.Poll == nil {
		t.Fatal("expected poll payload")
	}
	if result.Poll.Mode != "regular" {
		t.Fatalf("expected mode=regular, got %s", result.Poll.Mode)
	}
	if !result.ResponseHandled {
		t.Fatal("expected ResponseHandled=true")
	}
}

func TestSendQuizTool_ValidatesQuizInputs(t *testing.T) {
	tool := NewSendQuizTool()

	// No correct options
	result := tool.Execute(context.Background(), map[string]any{
		"question":           "Test?",
		"options":            []any{"A", "B"},
		"correct_option_ids": []any{},
	})
	if !result.IsError {
		t.Fatal("expected error for empty correct_option_ids")
	}

	// Correct option out of range
	result = tool.Execute(context.Background(), map[string]any{
		"question":           "Test?",
		"options":            []any{"A", "B"},
		"correct_option_ids": []any{float64(5)},
	})
	if !result.IsError {
		t.Fatal("expected error for out-of-range correct_option_id")
	}

	// Duplicate correct option
	result = tool.Execute(context.Background(), map[string]any{
		"question":           "Test?",
		"options":            []any{"A", "B", "C"},
		"correct_option_ids": []any{float64(0), float64(0)},
	})
	if !result.IsError {
		t.Fatal("expected error for duplicate correct_option_id")
	}

	// Explanation too long
	longExpl := make([]byte, 201)
	for i := range longExpl {
		longExpl[i] = 'x'
	}
	result = tool.Execute(context.Background(), map[string]any{
		"question":           "Test?",
		"options":            []any{"A", "B"},
		"correct_option_ids": []any{float64(0)},
		"explanation":        string(longExpl),
	})
	if !result.IsError {
		t.Fatal("expected error for explanation > 200 chars")
	}

	// Valid quiz with multiple correct answers
	result = tool.Execute(context.Background(), map[string]any{
		"question":           "Which are primary colors?",
		"options":            []any{"Red", "Blue", "Green", "Yellow"},
		"correct_option_ids": []any{float64(0), float64(1)},
		"explanation":        "Red and Blue are primary colors.",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if result.Poll == nil {
		t.Fatal("expected poll payload")
	}
	if result.Poll.Mode != "quiz" {
		t.Fatalf("expected mode=quiz, got %s", result.Poll.Mode)
	}
	if len(result.Poll.CorrectOptionIDs) != 2 {
		t.Fatalf("expected 2 correct option IDs, got %d", len(result.Poll.CorrectOptionIDs))
	}
	if result.Poll.CorrectOptionIDs[0] != 0 || result.Poll.CorrectOptionIDs[1] != 1 {
		t.Fatalf("incorrect correct_option_ids: %v", result.Poll.CorrectOptionIDs)
	}
	if !result.ResponseHandled {
		t.Fatal("expected ResponseHandled=true")
	}
}

func TestSendQuizTool_UsesModernCorrectOptionIDs(t *testing.T) {
	tool := NewSendQuizTool()
	result := tool.Execute(context.Background(), map[string]any{
		"question":           "Capital of Japan?",
		"options":            []any{"Tokyo", "Osaka", "Kyoto"},
		"correct_option_ids": []any{float64(0)},
	})
	if result.IsError || result.Poll == nil {
		t.Fatal("unexpected error or nil poll")
	}
	if len(result.Poll.CorrectOptionIDs) != 1 || result.Poll.CorrectOptionIDs[0] != 0 {
		t.Fatalf("expected correct_option_ids=[0], got %v", result.Poll.CorrectOptionIDs)
	}
}

func TestStopPollTool_RequiresPollHandle(t *testing.T) {
	tool := NewStopPollTool()

	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error for missing poll_handle")
	}

	result = tool.Execute(context.Background(), map[string]any{
		"poll_handle": "some-handle-123",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if result.StopPollID != "some-handle-123" {
		t.Fatalf("expected StopPollID=some-handle-123, got %s", result.StopPollID)
	}
}
