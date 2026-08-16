package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/logger"
)

const sessionDashboardConfigBodyLimit = 64 << 10

var sessionDashboardGatewayReloader = func(h *Handler) error {
	return h.restartGatewayForDashboardConfig()
}

func (h *Handler) registerSessionDashboardRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/dashboard/superadmin", h.handleGetSessionSuperadmin)
	mux.HandleFunc("PUT /api/dashboard/superadmin", h.handlePutSessionSuperadmin)
	mux.HandleFunc("DELETE /api/dashboard/superadmin", h.handleDeleteSessionSuperadmin)
}

func (h *Handler) handleGetSessionSuperadmin(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, "Failed to load config", http.StatusInternalServerError)
		return
	}
	writeSessionSuperadminResponse(w, cfg.Dashboard.Superadmin)
}

func (h *Handler) handlePutSessionSuperadmin(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, sessionDashboardConfigBodyLimit))
	decoder.DisallowUnknownFields()
	var requested config.SessionSuperadminConfig
	if err := decoder.Decode(&requested); err != nil {
		http.Error(w, fmt.Sprintf("Invalid superadmin config: %v", err), http.StatusBadRequest)
		return
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		http.Error(w, "Invalid superadmin config", http.StatusBadRequest)
		return
	}
	requested.TelegramUserID = strings.TrimSpace(requested.TelegramUserID)
	requested.BotAccount = strings.TrimSpace(requested.BotAccount)
	requested.AgentID = strings.TrimSpace(requested.AgentID)
	dashboard := config.DashboardConfig{Superadmin: requested}
	if err := dashboard.Validate(); err != nil {
		writeConfigValidationError(w, err.Error())
		return
	}

	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, "Failed to load config", http.StatusInternalServerError)
		return
	}
	// Replacement is atomic and singular on disk. If the running gateway needs
	// a restart, the API does not report success until that reload succeeds.
	cfg.Dashboard = dashboard
	if err := config.SaveConfig(h.configPath, cfg); err != nil {
		http.Error(w, "Failed to save superadmin config", http.StatusInternalServerError)
		return
	}
	if err := sessionDashboardGatewayReloader(h); err != nil {
		logger.ErrorF(
			"failed to apply session dashboard config to running gateway",
			map[string]any{"error": err.Error()},
		)
		http.Error(
			w,
			"Superadmin config was saved but could not be applied to the running gateway",
			http.StatusInternalServerError,
		)
		return
	}
	writeSessionSuperadminResponse(w, requested)
}

func (h *Handler) handleDeleteSessionSuperadmin(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, "Failed to load config", http.StatusInternalServerError)
		return
	}
	cfg.Dashboard.Superadmin = config.SessionSuperadminConfig{}
	if err := config.SaveConfig(h.configPath, cfg); err != nil {
		http.Error(w, "Failed to delete superadmin config", http.StatusInternalServerError)
		return
	}
	if err := sessionDashboardGatewayReloader(h); err != nil {
		logger.ErrorF(
			"failed to apply session dashboard revocation to running gateway",
			map[string]any{"error": err.Error()},
		)
		http.Error(
			w,
			"Superadmin revocation was saved but could not be applied to the running gateway",
			http.StatusInternalServerError,
		)
		return
	}
	writeSessionSuperadminResponse(w, config.SessionSuperadminConfig{})
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func (h *Handler) restartGatewayForDashboardConfig() error {
	status := h.gatewayStatusData()
	gatewayStatus, _ := status["gateway_status"].(string)
	if gatewayStatus != "running" {
		return nil
	}
	_, err := h.RestartGateway()
	return err
}

func writeSessionSuperadminResponse(w http.ResponseWriter, superadmin config.SessionSuperadminConfig) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "ok",
		"superadmin": superadmin,
	})
}
