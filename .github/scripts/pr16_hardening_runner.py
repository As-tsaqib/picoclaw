from pathlib import Path
import subprocess
import textwrap

REPO = Path.cwd()
BASE_HELPER_COMMIT = "7c9c7235972f82d6c3b156ddd0af15367eac7eb9"


def replace_once(path: str, old: str, new: str) -> None:
    p = REPO / path
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"{path}: expected one replacement, found {count}: {old[:120]!r}")
    p.write_text(text.replace(old, new, 1))


def run(*args: str) -> None:
    subprocess.run(args, cwd=REPO, check=True)


def apply_historical_base_patch() -> None:
    source = subprocess.check_output(
        ["git", "show", f"{BASE_HELPER_COMMIT}:.github/workflows/pr16-final-hardening.yml"],
        cwd=REPO,
        text=True,
    )
    start_marker = "      - name: Apply final hardening patch\n        shell: bash\n        run: |\n"
    end_marker = "\n      - name: Targeted Go validation"
    start = source.index(start_marker) + len(start_marker)
    end = source.index(end_marker, start)
    block = source[start:end]
    lines = [line[10:] if line.startswith("          ") else line for line in block.splitlines()]
    patch = Path("/tmp/pr16-base-hardening.sh")
    patch.write_text("\n".join(lines) + "\n")
    subprocess.run(["bash", str(patch)], cwd=REPO, check=True)


def write_native_payload() -> None:
    (REPO / "pkg/bus/native_single_media.go").write_text(r'''package bus

import (
    "encoding/base64"
    "encoding/json"
    "strings"
)

const nativeSingleMediaRefPrefix = "picoclaw-native-media:v1:"

type NativeSingleMediaKind string

const (
    NativeSingleMediaAnimation NativeSingleMediaKind = "animation"
    NativeSingleMediaSticker   NativeSingleMediaKind = "sticker"
    NativeSingleMediaVideoNote NativeSingleMediaKind = "video_note"
)

type NativeSingleMediaPayload struct {
    Kind    NativeSingleMediaKind `json:"kind"`
    Ref     string                `json:"ref"`
    Caption string                `json:"caption,omitempty"`
}

func validNativeSingleMediaPayload(payload NativeSingleMediaPayload) bool {
    if !strings.HasPrefix(strings.TrimSpace(payload.Ref), "media://") {
        return false
    }
    switch payload.Kind {
    case NativeSingleMediaAnimation:
        return true
    case NativeSingleMediaSticker, NativeSingleMediaVideoNote:
        return strings.TrimSpace(payload.Caption) == ""
    default:
        return false
    }
}

func EncodeNativeSingleMediaRef(payload NativeSingleMediaPayload) (string, bool) {
    payload.Ref = strings.TrimSpace(payload.Ref)
    payload.Caption = strings.TrimSpace(payload.Caption)
    if !validNativeSingleMediaPayload(payload) {
        return "", false
    }
    raw, err := json.Marshal(payload)
    if err != nil {
        return "", false
    }
    return nativeSingleMediaRefPrefix + base64.RawURLEncoding.EncodeToString(raw), true
}

func DecodeNativeSingleMediaRef(value string) (NativeSingleMediaPayload, bool) {
    value = strings.TrimSpace(value)
    if !strings.HasPrefix(value, nativeSingleMediaRefPrefix) {
        return NativeSingleMediaPayload{}, false
    }
    raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, nativeSingleMediaRefPrefix))
    if err != nil {
        return NativeSingleMediaPayload{}, false
    }
    var payload NativeSingleMediaPayload
    if err := json.Unmarshal(raw, &payload); err != nil {
        return NativeSingleMediaPayload{}, false
    }
    payload.Ref = strings.TrimSpace(payload.Ref)
    payload.Caption = strings.TrimSpace(payload.Caption)
    return payload, validNativeSingleMediaPayload(payload)
}
''')


