package telegram

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/capability"
	"github.com/As-tsaqib/picoclaw/pkg/channels"
	"github.com/As-tsaqib/picoclaw/pkg/media"
)

const (
	telegramLivePhotoMaxBytes        int64 = 10 * 1024 * 1024
	telegramLivePhotoCaptionMaxChars       = 1024
)

// SendSemanticMedia recognizes the channel-neutral live-photo envelope. Other
// media is delegated to the existing SendMedia path by returning handled=false.
func (c *TelegramChannel) SendSemanticMedia(
	ctx context.Context,
	msg bus.OutboundMediaMessage,
) ([]string, bool, error) {
	if len(msg.Parts) != 1 {
		return nil, false, nil
	}
	if payload, ok := bus.DecodeLivePhotoMediaRef(msg.Parts[0].Ref); ok {
		ids, err := c.sendLivePhoto(ctx, msg, payload)
		return ids, true, err
	}
	if payload, ok := bus.DecodeNativeSingleMediaRef(msg.Parts[0].Ref); ok {
		ids, err := c.sendNativeSingleMedia(ctx, msg, payload)
		return ids, true, err
	}
	return nil, false, nil
}

func (c *TelegramChannel) sendLivePhoto(
	ctx context.Context,
	msg bus.OutboundMediaMessage,
	payload bus.LivePhotoPayload,
) ([]string, error) {
	if !c.IsRunning() {
		return nil, channels.ErrNotRunning
	}
	if utf8.RuneCountInString(payload.Caption) > telegramLivePhotoCaptionMaxChars {
		return nil, fmt.Errorf(
			"live photo caption exceeds %d characters: %w",
			telegramLivePhotoCaptionMaxChars,
			channels.ErrSendFailed,
		)
	}

	chatID, threadID, err := resolveTelegramOutboundTarget(msg.ChatID, &msg.Context)
	if err != nil {
		return nil, fmt.Errorf("invalid live photo target: %w", channels.ErrSendFailed)
	}
	ephemeralTarget, err := c.resolveEphemeralTarget(msg.Context, msg.Scope, msg.SessionKey, chatID, threadID)
	if err != nil {
		return nil, ephemeralDeliveryError("live photo route resolution", err)
	}
	store := c.GetMediaStore()
	if store == nil {
		return nil, fmt.Errorf("no media store available: %w", channels.ErrSendFailed)
	}

	photoPath, photoMeta, err := store.ResolveWithMeta(payload.PhotoRef)
	if err != nil {
		return nil, fmt.Errorf("resolve live photo static image: %w", channels.ErrSendFailed)
	}
	videoPath, videoMeta, err := store.ResolveWithMeta(payload.LiveVideoRef)
	if err != nil {
		return nil, fmt.Errorf("resolve live photo video: %w", channels.ErrSendFailed)
	}
	if !livePhotoStaticImageShape(photoPath, photoMeta) {
		return nil, fmt.Errorf("live photo photo_ref is not an image: %w", channels.ErrSendFailed)
	}
	if !livePhotoVideoShape(videoPath, videoMeta) {
		return nil, fmt.Errorf("live photo live_video_ref is not a video: %w", channels.ErrSendFailed)
	}
	if sizeErr := enforceLivePhotoFileSize(photoPath, "photo"); sizeErr != nil {
		return nil, sizeErr
	}
	if sizeErr := enforceLivePhotoFileSize(videoPath, "live video"); sizeErr != nil {
		return nil, sizeErr
	}

	photoFile, err := os.Open(photoPath)
	if err != nil {
		return nil, fmt.Errorf("open live photo static image: %w", channels.ErrSendFailed)
	}
	defer photoFile.Close()
	videoFile, err := os.Open(videoPath)
	if err != nil {
		return nil, fmt.Errorf("open live photo video: %w", channels.ErrSendFailed)
	}
	defer videoFile.Close()

	params := &telego.SendLivePhotoParams{
		ChatID:          tu.ID(chatID),
		MessageThreadID: threadID,
		ReceiverUserID:  ephemeralReceiverUserID(ephemeralTarget),
		CallbackQueryID: ephemeralCallbackQueryID(ephemeralTarget),
		Photo:           telego.InputFile{File: photoFile},
		LivePhoto:       telego.InputFile{File: videoFile},
		Caption:         payload.Caption,
		ReplyParameters: ephemeralReplyParameters(ephemeralTarget, msg.Context.ReplyToMessageID),
	}
	result, sendErr := c.bot.SendLivePhoto(ctx, params)
	if sendErr != nil {
		account := strings.TrimSpace(msg.Context.Account)
		if account == "" {
			account = c.Name()
		}
		serverID := ""
		if c.tgCfg != nil {
			serverID = c.tgCfg.BaseURL
		}
		if capability.GlobalNegativeCache.RecordFailure(
			"telegram", account, serverID, capability.FeatureMediaLivePhoto, sendErr,
		) {
			return nil, fmt.Errorf(
				"native live photo is unsupported by this Telegram server: %v: %w",
				sendErr,
				channels.ErrSendFailed,
			)
		}
		return nil, fmt.Errorf("telegram send live photo: %v: %w", sendErr, channels.ErrTemporary)
	}
	messageID, resultErr := validateEphemeralSendResult(result, ephemeralTarget)
	if resultErr != nil {
		return nil, resultErr
	}
	return []string{messageID}, nil
}

func enforceLivePhotoFileSize(path, role string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat live photo %s: %w", role, channels.ErrSendFailed)
	}
	if info.Size() <= 0 || info.Size() > telegramLivePhotoMaxBytes {
		return fmt.Errorf(
			"live photo %s must be 1-%d bytes, got %d: %w",
			role,
			telegramLivePhotoMaxBytes,
			info.Size(),
			channels.ErrSendFailed,
		)
	}
	return nil
}

func livePhotoStaticImageShape(path string, meta media.MediaMeta) bool {
	contentType := strings.ToLower(strings.TrimSpace(meta.ContentType))
	if contentType != "" {
		return strings.HasPrefix(contentType, "image/")
	}
	if filename := strings.TrimSpace(meta.Filename); filename != "" {
		path = filename
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}

func livePhotoVideoShape(path string, meta media.MediaMeta) bool {
	contentType := strings.ToLower(strings.TrimSpace(meta.ContentType))
	if contentType != "" {
		return strings.HasPrefix(contentType, "video/")
	}
	if filename := strings.TrimSpace(meta.Filename); filename != "" {
		path = filename
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".mov", ".m4v":
		return true
	default:
		return false
	}
}
