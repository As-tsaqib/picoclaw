package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	toolshared "github.com/As-tsaqib/picoclaw/pkg/tools/shared"
)

const (
	pollQuestionMaxChars    = 300
	pollOptionMaxChars      = 100
	pollDescriptionMaxChars = 1024
	pollExplanationMaxChars = 200
	pollMinOptions          = 1
	pollMaxOptions          = 12
	pollMinOpenPeriod       = 5
	pollMaxOpenPeriod       = 2628000
	pollMaxCountryCodes     = 12
)

func validatePollInputs(question string, options []string) error {
	qLen := utf8.RuneCountInString(question)
	if qLen == 0 {
		return fmt.Errorf("question is required")
	}
	if qLen > pollQuestionMaxChars {
		return fmt.Errorf("question must be at most %d characters, got %d", pollQuestionMaxChars, qLen)
	}
	if len(options) < pollMinOptions {
		return fmt.Errorf("polls must have at least %d option", pollMinOptions)
	}
	if len(options) > pollMaxOptions {
		return fmt.Errorf("polls must have at most %d options, got %d", pollMaxOptions, len(options))
	}
	for i, opt := range options {
		optLen := utf8.RuneCountInString(opt)
		if optLen == 0 || optLen > pollOptionMaxChars {
			return fmt.Errorf("option %d must be 1-%d characters, got %d", i, pollOptionMaxChars, optLen)
		}
	}
	return nil
}

func validatePollCommon(description string, openPeriod int, closeAt time.Time, countryCodes []string) error {
	if utf8.RuneCountInString(description) > pollDescriptionMaxChars {
		return fmt.Errorf("description must be at most %d characters", pollDescriptionMaxChars)
	}
	if openPeriod != 0 && (openPeriod < pollMinOpenPeriod || openPeriod > pollMaxOpenPeriod) {
		return fmt.Errorf("open_period_seconds must be %d-%d", pollMinOpenPeriod, pollMaxOpenPeriod)
	}
	if openPeriod != 0 && !closeAt.IsZero() {
		return fmt.Errorf("open_period_seconds and close_date_unix are mutually exclusive")
	}
	if !closeAt.IsZero() {
		delta := time.Until(closeAt)
		if delta < pollMinOpenPeriod*time.Second || delta > pollMaxOpenPeriod*time.Second {
			return fmt.Errorf(
				"close_date_unix must be %d-%d seconds in the future",
				pollMinOpenPeriod,
				pollMaxOpenPeriod,
			)
		}
	}
	if len(countryCodes) > pollMaxCountryCodes {
		return fmt.Errorf("country_codes must contain at most %d entries", pollMaxCountryCodes)
	}
	for _, code := range countryCodes {
		trimmed := strings.ToUpper(strings.TrimSpace(code))
		if trimmed == "FT" {
			continue
		}
		if len(trimmed) != 2 || trimmed[0] < 'A' || trimmed[0] > 'Z' ||
			trimmed[1] < 'A' || trimmed[1] > 'Z' {
			return fmt.Errorf("country code %q must be a two-letter ISO 3166-1 alpha-2 code or FT", code)
		}
	}
	return nil
}

