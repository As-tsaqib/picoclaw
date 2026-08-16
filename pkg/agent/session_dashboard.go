package agent

import (
	"strconv"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func (al *AgentLoop) telegramSessionDashboard(
	inbound *bus.InboundContext,
	agentID string,
	defaultKey string,
	defaultScope *session.SessionScope,
	defaultAliases []string,
) (session.DashboardMode, session.DashboardQuery, bool) {
	if inbound == nil || !isTelegramPrivateDashboardInbound(*inbound) {
		return session.DashboardModeRoute, session.DashboardQuery{}, false
	}
	userID := strings.TrimSpace(inbound.SenderID)
	parsed, err := strconv.ParseInt(userID, 10, 64)
	if err != nil || parsed <= 0 {
		return session.DashboardModeRoute, session.DashboardQuery{}, false
	}
	userID = strconv.FormatInt(parsed, 10)
	botAccount := strings.TrimSpace(inbound.Channel)
	mode := session.DashboardModePersonal
	includeLegacyUnknown := false
	if cfg := al.GetConfig(); cfg != nil &&
		cfg.Dashboard.Superadmin.AllowsTelegramPrivate(userID, botAccount, agentID) {
		mode = session.DashboardModeSuperadmin
		includeLegacyUnknown = cfg.Dashboard.Superadmin.IncludeLegacyUnknown
	}
	query := session.DashboardQuery{
		Mode:                 mode,
		OwnerUserID:          userID,
		ChatID:               strings.TrimSpace(inbound.ChatID),
		BotAccount:           botAccount,
		Account:              strings.TrimSpace(inbound.Account),
		AgentID:              strings.TrimSpace(agentID),
		IncludeLegacyUnknown: includeLegacyUnknown,
		DefaultSessionKey:    strings.TrimSpace(defaultKey),
		DefaultScope:         session.CloneScope(defaultScope),
		DefaultAliases:       append([]string(nil), defaultAliases...),
	}
	return mode, query, true
}

func isTelegramPrivateDashboardInbound(inbound bus.InboundContext) bool {
	if !strings.EqualFold(strings.TrimSpace(inbound.ChatType), "direct") ||
		strings.TrimSpace(inbound.ChatID) == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(inbound.Raw["platform"]), "telegram") {
		return true
	}
	// Backward-compatible fallback for the default Telegram channel name.
	return strings.EqualFold(strings.TrimSpace(inbound.Channel), "telegram")
}
