package bus

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
	if _, ok := EncodeNativeSingleMediaRef(
		NativeSingleMediaPayload{Kind: NativeSingleMediaAnimation, Ref: "https://example.com/a.gif"},
	); ok {
		t.Fatal("URL escaped MediaStore boundary")
	}
	if _, ok := EncodeNativeSingleMediaRef(
		NativeSingleMediaPayload{Kind: NativeSingleMediaSticker, Ref: "media://s", Caption: "not allowed"},
	); ok {
		t.Fatal("sticker caption unexpectedly accepted")
	}
}
