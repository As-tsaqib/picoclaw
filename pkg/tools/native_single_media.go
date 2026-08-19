package tools

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	toolshared "github.com/As-tsaqib/picoclaw/pkg/tools/shared"
)

const nativeAnimationCaptionMaxChars = 1024

type sendNativeSingleMediaTool struct {
	name        string
	description string
	kind        bus.NativeSingleMediaKind
	caption     bool
}

func NewSendAnimationTool() Tool {
	return &sendNativeSingleMediaTool{
		name:        "send_animation",
		kind:        bus.NativeSingleMediaAnimation,
		caption:     true,
		description: "Send an existing PicoClaw media:// ref as a native Telegram animation on the current trusted route.",
	}
}

func NewSendStickerTool() Tool {
	return &sendNativeSingleMediaTool{
		name:        "send_sticker",
		kind:        bus.NativeSingleMediaSticker,
		description: "Send an existing PicoClaw media:// ref as a native Telegram sticker on the current trusted route.",
	}
}

func NewSendVideoNoteTool() Tool {
	return &sendNativeSingleMediaTool{
		name:        "send_video_note",
		kind:        bus.NativeSingleMediaVideoNote,
		description: "Send an existing PicoClaw media:// ref as a native Telegram video note on the current trusted route.",
	}
}

func (t *sendNativeSingleMediaTool) Name() string        { return t.name }
func (t *sendNativeSingleMediaTool) Description() string { return t.description }
func (t *sendNativeSingleMediaTool) Parameters() map[string]any {
	properties := map[string]any{
		"media_ref": map[string]any{
			"type":        "string",
			"description": "Existing trusted PicoClaw media:// ref. URLs, local paths, Telegram file IDs and target chat IDs are not accepted.",
		},
	}
	if t.caption {
		properties["caption"] = map[string]any{
			"type": "string", "maxLength": nativeAnimationCaptionMaxChars,
			"description": "Optional animation caption, at most 1024 characters.",
		}
	}
	return map[string]any{
		"type": "object", "properties": properties, "required": []string{"media_ref"},
	}
}

func (t *sendNativeSingleMediaTool) Execute(_ context.Context, args map[string]any) *toolshared.ToolResult {
	ref, _ := args["media_ref"].(string)
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "media://") {
		return toolshared.ErrorResult("media_ref must be an existing trusted media:// ref")
	}
	caption := ""
	if t.caption {
		caption, _ = args["caption"].(string)
		caption = strings.TrimSpace(caption)
		if utf8.RuneCountInString(caption) > nativeAnimationCaptionMaxChars {
			return toolshared.ErrorResult("caption must be at most 1024 characters")
		}
	} else if raw, ok := args["caption"].(string); ok && strings.TrimSpace(raw) != "" {
		return toolshared.ErrorResult("caption is not supported for this native media type")
	}
	encoded, ok := bus.EncodeNativeSingleMediaRef(bus.NativeSingleMediaPayload{
		Kind: t.kind, Ref: ref, Caption: caption,
	})
	if !ok {
		return toolshared.ErrorResult("invalid native media payload")
	}
	return (&toolshared.ToolResult{
		ForLLM: "Native media queued for delivery.", Media: []string{encoded},
	}).WithResponseHandled()
}
