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

type livePhotoTestMedia struct {
	path string
	meta media.MediaMeta
}

type livePhotoTestStore struct {
	entries map[string]livePhotoTestMedia
}

func (s *livePhotoTestStore) Store(string, media.MediaMeta, string) (string, error) {
	return "", errors.New("not implemented in test store")
}

func (s *livePhotoTestStore) Resolve(ref string) (string, error) {
	entry, ok := s.entries[ref]
	if !ok {
		return "", errors.New("unknown ref")
	}
	return entry.path, nil
}

func (s *livePhotoTestStore) ResolveWithMeta(ref string) (string, media.MediaMeta, error) {
	entry, ok := s.entries[ref]
	if !ok {
		return "", media.MediaMeta{}, errors.New("unknown ref")
	}
	return entry.path, entry.meta, nil
}

func (s *livePhotoTestStore) ReleaseAll(string) error { return nil }

func TestTelegramSemanticLivePhotoSuccess(t *testing.T) {
	capability.GlobalNegativeCache.Clear()
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		require.Contains(t, url, "sendLivePhoto")
		return successResponse(t), nil
	}}
	ch := newTestChannel(t, caller)
	photo, video := livePhotoTestFiles(t)
	ch.SetMediaStore(&livePhotoTestStore{entries: map[string]livePhotoTestMedia{
		"media://photo": {path: photo, meta: media.MediaMeta{Filename: "photo.jpg", ContentType: "image/jpeg"}},
		"media://video": {path: video, meta: media.MediaMeta{Filename: "live.mp4", ContentType: "video/mp4"}},
	}})
	ref, ok := bus.EncodeLivePhotoMediaRef(bus.LivePhotoPayload{
		PhotoRef: "media://photo", LiveVideoRef: "media://video", Caption: "hello",
	})
	require.True(t, ok)

	ids, handled, err := ch.SendSemanticMedia(context.Background(), bus.OutboundMediaMessage{
		ChatID: "-1001234567890/42",
		Context: bus.InboundContext{
			Channel: "telegram", Account: ch.Name(), ChatID: "-1001234567890", TopicID: "42",
			ReplyToMessageID: "7",
		},
		Parts: []bus.MediaPart{{Ref: ref}},
	})
	require.NoError(t, err)
	require.True(t, handled)
	require.Len(t, ids, 1)
	require.Len(t, caller.calls, 1)
}

func TestTelegramSemanticLivePhotoRejectsWrongMediaShape(t *testing.T) {
	caller := &stubCaller{callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
		return successResponse(t), nil
	}}
	ch := newTestChannel(t, caller)
	photo, video := livePhotoTestFiles(t)
	ch.SetMediaStore(&livePhotoTestStore{entries: map[string]livePhotoTestMedia{
		"media://photo": {path: photo, meta: media.MediaMeta{Filename: "not-photo.mp4", ContentType: "video/mp4"}},
		"media://video": {path: video, meta: media.MediaMeta{Filename: "live.mp4", ContentType: "video/mp4"}},
	}})
	ref, _ := bus.EncodeLivePhotoMediaRef(
		bus.LivePhotoPayload{PhotoRef: "media://photo", LiveVideoRef: "media://video"},
	)

	_, handled, err := ch.SendSemanticMedia(context.Background(), bus.OutboundMediaMessage{
		ChatID: "12345", Context: bus.InboundContext{Channel: "telegram", Account: ch.Name(), ChatID: "12345"},
		Parts: []bus.MediaPart{{Ref: ref}},
	})
	require.True(t, handled)
	require.Error(t, err)
	require.ErrorIs(t, err, channels.ErrSendFailed)
	require.Empty(t, caller.calls)
}

func TestTelegramSemanticLivePhotoUnsupportedDowngradesOnlyLivePhoto(t *testing.T) {
	capability.GlobalNegativeCache.Clear()
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		if strings.Contains(url, "sendLivePhoto") {
			return nil, errors.New("Bad Request: method not found")
		}
		return successResponse(t), nil
	}}
	ch := newTestChannel(t, caller)
	photo, video := livePhotoTestFiles(t)
	ch.SetMediaStore(&livePhotoTestStore{entries: map[string]livePhotoTestMedia{
		"media://photo": {path: photo, meta: media.MediaMeta{Filename: "photo.jpg", ContentType: "image/jpeg"}},
		"media://video": {path: video, meta: media.MediaMeta{Filename: "live.mp4", ContentType: "video/mp4"}},
	}})
	ref, _ := bus.EncodeLivePhotoMediaRef(
		bus.LivePhotoPayload{PhotoRef: "media://photo", LiveVideoRef: "media://video"},
	)

	_, handled, err := ch.SendSemanticMedia(context.Background(), bus.OutboundMediaMessage{
		ChatID: "12345", Context: bus.InboundContext{Channel: "telegram", Account: ch.Name(), ChatID: "12345"},
		Parts: []bus.MediaPart{{Ref: ref}},
	})
	require.True(t, handled)
	require.ErrorIs(t, err, channels.ErrSendFailed)
	require.True(t, capability.GlobalNegativeCache.IsDowngraded(
		"telegram", ch.Name(), ch.tgCfg.BaseURL, capability.FeatureMediaLivePhoto,
	))
	require.False(t, capability.GlobalNegativeCache.IsDowngraded(
		"telegram", ch.Name(), ch.tgCfg.BaseURL, capability.FeatureMediaImage,
	))
}

func TestTelegramSemanticMediaDelegatesOrdinaryMedia(t *testing.T) {
	ch := newTestChannel(t, &stubCaller{})
	ids, handled, err := ch.SendSemanticMedia(context.Background(), bus.OutboundMediaMessage{
		Parts: []bus.MediaPart{{Ref: "media://ordinary"}},
	})
	require.NoError(t, err)
	require.False(t, handled)
	require.Nil(t, ids)
}

func livePhotoTestFiles(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	photo := filepath.Join(dir, "photo.jpg")
	video := filepath.Join(dir, "live.mp4")
	require.NoError(t, os.WriteFile(photo, []byte("photo-bytes"), 0o600))
	require.NoError(t, os.WriteFile(video, []byte("video-bytes"), 0o600))
	return photo, video
}