def write_native_tools() -> None:
    (REPO / "pkg/tools/native_single_media.go").write_text(r'''package tools

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
        name: "send_animation", kind: bus.NativeSingleMediaAnimation, caption: true,
        description: "Send an existing PicoClaw media:// ref as a native Telegram animation on the current trusted route.",
    }
}

func NewSendStickerTool() Tool {
    return &sendNativeSingleMediaTool{
        name: "send_sticker", kind: bus.NativeSingleMediaSticker,
        description: "Send an existing PicoClaw media:// ref as a native Telegram sticker on the current trusted route.",
    }
}

func NewSendVideoNoteTool() Tool {
    return &sendNativeSingleMediaTool{
        name: "send_video_note", kind: bus.NativeSingleMediaVideoNote,
        description: "Send an existing PicoClaw media:// ref as a native Telegram video note on the current trusted route.",
    }
}

func (t *sendNativeSingleMediaTool) Name() string { return t.name }
func (t *sendNativeSingleMediaTool) Description() string { return t.description }
func (t *sendNativeSingleMediaTool) Parameters() map[string]any {
    properties := map[string]any{
        "media_ref": map[string]any{
            "type": "string",
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
''')


def write_native_telegram_delivery() -> None:
    (REPO / "pkg/channels/telegram/native_single_media_delivery.go").write_text(r'''package telegram

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
''')


