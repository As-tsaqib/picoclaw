package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/config"
)

func TestSessionSuperadminMutationReportsGatewayReloadFailure(t *testing.T) {
	configPath, cleanup := setupOAuthTestEnv(t)
	defer cleanup()
	h := NewHandler(configPath)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	previousReloader := sessionDashboardGatewayReloader
	sessionDashboardGatewayReloader = func(*Handler) error {
		return errors.New("simulated gateway restart failure")
	}
	defer func() { sessionDashboardGatewayReloader = previousReloader }()

	body, err := json.Marshal(config.SessionSuperadminConfig{
		TelegramUserID: "123",
		BotAccount:     "telegram",
		AgentID:        "main",
		Enabled:        true,
	})
	require.NoError(t, err)
	putReq := httptest.NewRequest(http.MethodPut, "/api/dashboard/superadmin", bytes.NewReader(body))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	mux.ServeHTTP(putRec, putReq)
	assert.Equal(t, http.StatusInternalServerError, putRec.Code)
	assert.Contains(t, putRec.Body.String(), "could not be applied")

	cfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)
	assert.True(t, cfg.Dashboard.Superadmin.AllowsTelegramPrivate("123", "telegram", "main"),
		"requested config remains durably saved even when runtime reload fails")

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/dashboard/superadmin", nil)
	deleteRec := httptest.NewRecorder()
	mux.ServeHTTP(deleteRec, deleteReq)
	assert.Equal(t, http.StatusInternalServerError, deleteRec.Code)
	assert.Contains(t, deleteRec.Body.String(), "could not be applied")

	cfg, err = config.LoadConfig(configPath)
	require.NoError(t, err)
	assert.False(t, cfg.Dashboard.Superadmin.Enabled)
	assert.Empty(t, cfg.Dashboard.Superadmin.TelegramUserID,
		"revocation must remain durable even when the caller is told runtime reload failed")
}
