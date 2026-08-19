package agent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/capability"
	"github.com/As-tsaqib/picoclaw/pkg/config"
)

func TestRouteAwareToolAvailability_FilteringAndGuard(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.SendPoll.Enabled = true
	cfg.Tools.SendQuiz.Enabled = true
	cfg.Tools.StopPoll.Enabled = true

	al := &AgentLoop{
		cfg: cfg,
	}

	tsTelegram := &turnState{
		channel: "telegram",
		chatID:  "12345",
		opts: processOptions{
			Dispatch: DispatchRequest{
				InboundContext: &bus.InboundContext{
					Channel:  "telegram",
					ChatID:   "12345",
					SenderID: "user-1",
				},
			},
		},
	}

	// On Telegram, send_poll and send_quiz are supported by default
	assert.True(t, isToolAllowedByRoute(al, tsTelegram, "send_poll"))
	assert.True(t, isToolAllowedByRoute(al, tsTelegram, "send_quiz"))
	assert.True(t, isToolAllowedByRoute(al, tsTelegram, "stop_poll"))

	// On a non-Telegram channel (e.g. pico), native poll/quiz are unsupported
	tsPico := &turnState{
		channel: "pico",
		chatID:  "chat-1",
		opts: processOptions{
			Dispatch: DispatchRequest{
				InboundContext: &bus.InboundContext{
					Channel:  "pico",
					ChatID:   "chat-1",
					SenderID: "user-1",
				},
			},
		},
	}

	assert.False(t, isToolAllowedByRoute(al, tsPico, "send_poll"))
	assert.False(t, isToolAllowedByRoute(al, tsPico, "send_quiz"))
	assert.False(t, isToolAllowedByRoute(al, tsPico, "stop_poll"))

	// When downgraded by negative cache on telegram custom server
	tsCustom := &turnState{
		channel: "telegram",
		chatID:  "12345",
		opts: processOptions{
			Dispatch: DispatchRequest{
				InboundContext: &bus.InboundContext{
					Channel:  "telegram",
					Account:  "custom-acc",
					ChatID:   "12345",
					SenderID: "user-1",
					Raw:      map[string]string{"server_id": "http://legacy-tg:8081"},
				},
			},
		},
	}

	capability.GlobalNegativeCache.RecordFailure(
		"telegram",
		"custom-acc",
		"http://legacy-tg:8081",
		capability.FeaturePollQuiz,
		errors.New("api: 400 Bad Request: METHOD_NOT_FOUND sendPoll"),
	)

	// Quiz should be blocked on the downgraded server route, but regular poll remains allowed
	assert.False(t, isToolAllowedByRoute(al, tsCustom, "send_quiz"))
	assert.True(t, isToolAllowedByRoute(al, tsCustom, "send_poll"))
}

func TestRouteAwareToolAvailability_LivePhoto(t *testing.T) {
	capability.GlobalNegativeCache.Clear()
	t.Cleanup(capability.GlobalNegativeCache.Clear)
	al := &AgentLoop{cfg: config.DefaultConfig()}
	telegram := &turnState{
		channel: "telegram",
		chatID:  "123",
		opts: processOptions{Dispatch: DispatchRequest{InboundContext: &bus.InboundContext{
			Channel: "telegram", Account: "default", ChatID: "123", SenderID: "u1",
		}}},
	}
	if !isToolAllowedByRoute(al, telegram, "send_live_photo") {
		t.Fatal("send_live_photo should be available on a supported Telegram route")
	}
	pico := &turnState{channel: "pico", chatID: "p1", opts: processOptions{Dispatch: DispatchRequest{
		InboundContext: &bus.InboundContext{Channel: "pico", Account: "default", ChatID: "p1", SenderID: "u1"},
	}}}
	if isToolAllowedByRoute(al, pico, "send_live_photo") {
		t.Fatal("send_live_photo leaked to non-Telegram route")
	}
	legacy := &turnState{
		channel: "telegram", chatID: "123",
		opts: processOptions{Dispatch: DispatchRequest{InboundContext: &bus.InboundContext{
			Channel: "telegram", Account: "legacy", ChatID: "123", SenderID: "u1",
			Raw: map[string]string{"server_id": "http://legacy-live-photo"},
		}}},
	}
	capability.GlobalNegativeCache.RecordFailure(
		"telegram", "legacy", "http://legacy-live-photo", capability.FeatureMediaLivePhoto,
		errors.New("400 Bad Request: method not found"),
	)
	if isToolAllowedByRoute(al, legacy, "send_live_photo") {
		t.Fatal("send_live_photo should be filtered after server downgrade")
	}
}

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
