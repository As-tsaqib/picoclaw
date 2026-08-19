package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestSendLivePhotoToolRequiresExplicitPair(t *testing.T) {
	tool := NewSendLivePhotoTool()
	for _, args := range []map[string]any{
		{},
		{"photo_ref": "media://photo"},
		{"photo_ref": "/tmp/photo.jpg", "live_video_ref": "media://video"},
		{"photo_ref": "media://same", "live_video_ref": "media://same"},
	} {
		if result := tool.Execute(context.Background(), args); !result.IsError {
			t.Fatalf("invalid live photo args accepted: %#v", args)
		}
	}
}

func TestSendLivePhotoToolQueuesChannelNeutralEnvelope(t *testing.T) {
	tool := NewSendLivePhotoTool()
	result := tool.Execute(context.Background(), map[string]any{
		"photo_ref": "media://photo", "live_video_ref": "media://video", "caption": "hello",
	})
	if result.IsError || !result.ResponseHandled || len(result.Media) != 1 {
		t.Fatalf("unexpected live photo result: %#v", result)
	}
	payload, ok := bus.DecodeLivePhotoMediaRef(result.Media[0])
	if !ok || payload.PhotoRef != "media://photo" || payload.LiveVideoRef != "media://video" ||
		payload.Caption != "hello" {
		t.Fatalf("unexpected live photo payload: %#v ok=%t", payload, ok)
	}
}

func TestSendLivePhotoToolCaptionLimit(t *testing.T) {
	tool := NewSendLivePhotoTool()
	result := tool.Execute(context.Background(), map[string]any{
		"photo_ref": "media://photo", "live_video_ref": "media://video",
		"caption": strings.Repeat("x", livePhotoCaptionMaxChars+1),
	})
	if !result.IsError {
		t.Fatal("oversize live photo caption accepted")
	}
}
