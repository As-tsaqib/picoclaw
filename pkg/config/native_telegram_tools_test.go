package config

import "testing"

func TestDefaultConfigEnablesSafeTelegramSemanticTools(t *testing.T) {
	cfg := DefaultConfig()
	tools := []string{
		"send_animation", "send_sticker", "send_video_note", "send_live_photo",
		"send_location", "send_contact", "send_dice",
	}
	for _, name := range tools {
		if !cfg.Tools.IsToolEnabled(name) {
			t.Fatalf("%s should default enabled", name)
		}
	}
	cfg.Tools.SendLivePhoto.Enabled = false
	if cfg.Tools.IsToolEnabled("send_live_photo") {
		t.Fatal("send_live_photo policy toggle was ignored")
	}
}
