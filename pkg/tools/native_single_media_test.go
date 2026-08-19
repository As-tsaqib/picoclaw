package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestNativeSingleMediaToolsUseMediaStoreRefsAndHandleResponse(t *testing.T) {
	tests := []struct {
		tool Tool
		kind bus.NativeSingleMediaKind
	}{
		{NewSendAnimationTool(), bus.NativeSingleMediaAnimation},
		{NewSendStickerTool(), bus.NativeSingleMediaSticker},
		{NewSendVideoNoteTool(), bus.NativeSingleMediaVideoNote},
	}
	for _, tc := range tests {
		result := tc.tool.Execute(context.Background(), map[string]any{"media_ref": "media://asset"})
		if result.IsError || !result.ResponseHandled || len(result.Media) != 1 {
			t.Fatalf("%s result=%#v", tc.tool.Name(), result)
		}
		payload, ok := bus.DecodeNativeSingleMediaRef(result.Media[0])
		if !ok || payload.Kind != tc.kind || payload.Ref != "media://asset" {
			t.Fatalf("%s payload=%#v ok=%v", tc.tool.Name(), payload, ok)
		}
		bad := tc.tool.Execute(context.Background(), map[string]any{"media_ref": "https://example.com/file"})
		if !bad.IsError {
			t.Fatalf("%s accepted URL", tc.tool.Name())
		}
	}
}

func TestSendAnimationCaptionBound(t *testing.T) {
	result := NewSendAnimationTool().Execute(context.Background(), map[string]any{
		"media_ref": "media://asset", "caption": strings.Repeat("x", nativeAnimationCaptionMaxChars+1),
	})
	if !result.IsError {
		t.Fatal("oversized caption accepted")
	}
}
