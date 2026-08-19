package telegram

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ta "github.com/mymmrac/telego/telegoapi"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/capability"
	"github.com/As-tsaqib/picoclaw/pkg/channels"
	"github.com/As-tsaqib/picoclaw/pkg/media"
)

func TestTelegramSemanticNativeSingleMedia(t *testing.T) {
	tests := []struct {
		name, endpoint, filename, contentType string
		kind                                  bus.NativeSingleMediaKind
	}{
		{"animation", "sendAnimation", "a.gif", "image/gif", bus.NativeSingleMediaAnimation},
		{"sticker", "sendSticker", "s.webp", "image/webp", bus.NativeSingleMediaSticker},
		{"video_note", "sendVideoNote", "n.mp4", "video/mp4", bus.NativeSingleMediaVideoNote},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
				require.Contains(t, url, tc.endpoint)
				return successResponse(t), nil
			}}
			ch := newTestChannel(t, caller)
			path := filepath.Join(t.TempDir(), tc.filename)
			require.NoError(t, os.WriteFile(path, []byte("media"), 0o600))
			ch.SetMediaStore(&livePhotoTestStore{entries: map[string]livePhotoTestMedia{
				"media://asset": {
					path: path,
					meta: media.MediaMeta{Filename: tc.filename, ContentType: tc.contentType},
				},
			}})
			encoded, ok := bus.EncodeNativeSingleMediaRef(
				bus.NativeSingleMediaPayload{Kind: tc.kind, Ref: "media://asset"},
			)
			require.True(t, ok)
			ids, handled, err := ch.SendSemanticMedia(context.Background(), bus.OutboundMediaMessage{
				ChatID: "-100123/42",
				Context: bus.InboundContext{
					Channel:          "telegram",
					Account:          ch.Name(),
					ChatID:           "-100123",
					TopicID:          "42",
					ReplyToMessageID: "7",
				},
				Parts: []bus.MediaPart{{Ref: encoded}},
			})
			require.NoError(t, err)
			require.True(t, handled)
			require.Len(t, ids, 1)
			require.Len(t, caller.calls, 1)
		})
	}
}

func TestTelegramSemanticNativeSingleMediaRejectsWrongShape(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})
	path := filepath.Join(t.TempDir(), "bad.mp4")
	require.NoError(t, os.WriteFile(path, []byte("media"), 0o600))
	ch.SetMediaStore(&livePhotoTestStore{entries: map[string]livePhotoTestMedia{
		"media://asset": {path: path, meta: media.MediaMeta{Filename: "bad.mp4", ContentType: "video/mp4"}},
	}})
	encoded, _ := bus.EncodeNativeSingleMediaRef(
		bus.NativeSingleMediaPayload{Kind: bus.NativeSingleMediaSticker, Ref: "media://asset"},
	)
	_, handled, err := ch.SendSemanticMedia(context.Background(), bus.OutboundMediaMessage{
		ChatID: "123", Context: bus.InboundContext{Channel: "telegram", Account: ch.Name(), ChatID: "123"},
		Parts: []bus.MediaPart{{Ref: encoded}},
	})
	require.True(t, handled)
	require.ErrorIs(t, err, channels.ErrSendFailed)
}

func TestTelegramNativeSingleMediaUnsupportedDowngradesOnlyFeature(t *testing.T) {
	capability.GlobalNegativeCache.Clear()
	t.Cleanup(capability.GlobalNegativeCache.Clear)
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		if strings.Contains(url, "sendSticker") {
			return nil, errors.New("Bad Request: method not found")
		}
		return successResponse(t), nil
	}}
	ch := newTestChannel(t, caller)
	path := filepath.Join(t.TempDir(), "s.webp")
	require.NoError(t, os.WriteFile(path, []byte("media"), 0o600))
	ch.SetMediaStore(&livePhotoTestStore{entries: map[string]livePhotoTestMedia{
		"media://asset": {path: path, meta: media.MediaMeta{Filename: "s.webp", ContentType: "image/webp"}},
	}})
	encoded, _ := bus.EncodeNativeSingleMediaRef(
		bus.NativeSingleMediaPayload{Kind: bus.NativeSingleMediaSticker, Ref: "media://asset"},
	)
	_, handled, err := ch.SendSemanticMedia(context.Background(), bus.OutboundMediaMessage{
		ChatID: "123", Context: bus.InboundContext{Channel: "telegram", Account: ch.Name(), ChatID: "123"},
		Parts: []bus.MediaPart{{Ref: encoded}},
	})
	require.True(t, handled)
	require.ErrorIs(t, err, channels.ErrSendFailed)
	require.True(
		t,
		capability.GlobalNegativeCache.IsDowngraded(
			"telegram",
			ch.Name(),
			ch.tgCfg.BaseURL,
			capability.FeatureMediaSticker,
		),
	)
	require.False(
		t,
		capability.GlobalNegativeCache.IsDowngraded(
			"telegram",
			ch.Name(),
			ch.tgCfg.BaseURL,
			capability.FeatureMediaAnimation,
		),
	)
}
