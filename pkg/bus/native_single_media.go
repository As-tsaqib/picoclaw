package bus

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
