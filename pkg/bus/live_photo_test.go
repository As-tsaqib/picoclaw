package bus

import "testing"

func TestLivePhotoMediaRefRoundTrip(t *testing.T) {
	payload := LivePhotoPayload{
		PhotoRef: "media://photo-1", LiveVideoRef: "media://video-1", Caption: "caption",
	}
	encoded, ok := EncodeLivePhotoMediaRef(payload)
	if !ok || encoded == "" {
		t.Fatal("valid live photo payload was not encoded")
	}
	decoded, ok := DecodeLivePhotoMediaRef(encoded)
	if !ok || decoded != payload {
		t.Fatalf("decoded payload = %#v ok=%t, want %#v", decoded, ok, payload)
	}
}

func TestLivePhotoMediaRefRejectsInvalidShape(t *testing.T) {
	invalid := []LivePhotoPayload{
		{},
		{PhotoRef: "media://photo"},
		{LiveVideoRef: "media://video"},
		{PhotoRef: "media://same", LiveVideoRef: "media://same"},
	}
	for _, payload := range invalid {
		if encoded, ok := EncodeLivePhotoMediaRef(payload); ok || encoded != "" {
			t.Fatalf("invalid payload encoded: %#v -> %q", payload, encoded)
		}
	}
	for _, value := range []string{"", "media://ordinary", "picoclaw-live-photo:v1:not-base64"} {
		if _, ok := DecodeLivePhotoMediaRef(value); ok {
			t.Fatalf("invalid envelope decoded: %q", value)
		}
	}
}
