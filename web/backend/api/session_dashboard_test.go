package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/config"
)

func TestSessionSuperadminEndpointReplaceAndDeletePersist(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()
	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	put := func(userID string) {
		t.Helper()
		body, err := json.Marshal(config.SessionSuperadminConfig{
			TelegramUserID: userID,
			BotAccount:     "telegram",
			AgentID:        "main",
			Enabled:        true,
		})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPut, "/api/dashboard/superadmin", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}

	put("123")
	cfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)
	assert.True(t, cfg.Dashboard.Superadmin.AllowsTelegramPrivate("123", "telegram", "main"))

	put("456")
	cfg, err = config.LoadConfig(configPath)
	require.NoError(t, err)
	assert.False(t, cfg.Dashboard.Superadmin.AllowsTelegramPrivate("123", "telegram", "main"))
	assert.True(t, cfg.Dashboard.Superadmin.AllowsTelegramPrivate("456", "telegram", "main"))

	req := httptest.NewRequest(http.MethodDelete, "/api/dashboard/superadmin", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	cfg, err = config.LoadConfig(configPath)
	require.NoError(t, err)
	assert.False(t, cfg.Dashboard.Superadmin.Enabled)
	assert.Empty(t, cfg.Dashboard.Superadmin.TelegramUserID)
}

func TestSessionSuperadminEndpointRejectsUnsafeConfig(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()
	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/dashboard/superadmin", bytes.NewBufferString(`{
		"telegram_user_id":"@admin",
		"bot_account":"telegram",
		"agent_id":"main",
		"enabled":true
	}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "numeric Telegram user ID")
}
