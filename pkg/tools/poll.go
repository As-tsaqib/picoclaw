package tools

import (
	"context"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	toolshared "github.com/As-tsaqib/picoclaw/pkg/tools/shared"
)

func validatePollInputs(question string, options []string) error {
	qLen := utf8.RuneCountInString(question)
	if qLen == 0 {
		return fmt.Errorf("question is required")
	}
	if qLen > 300 {
		return fmt.Errorf("question must be at most 300 characters, got %d", qLen)
	}
	if len(options) < 2 {
		return fmt.Errorf("polls must have at least 2 options")
	}
	if len(options) > 12 {
		return fmt.Errorf("polls must have at most 12 options, got %d", len(options))
	}
	for i, opt := range options {
		optLen := utf8.RuneCountInString(opt)
		if optLen == 0 || optLen > 100 {
			return fmt.Errorf("option %d must be 1-100 characters, got %d", i, optLen)
		}
	}
	return nil
}

func NewSendPollTool() Tool {
	return &sendPollTool{}
}

type sendPollTool struct{}

func (t *sendPollTool) Name() string { return "send_poll" }

func (t *sendPollTool) Description() string {
	return `Send a regular or native poll to the current channel.
Use this when you need to gather opinions or let users vote. Do not use for quizzes; use send_quiz instead.
`
}

func (t *sendPollTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": "Poll question (1-300 chars).",
			},
			"options": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Array of 2-12 options (1-100 chars each).",
			},
			"is_anonymous": map[string]any{
				"type":        "boolean",
				"description": "True if the poll needs to be anonymous, defaults to True.",
			},
			"allows_multiple_answers": map[string]any{
				"type":        "boolean",
				"description": "Pass True if the poll allows multiple answers.",
			},
			"allows_revoting": map[string]any{
				"type":        "boolean",
				"description": "Pass True if the poll allows voters to change their chosen answers.",
			},
			"shuffle_options": map[string]any{
				"type":        "boolean",
				"description": "Pass True if the poll options should be shown in random order.",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Optional description for the poll (0-1024 chars).",
			},
			"open_period_seconds": map[string]any{
				"type":        "integer",
				"description": "Amount of time in seconds the poll will be active (5-2628000).",
			},
		},
		"required": []string{"question", "options"},
	}
}

func (t *sendPollTool) Execute(ctx context.Context, args map[string]any) *toolshared.ToolResult {
	question, _ := args["question"].(string)
	optionsInterface, _ := args["options"].([]any)
	var options []string
	for _, opt := range optionsInterface {
		if s, ok := opt.(string); ok {
			options = append(options, s)
		}
	}

	if err := validatePollInputs(question, options); err != nil {
		return toolshared.ErrorResult(err.Error())
	}

	payload := &bus.PollPayload{
		ID:       uuid.New().String(),
		Mode:     "regular",
		Question: question,
		Options:  options,
	}
	if val, ok := args["allows_multiple_answers"].(bool); ok {
		payload.AllowsMultipleAnswers = val
	}
	if val, ok := args["allows_revoting"].(bool); ok {
		payload.AllowsRevoting = val
	}
	if val, ok := args["shuffle_options"].(bool); ok {
		payload.ShuffleOptions = val
	}
	if desc, ok := args["description"].(string); ok {
		payload.Description = desc
	}
	if val, ok := args["open_period_seconds"].(float64); ok {
		payload.OpenPeriodSeconds = int(val)
	}
	if val, ok := args["is_anonymous"].(bool); ok {
		payload.IsAnonymous = val
	} else {
		payload.IsAnonymous = true
	}

	return &toolshared.ToolResult{
		ForLLM:          "Native poll queued for delivery. poll_handle=" + payload.ID,
		ResponseHandled: true,
		Poll:            payload,
	}
}

func NewSendQuizTool() Tool {
	return &sendQuizTool{}
}

type sendQuizTool struct{}

func (t *sendQuizTool) Name() string { return "send_quiz" }

func (t *sendQuizTool) Description() string {
	return `Send a native quiz poll to the user.
Quizzes have exactly one or multiple correct answers and display an explanation if the user gets it wrong.
`
}

