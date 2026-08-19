package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
)

func TestSendPollTool_CurrentBotAPILimits(t *testing.T) {
	tool := NewSendPollTool()

	result := tool.Execute(context.Background(), map[string]any{
		"question": "Only option?", "options": []any{"Yes"},
	})
	if result.IsError || result.Poll == nil {
		t.Fatalf("one-option poll must be accepted: %s", result.ForLLM)
	}
	if !result.Poll.AllowsRevoting {
		t.Fatal("regular poll must default allows_revoting=true")
	}

	result = tool.Execute(context.Background(), map[string]any{
		"question": "No options?", "options": []any{},
	})
	if !result.IsError {
		t.Fatal("expected error for zero options")
	}

	longQ := strings.Repeat("q", 301)
	result = tool.Execute(context.Background(), map[string]any{
		"question": longQ, "options": []any{"A"},
	})
	if !result.IsError {
		t.Fatal("expected error for question > 300 chars")
	}

	longOpt := strings.Repeat("o", 101)
	result = tool.Execute(context.Background(), map[string]any{
		"question": "Test?", "options": []any{longOpt},
	})
	if !result.IsError {
		t.Fatal("expected error for option > 100 chars")
	}

	manyOpts := make([]any, 13)
	for i := range manyOpts {
		manyOpts[i] = "opt"
	}
	result = tool.Execute(context.Background(), map[string]any{
		"question": "Test?", "options": manyOpts,
	})
	if !result.IsError {
		t.Fatal("expected error for > 12 options")
	}

	result = tool.Execute(context.Background(), map[string]any{
		"question": "Test?", "options": []any{"A"},
		"description": strings.Repeat("d", 1025),
	})
	if !result.IsError {
		t.Fatal("expected error for description > 1024 chars")
	}

	for _, period := range []float64{4, 2628001} {
		result = tool.Execute(context.Background(), map[string]any{
			"question": "Test?", "options": []any{"A"}, "open_period_seconds": period,
		})
		if !result.IsError {
			t.Fatalf("expected invalid open period %v", period)
		}
	}

	result = tool.Execute(context.Background(), map[string]any{
		"question": "Test?", "options": []any{"A"},
		"open_period_seconds": float64(30),
		"close_date_unix":     float64(time.Now().Add(time.Minute).Unix()),
	})
	if !result.IsError {
		t.Fatal("expected open_period/close_date mutual exclusion")
	}

	result = tool.Execute(context.Background(), map[string]any{
		"question": "Test?", "options": []any{"A"}, "allow_adding_options": true,
	})
	if !result.IsError {
		t.Fatal("anonymous poll must reject allow_adding_options")
	}
	result = tool.Execute(context.Background(), map[string]any{
		"question": "Test?", "options": []any{"A"},
		"is_anonymous": false, "allow_adding_options": true,
	})
	if result.IsError {
		t.Fatalf("non-anonymous regular poll should allow adding options: %s", result.ForLLM)
	}
}

func TestSendQuizTool_CurrentValidation(t *testing.T) {
	tool := NewSendQuizTool()

	result := tool.Execute(context.Background(), map[string]any{
		"question": "Test?", "options": []any{"A"}, "correct_option_ids": []any{},
	})
	if !result.IsError {
		t.Fatal("expected error for empty correct_option_ids")
	}

	result = tool.Execute(context.Background(), map[string]any{
		"question": "Test?", "options": []any{"A"}, "correct_option_ids": []any{float64(1)},
	})
	if !result.IsError {
		t.Fatal("expected error for out-of-range correct option ID")
	}

	result = tool.Execute(context.Background(), map[string]any{
		"question": "Test?", "options": []any{"A", "B"},
		"correct_option_ids": []any{float64(0), float64(0)},
	})
	if !result.IsError {
		t.Fatal("expected duplicate correct option rejection")
	}

	result = tool.Execute(context.Background(), map[string]any{
		"question": "Test?", "options": []any{"A"},
		"correct_option_ids": []any{float64(0)}, "explanation": strings.Repeat("x", 201),
	})
	if !result.IsError {
		t.Fatal("expected explanation length rejection")
	}

	result = tool.Execute(context.Background(), map[string]any{
		"question": "Test?", "options": []any{"A"},
		"correct_option_ids": []any{float64(0)}, "explanation": "a\nb\nc\nd",
	})
	if !result.IsError {
		t.Fatal("expected explanation line-feed rejection")
	}

	result = tool.Execute(context.Background(), map[string]any{
		"question": "Single-answer quiz?", "options": []any{"Correct"},
		"correct_option_ids": []any{float64(0)},
	})
	if result.IsError || result.Poll == nil {
		t.Fatalf("one-option quiz must be accepted: %s", result.ForLLM)
	}

	result = tool.Execute(context.Background(), map[string]any{
		"question": "Which?", "options": []any{"A", "B", "C"},
		"correct_option_ids": []any{float64(2), float64(0)}, "explanation": "A and C.",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if got := result.Poll.CorrectOptionIDs; len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("expected sorted correct_option_ids [0 2], got %v", got)
	}
}

func TestSendPollTool_SchemaAdvertisesCurrentOptionLimit(t *testing.T) {
	props := NewSendPollTool().Parameters()["properties"].(map[string]any)
	options := props["options"].(map[string]any)
	if options["minItems"] != 1 {
		t.Fatalf("expected minItems=1, got %v", options["minItems"])
	}
	if options["maxItems"] != 12 {
		t.Fatalf("expected maxItems=12, got %v", options["maxItems"])
	}
}

func TestStopPollTool_RequiresPollHandle(t *testing.T) {
	tool := NewStopPollTool()
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error for missing poll_handle")
	}

	caller := memory.CallerScope{
		AgentID:    "main",
		Account:    "telegram",
		ChatID:     "12345",
		TopicID:    "42",
		SessionKey: "session-1",
	}
	ctx := WithToolCallerScope(context.Background(), caller)
	result = tool.Execute(ctx, map[string]any{"poll_handle": "some-handle-123"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	want := bus.NewPollStopRouteToken(
		"some-handle-123",
		caller.Account,
		caller.ChatID,
		caller.TopicID,
		caller.AgentID,
		"",
		caller.SessionKey,
	)
	if result.StopPollID != want {
		t.Fatalf("expected route-bound StopPollID=%s, got %s", want, result.StopPollID)
	}
}
