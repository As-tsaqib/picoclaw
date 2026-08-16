package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionSuperadminConfigIsSingularValidatedAndDurable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Dashboard.Superadmin = SessionSuperadminConfig{
		TelegramUserID: "123456",
		BotAccount:     "telegram",
		AgentID:        "main",
		Enabled:        true,
	}
	require.NoError(t, cfg.Dashboard.Validate())
	assert.True(t, cfg.Dashboard.Superadmin.AllowsTelegramPrivate("123456", "telegram", "main"))
	assert.False(t, cfg.Dashboard.Superadmin.AllowsTelegramPrivate("999", "telegram", "main"))
	assert.False(t, cfg.Dashboard.Superadmin.AllowsTelegramPrivate("123456", "other-bot", "main"))
	assert.False(t, cfg.Dashboard.Superadmin.AllowsTelegramPrivate("123456", "telegram", "other"))

	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, SaveConfig(path, cfg))
	reloaded, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, cfg.Dashboard.Superadmin, reloaded.Dashboard.Superadmin)

	// Replacing the singular record removes the old user's authorization.
	reloaded.Dashboard.Superadmin = SessionSuperadminConfig{
		TelegramUserID: "654321", BotAccount: "telegram", AgentID: "main", Enabled: true,
	}
	require.NoError(t, SaveConfig(path, reloaded))
	replaced, err := LoadConfig(path)
	require.NoError(t, err)
	assert.False(t, replaced.Dashboard.Superadmin.AllowsTelegramPrivate("123456", "telegram", "main"))
	assert.True(t, replaced.Dashboard.Superadmin.AllowsTelegramPrivate("654321", "telegram", "main"))
}

func TestSessionSuperadminValidationFailsClosed(t *testing.T) {
	for _, test := range []SessionSuperadminConfig{
		{TelegramUserID: "username", BotAccount: "telegram", AgentID: "main", Enabled: true},
		{TelegramUserID: "123", AgentID: "main", Enabled: true},
		{TelegramUserID: "123", BotAccount: "telegram", Enabled: true},
	} {
		cfg := DashboardConfig{Superadmin: test}
		assert.Error(t, cfg.Validate())
	}

	// Disabled or absent configuration never grants global access.
	disabled := SessionSuperadminConfig{TelegramUserID: "123", BotAccount: "telegram", AgentID: "main"}
	require.NoError(t, (DashboardConfig{Superadmin: disabled}).Validate())
	assert.False(t, disabled.AllowsTelegramPrivate("123", "telegram", "main"))
	assert.False(t, (SessionSuperadminConfig{}).AllowsTelegramPrivate("123", "telegram", "main"))
}
