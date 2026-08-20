package commands

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestSessionCommandDispatchesAllSupportedForms(t *testing.T) {
	tests := []struct {
		text      string
		operation string
		argument  string
	}{
		{text: "/session", operation: "list"},
		{text: "/session list", operation: "list"},
		{text: "/session current", operation: "current"},
		{text: "/session new", operation: "new"},
		{text: "/session new Watchdog Gateway", operation: "new", argument: "Watchdog Gateway"},
		{text: "/session rename Telegram Config", operation: "rename", argument: "Telegram Config"},
		{text: "/session remove", operation: "remove"},
		{text: "/session use a1b2c3d4", operation: "use", argument: "a1b2c3d4"},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			var got SessionCommandRequest
			rt := &Runtime{
				SessionCommand: func(_ context.Context, req SessionCommandRequest) (*bus.StructuredContent, error) {
					got = req
					return &bus.StructuredContent{Kind: "paragraph", Fallback: "ok"}, nil
				},
			}
			var structured bus.StructuredContent
			result := NewExecutor(
				NewRegistry([]Definition{sessionCommand()}),
				rt,
			).Execute(context.Background(), Request{
				Channel: "telegram", Text: tt.text,
				ReplyStructured: func(content bus.StructuredContent) error {
					structured = content
					return nil
				},
			})
			require.Equal(t, OutcomeHandled, result.Outcome)
			require.NoError(t, result.Err)
			assert.Equal(t, tt.operation, got.Operation)
			assert.Equal(t, tt.argument, got.Argument)
			assert.Equal(t, "paragraph", structured.Kind)
		})
	}
}

func TestInformationalCommandProducesStructuredTableAndPlainFallback(t *testing.T) {
	defs := BuiltinDefinitions()
	rt := &Runtime{ListDefinitions: func() []Definition { return defs }}
	executor := NewExecutor(NewRegistry(defs), rt)
	var content bus.StructuredContent
	result := executor.Execute(context.Background(), Request{
		Channel: "telegram", Text: "/help",
		ReplyStructured: func(value bus.StructuredContent) error {
			content = value
			return nil
		},
	})
	require.Equal(t, OutcomeHandled, result.Outcome)
	require.NoError(t, result.Err)
	require.Len(t, content.Tables, 1)
	assert.Equal(t, bus.CardHeaderColumns(bus.CardHeaderInventory, true), content.Tables[0].Columns)
	assert.True(t, content.Tables[0].Border)
	assert.True(t, content.Tables[0].Striped)
	assert.True(t, content.Tables[0].Header)
	assert.Contains(t, content.FallbackText(), "/session")

	var fallback string
	result = executor.Execute(context.Background(), Request{
		Channel: "discord", Text: "/help",
		Reply: func(value string) error {
			fallback = value
			return nil
		},
	})
	require.Equal(t, OutcomeHandled, result.Outcome)
	require.NoError(t, result.Err)
	assert.Equal(t, content.FallbackText(), fallback)
}