def write_tests() -> None:
    (REPO / "pkg/bus/native_single_media_test.go").write_text(r'''package bus

import "testing"

func TestNativeSingleMediaRefRoundTrip(t *testing.T) {
    cases := []NativeSingleMediaPayload{
        {Kind: NativeSingleMediaAnimation, Ref: "media://a", Caption: "caption"},
        {Kind: NativeSingleMediaSticker, Ref: "media://s"},
        {Kind: NativeSingleMediaVideoNote, Ref: "media://v"},
    }
    for _, tc := range cases {
        encoded, ok := EncodeNativeSingleMediaRef(tc)
        if !ok {
            t.Fatalf("EncodeNativeSingleMediaRef(%#v) failed", tc)
        }
        decoded, ok := DecodeNativeSingleMediaRef(encoded)
        if !ok || decoded != tc {
            t.Fatalf("round trip got %#v ok=%v, want %#v", decoded, ok, tc)
        }
    }
}

func TestNativeSingleMediaRefRejectsAuthorityAndInvalidCaptionShape(t *testing.T) {
    if _, ok := EncodeNativeSingleMediaRef(NativeSingleMediaPayload{Kind: NativeSingleMediaAnimation, Ref: "https://example.com/a.gif"}); ok {
        t.Fatal("URL escaped MediaStore boundary")
    }
    if _, ok := EncodeNativeSingleMediaRef(NativeSingleMediaPayload{Kind: NativeSingleMediaSticker, Ref: "media://s", Caption: "not allowed"}); ok {
        t.Fatal("sticker caption unexpectedly accepted")
    }
}
''')
    (REPO / "pkg/tools/native_single_media_test.go").write_text(r'''package tools

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
''')
    (REPO / "pkg/channels/manager_native_hardening_test.go").write_text(r'''package channels

import (
    "context"
    "testing"

    "golang.org/x/time/rate"

    "github.com/As-tsaqib/picoclaw/pkg/bus"
)

type preferredTestChannel struct {
    mockSessionStreamingChannel
    preferredCalled bool
    legacyCalled    bool
}

func (c *preferredTestChannel) BeginPreferredStreamForSession(_ context.Context, _, _ string) (Streamer, error) {
    c.preferredCalled = true
    return c.streamer, nil
}

func (c *preferredTestChannel) BeginStreamForSession(ctx context.Context, chatID, sessionKey string) (Streamer, error) {
    c.legacyCalled = true
    return c.mockSessionStreamingChannel.BeginStreamForSession(ctx, chatID, sessionKey)
}

func TestManagerPrefersPreferredSessionStreamer(t *testing.T) {
    ch := &preferredTestChannel{}
    ch.streamer = &mockStreamer{}
    m := newTestManager()
    m.channels["telegram"] = ch
    streamer, ok := m.GetStreamer(context.Background(), "telegram", "1", "session")
    if !ok || streamer == nil || !ch.preferredCalled || ch.legacyCalled {
        t.Fatalf("preferred=%v legacy=%v ok=%v streamer=%T", ch.preferredCalled, ch.legacyCalled, ok, streamer)
    }
}

type semanticMediaTestChannel struct {
    mockMediaChannel
    semanticHandled bool
    semanticErr     error
    semanticCalls   int
}

func (c *semanticMediaTestChannel) SendSemanticMedia(_ context.Context, _ bus.OutboundMediaMessage) ([]string, bool, error) {
    c.semanticCalls++
    return []string{"semantic"}, c.semanticHandled, c.semanticErr
}

func TestManagerSemanticMediaPrecedesOrdinaryMedia(t *testing.T) {
    ch := &semanticMediaTestChannel{semanticHandled: true}
    ch.sendMediaFn = func(context.Context, bus.OutboundMediaMessage) ([]string, error) {
        t.Fatal("ordinary media path called after semantic handler accepted payload")
        return nil, nil
    }
    m := newTestManager()
    w := &channelWorker{ch: ch, limiter: rate.NewLimiter(rate.Inf, 1)}
    ids, err := m.sendMediaWithRetry(context.Background(), "telegram", w, bus.OutboundMediaMessage{
        Context: bus.InboundContext{Channel: "telegram"}, Parts: []bus.MediaPart{{Ref: "semantic"}},
    })
    if err != nil || len(ids) != 1 || ids[0] != "semantic" || ch.semanticCalls != 1 {
        t.Fatalf("ids=%v err=%v semanticCalls=%d", ids, err, ch.semanticCalls)
    }
}

func TestManagerSemanticMediaDelegatesWhenUnhandled(t *testing.T) {
    ch := &semanticMediaTestChannel{}
    ch.sendMediaFn = func(context.Context, bus.OutboundMediaMessage) ([]string, error) {
        return []string{"ordinary"}, nil
    }
    m := newTestManager()
    w := &channelWorker{ch: ch, limiter: rate.NewLimiter(rate.Inf, 1)}
    ids, err := m.sendMediaWithRetry(context.Background(), "telegram", w, bus.OutboundMediaMessage{
        Context: bus.InboundContext{Channel: "telegram"}, Parts: []bus.MediaPart{{Ref: "media://ordinary"}},
    })
    if err != nil || len(ids) != 1 || ids[0] != "ordinary" || ch.semanticCalls != 1 || len(ch.sentMediaMessages) != 1 {
        t.Fatalf("ids=%v err=%v semanticCalls=%d ordinaryCalls=%d", ids, err, ch.semanticCalls, len(ch.sentMediaMessages))
    }
}
''')
    (REPO / "pkg/channels/telegram/native_single_media_delivery_test.go").write_text(r'''package telegram

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
        kind bus.NativeSingleMediaKind
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
                "media://asset": {path: path, meta: media.MediaMeta{Filename: tc.filename, ContentType: tc.contentType}},
            }})
            encoded, ok := bus.EncodeNativeSingleMediaRef(bus.NativeSingleMediaPayload{Kind: tc.kind, Ref: "media://asset"})
            require.True(t, ok)
            ids, handled, err := ch.SendSemanticMedia(context.Background(), bus.OutboundMediaMessage{
                ChatID: "-100123/42",
                Context: bus.InboundContext{Channel: "telegram", Account: ch.Name(), ChatID: "-100123", TopicID: "42", ReplyToMessageID: "7"},
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
    encoded, _ := bus.EncodeNativeSingleMediaRef(bus.NativeSingleMediaPayload{Kind: bus.NativeSingleMediaSticker, Ref: "media://asset"})
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
    encoded, _ := bus.EncodeNativeSingleMediaRef(bus.NativeSingleMediaPayload{Kind: bus.NativeSingleMediaSticker, Ref: "media://asset"})
    _, handled, err := ch.SendSemanticMedia(context.Background(), bus.OutboundMediaMessage{
        ChatID: "123", Context: bus.InboundContext{Channel: "telegram", Account: ch.Name(), ChatID: "123"},
        Parts: []bus.MediaPart{{Ref: encoded}},
    })
    require.True(t, handled)
    require.ErrorIs(t, err, channels.ErrSendFailed)
    require.True(t, capability.GlobalNegativeCache.IsDowngraded("telegram", ch.Name(), ch.tgCfg.BaseURL, capability.FeatureMediaSticker))
    require.False(t, capability.GlobalNegativeCache.IsDowngraded("telegram", ch.Name(), ch.tgCfg.BaseURL, capability.FeatureMediaAnimation))
}
''')


