package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTelegramEphemeralConfig_DefaultsToDisabledAndIsolated(t *testing.T) {
	cfg := DefaultConfig()
	channel := cfg.Channels.Get(ChannelTelegram)
	if channel == nil {
		t.Fatal("default configuration has no Telegram channel")
	}
	decoded, err := channel.GetDecoded()
	if err != nil {
		t.Fatalf("decode Telegram settings: %v", err)
	}
	settings, ok := decoded.(*TelegramSettings)
	if !ok {
		t.Fatalf("decoded Telegram settings type = %T", decoded)
	}
	if settings.Ephemeral.EffectiveMode() != TelegramEphemeralModeOff {
		t.Fatalf("default ephemeral mode = %q, want off", settings.Ephemeral.EffectiveMode())
	}
	if !settings.Ephemeral.PersonalSessionIsolationEnabled() {
		t.Fatal("personal session isolation should default to enabled")
	}
}

func TestTelegramEphemeralConfig_CommandAllowlist(t *testing.T) {
	settings := TelegramEphemeralConfig{
		Mode:     TelegramEphemeralModeCommands,
		Commands: []string{"/clear", "status"},
	}
	if !settings.IsCommandEphemeral("clear") || !settings.IsCommandEphemeral("/status") {
		t.Fatal("configured commands should be ephemeral")
	}
	if settings.IsCommandEphemeral("help") {
		t.Fatal("unlisted command should remain public in allowlist mode")
	}
}

func TestTelegramEphemeralConfig_ParsesValidSchema(t *testing.T) {
	channel := &Channel{
		Enabled: true,
		Type:    ChannelTelegram,
		Settings: RawNode(`{
			"ephemeral": {
				"mode": "commands",
				"commands": ["clear", "status"],
				"personal_session_isolation": true
			}
		}`),
	}
	requireNoError := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected configuration error: %v", err)
		}
	}
	requireNoError(InitChannelList(ChannelsConfig{"telegram": channel}))
	decoded, err := channel.GetDecoded()
	requireNoError(err)
	settings, ok := decoded.(*TelegramSettings)
	if !ok {
		t.Fatalf("decoded Telegram settings type = %T", decoded)
	}
	if settings.Ephemeral.EffectiveMode() != TelegramEphemeralModeCommands {
		t.Fatalf("parsed mode = %q", settings.Ephemeral.EffectiveMode())
	}
	if len(settings.Ephemeral.Commands) != 2 || !settings.Ephemeral.IsCommandEphemeral("status") {
		t.Fatalf("parsed commands = %#v", settings.Ephemeral.Commands)
	}
	if !settings.Ephemeral.PersonalSessionIsolationEnabled() {
		t.Fatal("parsed personal session isolation is disabled")
	}
}

func TestTelegramEphemeralConfig_Validation(t *testing.T) {
	tests := []struct {
		name     string
		settings TelegramSettings
		wantErr  string
	}{
		{
			name:     "invalid mode",
			settings: TelegramSettings{Ephemeral: TelegramEphemeralConfig{Mode: "sometimes"}},
			wantErr:  "ephemeral.mode",
		},
		{
			name: "invalid command",
			settings: TelegramSettings{
				Ephemeral: TelegramEphemeralConfig{
					Mode:     "commands",
					Commands: []string{"bad-command"},
				},
			},
			wantErr: "ephemeral.commands",
		},
		{
			name: "duplicate command",
			settings: TelegramSettings{
				Ephemeral: TelegramEphemeralConfig{
					Mode:     "commands",
					Commands: []string{"clear", "/clear"},
				},
			},
			wantErr: "duplicate",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{
				Enabled:  true,
				Type:     ChannelTelegram,
				Settings: mustRawTelegramSettings(t, tt.settings),
			}
			err := InitChannelList(ChannelsConfig{"telegram": channel})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("InitChannelList error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestTelegramEphemeralConfig_RejectsIsolationOptOutWhenEnabled(t *testing.T) {
	falseValue := false
	settings := TelegramSettings{Ephemeral: TelegramEphemeralConfig{
		Mode:                     TelegramEphemeralModeAll,
		PersonalSessionIsolation: &falseValue,
	}}
	channel := &Channel{
		Enabled:  true,
		Type:     ChannelTelegram,
		Settings: mustRawTelegramSettings(t, settings),
	}
	err := InitChannelList(ChannelsConfig{"telegram": channel})
	if err == nil || !strings.Contains(err.Error(), "personal_session_isolation") {
		t.Fatalf("InitChannelList error = %v, want personal_session_isolation rejection", err)
	}
}

func mustRawTelegramSettings(t *testing.T, settings TelegramSettings) RawNode {
	t.Helper()
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal Telegram settings: %v", err)
	}
	return RawNode(data)
}
