package tools

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	toolshared "github.com/As-tsaqib/picoclaw/pkg/tools/shared"
)

const livePhotoCaptionMaxChars = 1024

type sendLivePhotoTool struct{}

// NewSendLivePhotoTool creates a semantic paired live-photo action. It accepts
// only MediaStore refs already created by PicoClaw; URLs, local paths and raw
// Telegram file identifiers are intentionally not model-controlled here.
func NewSendLivePhotoTool() Tool { return &sendLivePhotoTool{} }

func (t *sendLivePhotoTool) Name() string { return "send_live_photo" }

func (t *sendLivePhotoTool) Description() string {
	return "Send one native live photo using an explicit static-photo media ref and its paired live-video media ref. " +
		"Do not pass arbitrary local paths, URLs, chat IDs, or Telegram file IDs."
}

func (t *sendLivePhotoTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"photo_ref": map[string]any{
				"type":        "string",
				"description": "Existing PicoClaw media:// ref for the static photo.",
			},
			"live_video_ref": map[string]any{
				"type":        "string",
				"description": "Existing PicoClaw media:// ref for the paired live-photo video (Telegram limit: 10 seconds, 10 MB).",
			},
			"caption": map[string]any{
				"type":        "string",
				"maxLength":   livePhotoCaptionMaxChars,
				"description": "Optional caption, at most 1024 characters.",
			},
		},
		"required": []string{"photo_ref", "live_video_ref"},
	}
}

func (t *sendLivePhotoTool) Execute(_ context.Context, args map[string]any) *toolshared.ToolResult {
	photoRef, _ := args["photo_ref"].(string)
	videoRef, _ := args["live_video_ref"].(string)
	caption, _ := args["caption"].(string)
	photoRef = strings.TrimSpace(photoRef)
	videoRef = strings.TrimSpace(videoRef)
	caption = strings.TrimSpace(caption)

	if !strings.HasPrefix(photoRef, "media://") || !strings.HasPrefix(videoRef, "media://") {
		return toolshared.ErrorResult("photo_ref and live_video_ref must be trusted media:// refs")
	}
	if photoRef == videoRef {
		return toolshared.ErrorResult("photo_ref and live_video_ref must refer to different media objects")
	}
	if utf8.RuneCountInString(caption) > livePhotoCaptionMaxChars {
		return toolshared.ErrorResult("caption must be at most 1024 characters")
	}

	encoded, ok := bus.EncodeLivePhotoMediaRef(bus.LivePhotoPayload{
		PhotoRef: photoRef, LiveVideoRef: videoRef, Caption: caption,
	})
	if !ok {
		return toolshared.ErrorResult("invalid live photo media pair")
	}
	return &toolshared.ToolResult{
		ForLLM:          "Native live photo queued for delivery.",
		Media:           []string{encoded},
		ResponseHandled: true,
	}
}
