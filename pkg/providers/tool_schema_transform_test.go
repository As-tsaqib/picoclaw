package providers

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/config"
	providercommon "github.com/As-tsaqib/picoclaw/pkg/providers/common"
)

type toolCaptureProvider struct {
	lastTools []ToolDefinition
}

func (p *toolCaptureProvider) Chat(
	_ context.Context,
	_ []Message,
	tools []ToolDefinition,
	_ string,
	_ map[string]any,
) (*LLMResponse, error) {
	p.lastTools = tools
	return &LLMResponse{Content: "ok"}, nil
}

func (p *toolCaptureProvider) GetDefaultModel() string {
	return "test"
}

type statefulToolCaptureProvider struct {
	toolCaptureProvider
	closes atomic.Int32
}

func (p *statefulToolCaptureProvider) Close() { p.closes.Add(1) }

func TestFinalizeProviderClosesStatefulProviderOnTransformValidationFailure(t *testing.T) {
	provider := &statefulToolCaptureProvider{}
	_, _, err := finalizeProviderFromConfig(provider, "test", &config.ModelConfig{
		ToolSchemaTransform: "invalid",
	})
	if err == nil {
		t.Fatal("expected invalid transform error")
	}
	if got := provider.closes.Load(); got != 1 {
		t.Fatalf("Close calls = %d, want 1", got)
	}
}

func TestWrapProviderWithToolSchemaTransform_DisabledPassesToolsThrough(t *testing.T) {
	capture := &toolCaptureProvider{}
	wrapped, err := wrapProviderWithToolSchemaTransform(capture, "")
	if err != nil {
		t.Fatalf("wrapProviderWithToolSchemaTransform() error = %v", err)
	}

	tools := []ToolDefinition{{
		Type: "function",
		Function: ToolFunctionDefinition{
			Name:       "noop",
			Parameters: map[string]any{"type": "object"},
		},
	}}

	_, err = wrapped.Chat(t.Context(), nil, tools, "test", nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if !reflect.DeepEqual(capture.lastTools, tools) {
		t.Fatalf("tools mutated with transform off\n got: %#v\nwant: %#v", capture.lastTools, tools)
	}
}

func TestWrapProviderWithToolSchemaTransform_GoogleSanitizesSchemas(t *testing.T) {
	capture := &toolCaptureProvider{}
	wrapped, err := wrapProviderWithToolSchemaTransform(capture, "simple")
	if err != nil {
		t.Fatalf("wrapProviderWithToolSchemaTransform() error = %v", err)
	}

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"parent": map[string]any{
				"anyOf": []any{
					map[string]any{"$ref": "#/$defs/pageParent"},
					map[string]any{"$ref": "#/$defs/databaseParent"},
				},
			},
		},
		"$defs": map[string]any{
			"pageParent": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"page_id": map[string]any{"type": "string"},
				},
			},
			"databaseParent": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"database_id": map[string]any{"type": "string"},
				},
			},
		},
	}
	tools := []ToolDefinition{{
		Type: "function",
		Function: ToolFunctionDefinition{
			Name:       "mcp_notion_create",
			Parameters: schema,
		},
	}}

	_, err = wrapped.Chat(t.Context(), nil, tools, "test", nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	want := providercommon.SanitizeSchemaForGoogle(schema)
	got := capture.lastTools[0].Function.Parameters
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitized parameters mismatch\n got: %#v\nwant: %#v", got, want)
	}
}
