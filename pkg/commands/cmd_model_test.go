package commands

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestBuiltinDefinitionsIncludeModelCommand(t *testing.T) {
	var model *Definition
	definitions := BuiltinDefinitions()
	for i := range definitions {
		if definitions[i].Name == "model" {
			model = &definitions[i]
			break
		}
	}
	require.NotNil(t, model)
	assert.Equal(t, "View and switch the model for this session", model.Description)
	names := make([]string, 0, len(model.SubCommands))
	for _, sub := range model.SubCommands {
		names = append(names, sub.Name)
	}
	assert.ElementsMatch(t, []string{"current", "list", "use", "default", "search"}, names)
}

func TestModelCommandDispatchesSupportedFormsWithTextFallback(t *testing.T) {
	tests := []struct {
		text string
		want ModelCommandRequest
	}{
		{text: "/model", want: ModelCommandRequest{Operation: "dashboard"}},
		{text: "/model current", want: ModelCommandRequest{Operation: "current"}},
		{text: "/model list", want: ModelCommandRequest{Operation: "list"}},
		{
			text: "/model use configured-alias",
			want: ModelCommandRequest{Operation: "use", Argument: "configured-alias"},
		},
		{
			text: "/model use provider/discovered-model",
			want: ModelCommandRequest{Operation: "use", Argument: "provider/discovered-model"},
		},
		{text: "/model default", want: ModelCommandRequest{Operation: "default"}},
		{text: "/model search vision", want: ModelCommandRequest{Operation: "search", Argument: "vision"}},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			var got ModelCommandRequest
			var reply string
			rt := &Runtime{
				ModelCommand: func(_ context.Context, req ModelCommandRequest) (*bus.StructuredContent, error) {
					got = req
					return &bus.StructuredContent{Kind: "model_test", Fallback: "text fallback"}, nil
				},
			}
			result := NewExecutor(NewRegistry([]Definition{modelCommand()}), rt).Execute(context.Background(), Request{
				Channel: "cli",
				Text:    tt.text,
				Reply: func(text string) error {
					reply = text
					return nil
				},
			})
			assert.Equal(t, OutcomeHandled, result.Outcome)
			require.NoError(t, result.Err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, "text fallback", reply)
		})
	}
}

func TestModelCommandRejectsMissingArgumentsWithoutRuntimeMutation(t *testing.T) {
	for _, text := range []string{"/model use", "/model search"} {
		t.Run(text, func(t *testing.T) {
			called := false
			var reply string
			rt := &Runtime{ModelCommand: func(context.Context, ModelCommandRequest) (*bus.StructuredContent, error) {
				called = true
				return nil, nil
			}}
			result := NewExecutor(NewRegistry([]Definition{modelCommand()}), rt).Execute(context.Background(), Request{
				Text: text,
				Reply: func(text string) error {
					reply = text
					return nil
				},
			})
			assert.Equal(t, OutcomeHandled, result.Outcome)
			require.NoError(t, result.Err)
			assert.False(t, called)
			assert.Contains(t, reply, "Usage: /model")
		})
	}
}

func TestSwitchModelDelegatesToModelCommandWhenAvailable(t *testing.T) {
	definition := switchCommand()
	require.NotEmpty(t, definition.SubCommands)
	var modelHandler Handler
	for _, sub := range definition.SubCommands {
		if sub.Name == "model" {
			modelHandler = sub.Handler
			break
		}
	}
	require.NotNil(t, modelHandler)

	var got ModelCommandRequest
	var reply string
	rt := &Runtime{ModelCommand: func(_ context.Context, req ModelCommandRequest) (*bus.StructuredContent, error) {
		got = req
		return &bus.StructuredContent{
			Kind:     "model_changed",
			Fallback: "Switched model from old to gpt-next",
		}, nil
	}}
	err := modelHandler(context.Background(), Request{
		Text: "/switch model to gpt-next",
		Reply: func(text string) error {
			reply = text
			return nil
		},
		ReplyStructured: func(content bus.StructuredContent) error {
			reply = content.FallbackText()
			return nil
		},
	}, rt)
	require.NoError(t, err)
	assert.Equal(t, ModelCommandRequest{Operation: "use", Argument: "gpt-next", LegacySwitch: true}, got)
	assert.Equal(t, "Switched model from old to gpt-next", reply)
}
