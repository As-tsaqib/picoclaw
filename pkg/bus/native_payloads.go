package bus

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

const livePhotoMediaRefPrefix = "picoclaw-live-photo:v1:"

// LocationPayload represents geographical point or venue data.
type LocationPayload struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Title     string  `json:"title,omitempty"`   // If non-empty, sent as venue
	Address   string  `json:"address,omitempty"` // For venue
}

// ContactPayload represents a contact card.
type ContactPayload struct {
	PhoneNumber string `json:"phone_number"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name,omitempty"`
	VCard       string `json:"vcard,omitempty"`
}

// DicePayload represents an animated dice throw.
type DicePayload struct {
	Emoji string `json:"emoji,omitempty"` // 🎲, 🎯, 🏀, ⚽, 🎳, 🎰
}

// LivePhotoPayload explicitly pairs the static photo and its live-photo video.
// The refs remain MediaStore capabilities; adapters must resolve them through
// the same media policy used for ordinary attachments before opening files.
type LivePhotoPayload struct {
	PhotoRef     string `json:"photo_ref"`
	LiveVideoRef string `json:"live_video_ref"`
	Caption      string `json:"caption,omitempty"`
}

// EncodeLivePhotoMediaRef carries a channel-neutral live-photo action through
// the existing media bus without teaching generic channels Telegram SDK types.
// It is process-internal and contains only already-authorized MediaStore refs.
func EncodeLivePhotoMediaRef(payload LivePhotoPayload) (string, bool) {
	payload.PhotoRef = strings.TrimSpace(payload.PhotoRef)
	payload.LiveVideoRef = strings.TrimSpace(payload.LiveVideoRef)
	payload.Caption = strings.TrimSpace(payload.Caption)
	if payload.PhotoRef == "" || payload.LiveVideoRef == "" || payload.PhotoRef == payload.LiveVideoRef {
		return "", false
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	return livePhotoMediaRefPrefix + base64.RawURLEncoding.EncodeToString(data), true
}

// DecodeLivePhotoMediaRef recognizes only the exact internal format and fails
// closed for malformed/oversize values.
func DecodeLivePhotoMediaRef(value string) (LivePhotoPayload, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, livePhotoMediaRefPrefix) || len(value) > 16*1024 {
		return LivePhotoPayload{}, false
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, livePhotoMediaRefPrefix))
	if err != nil {
		return LivePhotoPayload{}, false
	}
	var payload LivePhotoPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return LivePhotoPayload{}, false
	}
	payload.PhotoRef = strings.TrimSpace(payload.PhotoRef)
	payload.LiveVideoRef = strings.TrimSpace(payload.LiveVideoRef)
	payload.Caption = strings.TrimSpace(payload.Caption)
	if payload.PhotoRef == "" || payload.LiveVideoRef == "" || payload.PhotoRef == payload.LiveVideoRef {
		return LivePhotoPayload{}, false
	}
	return payload, true
}