func (t *sendQuizTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": "Quiz question (1-300 chars).",
			},
			"options": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Array of 2-12 options (1-100 chars each).",
			},
			"correct_option_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "integer"},
				"description": "0-based indices of the correct answers. Must contain at least one.",
			},
			"explanation": map[string]any{
				"type":        "string",
				"description": "Text that is shown when a user chooses an incorrect answer (0-200 chars).",
			},
			"is_anonymous": map[string]any{
				"type":        "boolean",
				"description": "True if the poll needs to be anonymous, defaults to True.",
			},
			"allows_revoting": map[string]any{
				"type":        "boolean",
				"description": "Pass True if the quiz allows revoting.",
			},
			"shuffle_options": map[string]any{
				"type":        "boolean",
				"description": "Pass True if the quiz options should be shuffled.",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Optional description for the quiz (0-1024 chars).",
			},
			"open_period_seconds": map[string]any{
				"type":        "integer",
				"description": "Amount of time in seconds the quiz will be active (5-2628000).",
			},
		},
		"required": []string{"question", "options", "correct_option_ids"},
	}
}

func (t *sendQuizTool) Execute(ctx context.Context, args map[string]any) *toolshared.ToolResult {
	question, _ := args["question"].(string)
	optionsInterface, _ := args["options"].([]any)
	var options []string
	for _, opt := range optionsInterface {
		if s, ok := opt.(string); ok {
			options = append(options, s)
		}
	}

	correctInterface, _ := args["correct_option_ids"].([]any)
	var correctOptionIDs []int
	for _, id := range correctInterface {
		if v, ok := id.(float64); ok {
			correctOptionIDs = append(correctOptionIDs, int(v))
		}
	}

	if err := validatePollInputs(question, options); err != nil {
		return toolshared.ErrorResult(err.Error())
	}
	if len(correctOptionIDs) == 0 {
		return toolshared.ErrorResult("quizzes must have at least one correct_option_id")
	}

	sort.Ints(correctOptionIDs)

	seen := make(map[int]struct{}, len(correctOptionIDs))
	for _, id := range correctOptionIDs {
		if id < 0 || id >= len(options) {
			return toolshared.ErrorResult(fmt.Sprintf("correct_option_id %d is out of range [0, %d)", id, len(options)))
		}
		if _, dup := seen[id]; dup {
			return toolshared.ErrorResult(fmt.Sprintf("duplicate correct_option_id %d", id))
		}
		seen[id] = struct{}{}
	}

	explanation, _ := args["explanation"].(string)
	if utf8.RuneCountInString(explanation) > 200 {
		return toolshared.ErrorResult("explanation must be at most 200 characters")
	}

	payload := &bus.PollPayload{
		ID:               uuid.New().String(),
		Mode:             "quiz",
		Question:         question,
		Options:          options,
		CorrectOptionIDs: correctOptionIDs,
	}
	payload.Explanation = explanation
	if val, ok := args["allows_revoting"].(bool); ok {
		payload.AllowsRevoting = val
	}
	if val, ok := args["shuffle_options"].(bool); ok {
		payload.ShuffleOptions = val
	}
	if desc, ok := args["description"].(string); ok {
		payload.Description = desc
	}
	if val, ok := args["open_period_seconds"].(float64); ok {
		payload.OpenPeriodSeconds = int(val)
	}
	if val, ok := args["is_anonymous"].(bool); ok {
		payload.IsAnonymous = val
	} else {
		payload.IsAnonymous = true
	}

	return &toolshared.ToolResult{
		ForLLM:          "Native quiz queued for delivery. poll_handle=" + payload.ID,
		ResponseHandled: true,
		Poll:            payload,
	}
}

func NewStopPollTool() Tool {
	return &stopPollTool{}
}

type stopPollTool struct{}

func (t *stopPollTool) Name() string { return "stop_poll" }

func (t *stopPollTool) Description() string {
	return `Stop a native poll or quiz.
You must provide the opaque poll_handle.
`
}

func (t *stopPollTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"poll_handle": map[string]any{
				"type":        "string",
				"description": "The opaque handle of the poll to stop.",
			},
		},
		"required": []string{"poll_handle"},
	}
}

func (t *stopPollTool) Execute(ctx context.Context, args map[string]any) *toolshared.ToolResult {
	pollHandle, _ := args["poll_handle"].(string)
	if pollHandle == "" {
		return toolshared.ErrorResult("poll_handle is required")
	}

	return &toolshared.ToolResult{
		ForLLM:          "Stop poll command queued.",
		ResponseHandled: true,
		StopPollID:      pollHandle,
	}
}
