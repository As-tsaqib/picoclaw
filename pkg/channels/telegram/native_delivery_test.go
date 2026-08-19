package telegram

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/media"
)

func TestTelegramChannel_SendLocationAndVenue(t *testing.T) {
	var sentLocation telego.SendLocationParams
	var sentVenue telego.SendVenueParams

	ch := newTestPollChannel(t, func(ctx context.Context, url string, data *ta.RequestData) (*ta.Response, error) {
		body := readRequestBody(data)
		if strings.Contains(url, "sendLocation") {
			_ = json.Unmarshal(body, &sentLocation)
			msg := telego.Message{MessageID: 301}
			b, _ := json.Marshal(msg)
			return &ta.Response{Ok: true, Result: b}, nil
		}
		if strings.Contains(url, "sendVenue") {
			_ = json.Unmarshal(body, &sentVenue)
			msg := telego.Message{MessageID: 302}
			b, _ := json.Marshal(msg)
			return &ta.Response{Ok: true, Result: b}, nil
		}
		return &ta.Response{Ok: true, Result: []byte("{}")}, nil
	})

	// Test SendLocation
	msgIDs, err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID: "12345",
		Location: &bus.LocationPayload{
			Latitude:  -6.2088,
			Longitude: 106.8456,
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"301"}, msgIDs)
	assert.Equal(t, -6.2088, sentLocation.Latitude)
	assert.Equal(t, 106.8456, sentLocation.Longitude)

	// Test SendVenue
	msgIDs, err = ch.Send(context.Background(), bus.OutboundMessage{
		ChatID: "12345",
		Location: &bus.LocationPayload{
			Latitude:  -6.1754,
			Longitude: 106.8272,
			Title:     "Monas",
			Address:   "Gambir, Central Jakarta",
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"302"}, msgIDs)
	assert.Equal(t, "Monas", sentVenue.Title)
	assert.Equal(t, "Gambir, Central Jakarta", sentVenue.Address)
}

func TestTelegramChannel_SendContact(t *testing.T) {
	var sentContact telego.SendContactParams

	ch := newTestPollChannel(t, func(ctx context.Context, url string, data *ta.RequestData) (*ta.Response, error) {
		body := readRequestBody(data)
		if strings.Contains(url, "sendContact") {
			_ = json.Unmarshal(body, &sentContact)
			msg := telego.Message{MessageID: 401}
			b, _ := json.Marshal(msg)
			return &ta.Response{Ok: true, Result: b}, nil
		}
		return &ta.Response{Ok: true, Result: []byte("{}")}, nil
	})

	msgIDs, err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID: "12345",
		Contact: &bus.ContactPayload{
			PhoneNumber: "+628123456789",
			FirstName:   "Alice",
			LastName:    "Smith",
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"401"}, msgIDs)
	assert.Equal(t, "+628123456789", sentContact.PhoneNumber)
	assert.Equal(t, "Alice", sentContact.FirstName)
	assert.Equal(t, "Smith", sentContact.LastName)
}

func TestTelegramChannel_SendDice(t *testing.T) {
	var sentDice telego.SendDiceParams

	ch := newTestPollChannel(t, func(ctx context.Context, url string, data *ta.RequestData) (*ta.Response, error) {
		body := readRequestBody(data)
		if strings.Contains(url, "sendDice") {
			_ = json.Unmarshal(body, &sentDice)
			msg := telego.Message{MessageID: 501}
			b, _ := json.Marshal(msg)
			return &ta.Response{Ok: true, Result: b}, nil
		}
		return &ta.Response{Ok: true, Result: []byte("{}")}, nil
	})

	msgIDs, err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID: "12345",
		Dice:   &bus.DicePayload{Emoji: "🎯"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"501"}, msgIDs)
	assert.Equal(t, "🎯", sentDice.Emoji)

	// Test fallback default emoji for unknown dice
	_, err = ch.Send(context.Background(), bus.OutboundMessage{
		ChatID: "12345",
		Dice:   &bus.DicePayload{Emoji: "unknown"},
	})
	require.NoError(t, err)
	assert.Equal(t, "🎲", sentDice.Emoji)
}

func TestTelegramChannel_SendMedia_AnimationStickerVideoNote(t *testing.T) {
	calledMethods := make(map[string]bool)

	store := media.NewFileMediaStore()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.dat")
	err := os.WriteFile(filePath, []byte("fake binary data"), 0o600)
	require.NoError(t, err)

	ref, err := store.Store(
		filePath,
		media.MediaMeta{Filename: "sample.dat", ContentType: "application/octet-stream"},
		"scope-1",
	)
	require.NoError(t, err)

	ch := newTestChannelWithConstructor(t, &stubCaller{
		callFn: func(ctx context.Context, url string, data *ta.RequestData) (*ta.Response, error) {
			for _, m := range []string{"sendAnimation", "sendSticker", "sendVideoNote"} {
				if strings.Contains(url, m) {
					calledMethods[m] = true
					msg := telego.Message{MessageID: 601}
					b, _ := json.Marshal(msg)
					return &ta.Response{Ok: true, Result: b}, nil
				}
			}
			return &ta.Response{Ok: true, Result: []byte("{}")}, nil
		},
	}, &multipartRecordingConstructor{})
	ch.SetMediaStore(store)

	for _, mediaType := range []string{"animation", "sticker", "video_note"} {
		msgIDs, err := ch.SendMedia(context.Background(), bus.OutboundMediaMessage{
			ChatID: "12345",
			Parts: []bus.MediaPart{
				{Type: mediaType, Ref: ref},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"601"}, msgIDs)
	}

	assert.True(t, calledMethods["sendAnimation"])
	assert.True(t, calledMethods["sendSticker"])
	assert.True(t, calledMethods["sendVideoNote"])
}
