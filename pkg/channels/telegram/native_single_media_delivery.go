package telegram

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/capability"
	"github.com/As-tsaqib/picoclaw/pkg/channels"
	"github.com/As-tsaqib/picoclaw/pkg/media"
)

func nativeSingleMediaCapability(kind bus.NativeSingleMediaKind) (capability.Feature, bool) {
	switch kind {
	case bus.NativeSingleMediaAnimation:
		return capability.FeatureMediaAnimation, true
	case bus.NativeSingleMediaSticker:
		return capability.FeatureMediaSticker, true
	case bus.NativeSingleMediaVideoNote:
		return capability.FeatureMediaVideoNote, true
	default:
		return "", false
	}
}

func telegramMediaCapability(partType string) (capability.Feature, bool) {
	switch strings.ToLower(strings.TrimSpace(partType)) {
	case "animation":
		return capability.FeatureMediaAnimation, true
	case "sticker":
		return capability.FeatureMediaSticker, true
	case "video_note":
		return capability.FeatureMediaVideoNote, true
	default:
		return "", false
	}
}

func nativeSingleMediaShape(kind bus.NativeSingleMediaKind, path string, meta media.MediaMeta) bool {
	contentType := strings.ToLower(strings.TrimSpace(meta.ContentType))
	filename := strings.TrimSpace(meta.Filename)
	if filename != "" {
		path = filename
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch kind {
	case bus.NativeSingleMediaAnimation:
		if contentType != "" {
			return contentType == "image/gif" || contentType == "video/mp4"
		}
		return ext == ".gif" || ext == ".mp4"
	case bus.NativeSingleMediaSticker:
		if contentType != "" {
			return contentType == "image/webp" || contentType == "image/png" ||
				contentType == "video/webm" || contentType == "application/x-tgsticker"
		}
		return ext == ".webp" || ext == ".png" || ext == ".webm" || ext == ".tgs"
	case bus.NativeSingleMediaVideoNote:
		if contentType != "" {
			return contentType == "video/mp4"
		}
		return ext == ".mp4"
	default:
		return false
	}
}

func (c *TelegramChannel) sendNativeSingleMedia(
	ctx context.Context,
	msg bus.OutboundMediaMessage,
	payload bus.NativeSingleMediaPayload,
) ([]string, error) {
	store := c.GetMediaStore()
	if store == nil {
		return nil, fmt.Errorf("no media store available: %w", channels.ErrSendFailed)
	}
	path, meta, err := store.ResolveWithMeta(payload.Ref)
	if err != nil {
		return nil, fmt.Errorf("resolve native %s: %w", payload.Kind, channels.ErrSendFailed)
	}
	if !nativeSingleMediaShape(payload.Kind, path, meta) {
		return nil, fmt.Errorf("media_ref is not a valid %s: %w", payload.Kind, channels.ErrSendFailed)
	}
	if payload.Kind != bus.NativeSingleMediaAnimation && strings.TrimSpace(payload.Caption) != "" {
		return nil, fmt.Errorf("caption is not supported for %s: %w", payload.Kind, channels.ErrSendFailed)
	}
	ordinary := msg
	ordinary.Parts = []bus.MediaPart{{
		Ref: payload.Ref, Type: string(payload.Kind), Caption: payload.Caption,
		Filename: meta.Filename, ContentType: meta.ContentType,
	}}
	return c.SendMedia(ctx, ordinary)
}