func parseStringSliceArg(args map[string]any, key string) []string {
	values, _ := args[key].([]any)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s, ok := value.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func parseIntArg(args map[string]any, key string) int {
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func pollCommonSchemaProperties() map[string]any {
	return map[string]any{
		"is_anonymous": map[string]any{
			"type":        "boolean",
			"description": "Whether the poll is anonymous. Defaults to true.",
		},
		"allows_revoting": map[string]any{
			"type": "boolean",
			"description": "Whether voters may change their answer. " +
				"Regular polls default to true; quizzes default to false.",
		},
		"shuffle_options": map[string]any{
			"type":        "boolean",
			"description": "Whether Telegram should display options in random order.",
		},
		"hide_results_until_closes": map[string]any{
			"type":        "boolean",
			"description": "Hide poll results until the poll closes.",
		},
		"members_only": map[string]any{
			"type":        "boolean",
			"description": "Restrict voting to channel members. Telegram accepts this only for channel chats.",
		},
		"country_codes": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"maxItems":    pollMaxCountryCodes,
			"description": "Optional country restriction for channel polls: 0-12 ISO 3166-1 alpha-2 codes, plus FT.",
		},
		"description": map[string]any{
			"type":        "string",
			"maxLength":   pollDescriptionMaxChars,
			"description": "Optional poll description (0-1024 chars).",
		},
		"open_period_seconds": map[string]any{
			"type":        "integer",
			"minimum":     pollMinOpenPeriod,
			"maximum":     pollMaxOpenPeriod,
			"description": "Seconds the poll remains open (5-2628000). Mutually exclusive with close_date_unix.",
		},
		"close_date_unix": map[string]any{
			"type":        "integer",
			"description": "Unix timestamp when the poll closes, 5-2628000 seconds in the future. " +
				"Mutually exclusive with open_period_seconds.",
		},
		"is_closed": map[string]any{
			"type":        "boolean",
			"description": "Send the poll already closed.",
		},
	}
}

func NewSendPollTool() Tool { return &sendPollTool{} }

type sendPollTool struct{}

func (t *sendPollTool) Name() string { return "send_poll" }

func (t *sendPollTool) Description() string {
	return `Send a regular native poll to the current channel.
Use this when you need to gather opinions or let users vote. Do not use for quizzes; use send_quiz instead.`
}

func (t *sendPollTool) Parameters() map[string]any {
	props := pollCommonSchemaProperties()
	props["question"] = map[string]any{
		"type": "string", "minLength": 1, "maxLength": pollQuestionMaxChars,
		"description": "Poll question (1-300 chars).",
	}
	props["options"] = map[string]any{
		"type": "array", "minItems": pollMinOptions, "maxItems": pollMaxOptions,
		"items": map[string]any{
			"type": "string", "minLength": 1, "maxLength": pollOptionMaxChars,
		},
		"description": "Array of 1-12 options (1-100 chars each).",
	}
	props["allows_multiple_answers"] = map[string]any{
		"type": "boolean", "description": "Whether voters may choose multiple answers.",
	}
	props["allow_adding_options"] = map[string]any{
		"type": "boolean", "description": "Allow voters to add options. Invalid for anonymous polls and quizzes.",
	}
	return map[string]any{"type": "object", "properties": props, "required": []string{"question", "options"}}
}

func (t *sendPollTool) Execute(_ context.Context, args map[string]any) *toolshared.ToolResult {
	question, _ := args["question"].(string)
	options := parseStringSliceArg(args, "options")
	if err := validatePollInputs(question, options); err != nil {
		return toolshared.ErrorResult(err.Error())
	}

	payload := &bus.PollPayload{
		ID: uuid.New().String(), Mode: "regular", Question: question, Options: options,
		IsAnonymous: true, AllowsRevoting: true,
	}
	if val, ok := args["is_anonymous"].(bool); ok {
		payload.IsAnonymous = val
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
	if val, ok := args["allow_adding_options"].(bool); ok {
		payload.AllowAddingOptions = val
	}
	if val, ok := args["hide_results_until_closes"].(bool); ok {
		payload.HideResultsUntilCloses = val
	}
	if val, ok := args["members_only"].(bool); ok {
		payload.MembersOnly = val
	}
	if val, ok := args["is_closed"].(bool); ok {
		payload.IsClosed = val
	}
	payload.Description, _ = args["description"].(string)
	payload.OpenPeriodSeconds = parseIntArg(args, "open_period_seconds")
	payload.CountryCodes = parseStringSliceArg(args, "country_codes")
	if closeUnix := parseIntArg(args, "close_date_unix"); closeUnix != 0 {
		payload.CloseAt = time.Unix(int64(closeUnix), 0)
	}
	if payload.AllowAddingOptions && payload.IsAnonymous {
		return toolshared.ErrorResult("allow_adding_options is not supported for anonymous polls")
	}
	if err := validatePollCommon(
		payload.Description,
		payload.OpenPeriodSeconds,
		payload.CloseAt,
		payload.CountryCodes,
	); err != nil {
		return toolshared.ErrorResult(err.Error())
	}
	for i := range payload.CountryCodes {
		payload.CountryCodes[i] = strings.ToUpper(strings.TrimSpace(payload.CountryCodes[i]))
	}

	return &toolshared.ToolResult{
		ForLLM: "Native poll queued for delivery. poll_handle=" + payload.ID,
		ResponseHandled: true,
		Poll:            payload,
	}
}

func NewSendQuizTool() Tool { return &sendQuizTool{} }

type sendQuizTool struct{}

func (t *sendQuizTool) Name() string { return "send_quiz" }

func (t *sendQuizTool) Description() string {
	return `Send a native Telegram-style quiz through the channel capability layer.
A quiz requires one or more correct_option_ids and may include an explanation.`
}

func (t *sendQuizTool) Parameters() map[string]any {
	props := pollCommonSchemaProperties()
	props["question"] = map[string]any{
		"type": "string", "minLength": 1, "maxLength": pollQuestionMaxChars,
		"description": "Quiz question (1-300 chars).",
	}
	props["options"] = map[string]any{
		"type": "array", "minItems": pollMinOptions, "maxItems": pollMaxOptions,
		"items": map[string]any{
			"type": "string", "minLength": 1, "maxLength": pollOptionMaxChars,
		},
		"description": "Array of 1-12 options (1-100 chars each).",
	}
	props["correct_option_ids"] = map[string]any{
		"type": "array", "minItems": 1,
		"items": map[string]any{"type": "integer", "minimum": 0},
		"description": "Monotonically increasing unique 0-based indices of correct answers.",
	}
	props["explanation"] = map[string]any{
		"type": "string", "maxLength": pollExplanationMaxChars,
		"description": "Explanation shown for an incorrect answer (0-200 chars, at most 2 line feeds).",
	}
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   []string{"question", "options", "correct_option_ids"},
	}
}

func (t *sendQuizTool) Execute(_ context.Context, args map[string]any) *toolshared.ToolResult {
	question, _ := args["question"].(string)
	options := parseStringSliceArg(args, "options")
	if err := validatePollInputs(question, options); err != nil {
		return toolshared.ErrorResult(err.Error())
	}

	correctInterface, _ := args["correct_option_ids"].([]any)
	correctOptionIDs := make([]int, 0, len(correctInterface))
	for _, id := range correctInterface {
		switch v := id.(type) {
		case float64:
			correctOptionIDs = append(correctOptionIDs, int(v))
		case int:
			correctOptionIDs = append(correctOptionIDs, v)
		case int64:
			correctOptionIDs = append(correctOptionIDs, int(v))
		}
	}
	if len(correctOptionIDs) == 0 {
		return toolshared.ErrorResult("correct_option_ids must contain at least one option index")
	}
	sort.Ints(correctOptionIDs)
	seen := make(map[int]struct{}, len(correctOptionIDs))
	for _, id := range correctOptionIDs {
		if id < 0 || id >= len(options) {
			return toolshared.ErrorResult(
				fmt.Sprintf("correct_option_ids entry %d is out of range [0, %d)", id, len(options)),
			)
		}
		if _, dup := seen[id]; dup {
			return toolshared.ErrorResult(fmt.Sprintf("correct_option_ids contains duplicate index %d", id))
		}
		seen[id] = struct{}{}
	}

	explanation, _ := args["explanation"].(string)
	if utf8.RuneCountInString(explanation) > pollExplanationMaxChars {
		return toolshared.ErrorResult("explanation must be at most 200 characters")
	}
	if strings.Count(explanation, "\n") > 2 {
		return toolshared.ErrorResult("explanation must contain at most 2 line feeds")
	}

	payload := &bus.PollPayload{
		ID: uuid.New().String(), Mode: "quiz", Question: question, Options: options,
		CorrectOptionIDs: correctOptionIDs, Explanation: explanation, IsAnonymous: true,
	}
	if val, ok := args["is_anonymous"].(bool); ok {
		payload.IsAnonymous = val
	}
	if val, ok := args["allows_revoting"].(bool); ok {
		payload.AllowsRevoting = val
	}
	if val, ok := args["shuffle_options"].(bool); ok {
		payload.ShuffleOptions = val
	}
	if val, ok := args["hide_results_until_closes"].(bool); ok {
		payload.HideResultsUntilCloses = val
	}
	if val, ok := args["members_only"].(bool); ok {
		payload.MembersOnly = val
	}
	if val, ok := args["is_closed"].(bool); ok {
		payload.IsClosed = val
	}
	payload.Description, _ = args["description"].(string)
	payload.OpenPeriodSeconds = parseIntArg(args, "open_period_seconds")
	payload.CountryCodes = parseStringSliceArg(args, "country_codes")
	if closeUnix := parseIntArg(args, "close_date_unix"); closeUnix != 0 {
		payload.CloseAt = time.Unix(int64(closeUnix), 0)
	}
	if err := validatePollCommon(
		payload.Description,
		payload.OpenPeriodSeconds,
		payload.CloseAt,
		payload.CountryCodes,
	); err != nil {
		return toolshared.ErrorResult(err.Error())
	}
	for i := range payload.CountryCodes {
		payload.CountryCodes[i] = strings.ToUpper(strings.TrimSpace(payload.CountryCodes[i]))
	}

	return &toolshared.ToolResult{
		ForLLM: "Native quiz queued for delivery. poll_handle=" + payload.ID,
		ResponseHandled: true,
		Poll:            payload,
	}
}

func NewStopPollTool() Tool { return &stopPollTool{} }

type stopPollTool struct{}

func (t *stopPollTool) Name() string { return "stop_poll" }

func (t *stopPollTool) Description() string {
	return "Stop a native poll or quiz using its opaque poll_handle. " +
		"The current trusted route is bound by PicoClaw and cannot be overridden by model input."
}

func (t *stopPollTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"poll_handle": map[string]any{
				"type":        "string",
				"description": "Opaque handle of the PicoClaw-created poll to stop.",
			},
		},
		"required": []string{"poll_handle"},
	}
}

func (t *stopPollTool) Execute(ctx context.Context, args map[string]any) *toolshared.ToolResult {
	pollHandle, _ := args["poll_handle"].(string)
	pollHandle = strings.TrimSpace(pollHandle)
	if pollHandle == "" {
		return toolshared.ErrorResult("poll_handle is required")
	}
	caller, ok := ToolCallerScope(ctx)
	if !ok || strings.TrimSpace(caller.AgentID) == "" || strings.TrimSpace(caller.SessionKey) == "" ||
		strings.TrimSpace(caller.ChatID) == "" {
		return toolshared.ErrorResult("trusted route is unavailable for stop_poll")
	}
	boundHandle := bus.NewPollStopRouteToken(
		pollHandle,
		caller.Account,
		caller.ChatID,
		caller.TopicID,
		caller.AgentID,
		"",
		caller.SessionKey,
	)
	if boundHandle == "" {
		return toolshared.ErrorResult("trusted route is unavailable for stop_poll")
	}
	return &toolshared.ToolResult{
		ForLLM:         "Stop poll command queued.",
		ResponseHandled: true,
		StopPollID:      boundHandle,
	}
}