def apply_additional_hardening() -> None:
    # Manager must prefer the richer session streamer when a channel implements both.
    replace_once(
        "pkg/channels/manager.go",
        '''\tbeginStream := func(beginCtx context.Context) (bus.Streamer, error) {\n\t\tif sessionCapable, ok := ch.(SessionStreamingCapable); ok {\n\t\t\treturn sessionCapable.BeginStreamForSession(beginCtx, chatID, sessionKey)\n\t\t}\n\t\tif streamingCapable, ok := ch.(StreamingCapable); ok {\n\t\t\treturn streamingCapable.BeginStream(beginCtx, chatID)\n\t\t}\n\t\treturn nil, fmt.Errorf("channel %q does not support streaming", channelName)\n\t}\n\tif _, sessionAware := ch.(SessionStreamingCapable); !sessionAware {\n\t\tif _, streamingCapable := ch.(StreamingCapable); !streamingCapable {\n\t\t\treturn nil, false\n\t\t}\n\t}\n''',
        '''\tbeginStream := func(beginCtx context.Context) (bus.Streamer, error) {\n\t\tif preferred, ok := ch.(PreferredSessionStreamingCapable); ok {\n\t\t\treturn preferred.BeginPreferredStreamForSession(beginCtx, chatID, sessionKey)\n\t\t}\n\t\tif sessionCapable, ok := ch.(SessionStreamingCapable); ok {\n\t\t\treturn sessionCapable.BeginStreamForSession(beginCtx, chatID, sessionKey)\n\t\t}\n\t\tif streamingCapable, ok := ch.(StreamingCapable); ok {\n\t\t\treturn streamingCapable.BeginStream(beginCtx, chatID)\n\t\t}\n\t\treturn nil, fmt.Errorf("channel %q does not support streaming", channelName)\n\t}\n\tif _, preferred := ch.(PreferredSessionStreamingCapable); !preferred {\n\t\tif _, sessionAware := ch.(SessionStreamingCapable); !sessionAware {\n\t\t\tif _, streamingCapable := ch.(StreamingCapable); !streamingCapable {\n\t\t\t\treturn nil, false\n\t\t\t}\n\t\t}\n\t}\n''')

    replace_once(
        "pkg/channels/manager.go",
        '''\tms, ok := w.ch.(MediaSender)\n\tif !ok {\n\t\terr := fmt.Errorf("channel %q does not support media sending", name)\n\t\tlogger.WarnCF("channels", "Channel does not support MediaSender", map[string]any{\n\t\t\t"channel": name,\n\t\t\t"error":   err.Error(),\n\t\t})\n\t\treturn nil, err\n\t}\n''',
        '''\tms, hasMediaSender := w.ch.(MediaSender)\n\tsemantic, hasSemanticMedia := w.ch.(SemanticMediaCapable)\n\tif !hasMediaSender && !hasSemanticMedia {\n\t\terr := fmt.Errorf("channel %q does not support media sending", name)\n\t\tlogger.WarnCF("channels", "Channel does not support media sending", map[string]any{\n\t\t\t"channel": name,\n\t\t\t"error":   err.Error(),\n\t\t})\n\t\treturn nil, err\n\t}\n''')
    replace_once(
        "pkg/channels/manager.go",
        '''\tfor attempt := 0; attempt <= maxRetries; attempt++ {\n\t\tmsgIDs, lastErr = ms.SendMedia(ctx, msg)\n\t\tif lastErr == nil {\n''',
        '''\tfor attempt := 0; attempt <= maxRetries; attempt++ {\n\t\thandled := false\n\t\tif hasSemanticMedia {\n\t\t\tmsgIDs, handled, lastErr = semantic.SendSemanticMedia(ctx, msg)\n\t\t}\n\t\tif !handled {\n\t\t\tif !hasMediaSender {\n\t\t\t\tlastErr = fmt.Errorf("channel %q did not handle semantic media and has no ordinary media sender", name)\n\t\t\t} else {\n\t\t\t\tmsgIDs, lastErr = ms.SendMedia(ctx, msg)\n\t\t\t}\n\t\t}\n\t\tif lastErr == nil {\n''')

    # StopPoll must validate the actual runtime route, not substitute registry metadata.
    replace_once(
        "pkg/channels/telegram/poll_registry.go",
        '''func (c *TelegramChannel) StopPollForRoute(\n\tctx context.Context,\n\tlocalHandle string,\n\troute telegramPollRoute,\n) error {\n\tentry, ok := c.resolvePollByLocalHandle(localHandle)\n\tif !ok {\n\t\treturn fmt.Errorf("poll %q not found or expired", localHandle)\n\t}\n\treturn c.stopPollEntry(ctx, localHandle, entry, route)\n}\n''',
        '''func (c *TelegramChannel) StopPollForRoute(\n\tctx context.Context,\n\tlocalHandle string,\n\troute telegramPollRoute,\n) error {\n\tlookupHandle := strings.TrimSpace(localHandle)\n\tif handle, _, ok := bus.ParsePollStopRouteToken(lookupHandle); ok {\n\t\tlookupHandle = handle\n\t}\n\tentry, ok := c.resolvePollByLocalHandle(lookupHandle)\n\tif !ok {\n\t\treturn fmt.Errorf("poll %q not found or expired", lookupHandle)\n\t}\n\tverifiedHandle, err := stopPollHandleForEntry(localHandle, entry)\n\tif err != nil || verifiedHandle != lookupHandle {\n\t\treturn fmt.Errorf("not authorized to stop poll: poll route proof mismatch")\n\t}\n\treturn c.stopPollEntry(ctx, lookupHandle, entry, route)\n}\n''')
    replace_once(
        "pkg/channels/telegram/telegram.go",
        '''\tif msg.StopPollID != "" {\n\t\terr := c.StopPoll(ctx, msg.StopPollID, msg.AgentID, msg.SessionKey, msg.Context.SenderID)\n''',
        '''\tif msg.StopPollID != "" {\n\t\taccount := strings.TrimSpace(msg.Context.Account)\n\t\tif account == "" {\n\t\t\taccount = c.Name()\n\t\t}\n\t\terr := c.StopPollForRoute(ctx, msg.StopPollID, telegramPollRoute{\n\t\t\tAccount: account, ChatID: chatID, ThreadID: threadID,\n\t\t\tAgentID: msg.AgentID, SessionKey: msg.SessionKey, SenderID: msg.Context.SenderID,\n\t\t})\n''')

    # Wire safe semantic native media through the existing manager and Telegram adapter.
    replace_once(
        "pkg/channels/telegram/live_photo_delivery.go",
        '''\tpayload, ok := bus.DecodeLivePhotoMediaRef(msg.Parts[0].Ref)\n\tif !ok {\n\t\treturn nil, false, nil\n\t}\n\tids, err := c.sendLivePhoto(ctx, msg, payload)\n\treturn ids, true, err\n''',
        '''\tif payload, ok := bus.DecodeLivePhotoMediaRef(msg.Parts[0].Ref); ok {\n\t\tids, err := c.sendLivePhoto(ctx, msg, payload)\n\t\treturn ids, true, err\n\t}\n\tif payload, ok := bus.DecodeNativeSingleMediaRef(msg.Parts[0].Ref); ok {\n\t\tids, err := c.sendNativeSingleMedia(ctx, msg, payload)\n\t\treturn ids, true, err\n\t}\n\treturn nil, false, nil\n''')
    replace_once(
        "pkg/channels/telegram/telegram.go",
        '''\t\tif err != nil {\n\t\t\tif ephemeralTarget != nil {\n''',
        '''\t\tif err != nil {\n\t\t\tif feature, native := telegramMediaCapability(part.Type); native {\n\t\t\t\taccount := strings.TrimSpace(msg.Context.Account)\n\t\t\t\tif account == "" {\n\t\t\t\t\taccount = c.Name()\n\t\t\t\t}\n\t\t\t\tserverID := ""\n\t\t\t\tif c.tgCfg != nil {\n\t\t\t\t\tserverID = c.tgCfg.BaseURL\n\t\t\t\t}\n\t\t\t\tif capability.GlobalNegativeCache.RecordFailure("telegram", account, serverID, feature, err) {\n\t\t\t\t\treturn nil, fmt.Errorf("native %s is unsupported by this Telegram server: %v: %w", part.Type, err, channels.ErrSendFailed)\n\t\t\t\t}\n\t\t\t}\n\t\t\tif ephemeralTarget != nil {\n''')

    # Register and route-filter the newly reachable semantic tools.
    replace_once(
        "pkg/agent/instance.go",
        '''\tif cfg.Tools.IsToolEnabled("send_live_photo") {\n\t\ttoolsRegistry.Register(tools.NewSendLivePhotoTool())\n\t}\n''',
        '''\tif cfg.Tools.IsToolEnabled("send_live_photo") {\n\t\ttoolsRegistry.Register(tools.NewSendLivePhotoTool())\n\t}\n\tif cfg.Tools.IsToolEnabled("send_animation") {\n\t\ttoolsRegistry.Register(tools.NewSendAnimationTool())\n\t}\n\tif cfg.Tools.IsToolEnabled("send_sticker") {\n\t\ttoolsRegistry.Register(tools.NewSendStickerTool())\n\t}\n\tif cfg.Tools.IsToolEnabled("send_video_note") {\n\t\ttoolsRegistry.Register(tools.NewSendVideoNoteTool())\n\t}\n''')
    replace_once(
        "pkg/agent/pipeline_llm.go",
        '''\tcase "send_live_photo":\n\t\treturn capSet.IsSupported(capability.FeatureMediaLivePhoto)\n\tdefault:\n''',
        '''\tcase "send_live_photo":\n\t\treturn capSet.IsSupported(capability.FeatureMediaLivePhoto)\n\tcase "send_animation":\n\t\treturn capSet.IsSupported(capability.FeatureMediaAnimation)\n\tcase "send_sticker":\n\t\treturn capSet.IsSupported(capability.FeatureMediaSticker)\n\tcase "send_video_note":\n\t\treturn capSet.IsSupported(capability.FeatureMediaVideoNote)\n\tdefault:\n''')

    write_native_payload()
    write_native_tools()
    write_native_telegram_delivery()
    write_tests()

    # Extend route tests for all safe native single-media actions.
    with (REPO / "pkg/agent/capability_route_test.go").open("a") as f:
        f.write(r'''

func TestRouteAwareToolAvailability_NativeSingleMedia(t *testing.T) {
    al := &AgentLoop{cfg: config.DefaultConfig()}
    telegram := &turnState{channel: "telegram", chatID: "123", opts: processOptions{Dispatch: DispatchRequest{
        InboundContext: &bus.InboundContext{Channel: "telegram", Account: "default", ChatID: "123", SenderID: "u1"},
    }}}
    for _, name := range []string{"send_animation", "send_sticker", "send_video_note"} {
        if !isToolAllowedByRoute(al, telegram, name) {
            t.Fatalf("%s should be available on Telegram", name)
        }
    }
    pico := &turnState{channel: "pico", chatID: "p1", opts: processOptions{Dispatch: DispatchRequest{
        InboundContext: &bus.InboundContext{Channel: "pico", Account: "default", ChatID: "p1", SenderID: "u1"},
    }}}
    for _, name := range []string{"send_animation", "send_sticker", "send_video_note"} {
        if isToolAllowedByRoute(al, pico, name) {
            t.Fatalf("%s leaked to non-Telegram route", name)
        }
    }
}
''')

    # Production-dispatch regression: the stricter route function must accept only
    # the token bound to the current trusted route.
    with (REPO / "pkg/channels/telegram/poll_hardening_test.go").open("a") as f:
        f.write(r'''

func TestStopPollForRouteAcceptsBoundTokenAndRejectsWrongRoute(t *testing.T) {
    ch := newTestChannel(t, &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
        require.Contains(t, url, "stopPoll")
        return successResponse(t), nil
    }})
    entry := telegramPollEntry{
        LocalHandle: "route-bound", Account: ch.Name(), ChatID: -1001, ThreadID: 42,
        MessageID: 7, AgentID: "main", SenderID: "alice", SessionKey: "session-a",
    }
    ch.registerPollEntry(entry)
    token := bus.NewPollStopRouteToken(entry.LocalHandle, entry.Account, "-1001/42", "42", entry.AgentID, "", entry.SessionKey)
    wrong := telegramPollRoute{Account: entry.Account, ChatID: -1002, ThreadID: 42, AgentID: "main", SenderID: "alice", SessionKey: "session-a"}
    require.Error(t, ch.StopPollForRoute(context.Background(), token, wrong))
    if _, ok := ch.resolvePollByLocalHandle(entry.LocalHandle); !ok {
        t.Fatal("failed authorization consumed poll state")
    }
    good := telegramPollRoute{Account: entry.Account, ChatID: -1001, ThreadID: 42, AgentID: "main", SenderID: "alice", SessionKey: "session-a"}
    require.NoError(t, ch.StopPollForRoute(context.Background(), token, good))
    if _, ok := ch.resolvePollByLocalHandle(entry.LocalHandle); ok {
        t.Fatal("successful stop retained poll state")
    }
}
''')


def cleanup_helpers() -> None:
    for path in [
        ".github/workflows/pr16-final-hardening-runner.yml",
        ".github/scripts/pr16_hardening_runner.py",
    ]:
        p = REPO / path
        if p.exists():
            p.unlink()


def main() -> None:
    apply_historical_base_patch()
    apply_additional_hardening()
    run("gofmt", "-w",
        "pkg/bus/native_single_media.go",
        "pkg/bus/native_single_media_test.go",
        "pkg/tools/native_single_media.go",
        "pkg/tools/native_single_media_test.go",
        "pkg/channels/manager.go",
        "pkg/channels/manager_native_hardening_test.go",
        "pkg/channels/telegram/live_photo_delivery.go",
        "pkg/channels/telegram/native_single_media_delivery.go",
        "pkg/channels/telegram/native_single_media_delivery_test.go",
        "pkg/channels/telegram/poll_registry.go",
        "pkg/channels/telegram/poll_hardening_test.go",
        "pkg/channels/telegram/telegram.go",
        "pkg/agent/instance.go",
        "pkg/agent/pipeline_llm.go",
        "pkg/agent/capability_route_test.go",
    )
    cleanup_helpers()


if __name__ == "__main__":
    main()
