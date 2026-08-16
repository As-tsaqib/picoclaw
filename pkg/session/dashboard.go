package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/routing"
)

type DashboardMode string

const (
	DashboardModeRoute      DashboardMode = "route"
	DashboardModePersonal   DashboardMode = "personal"
	DashboardModeSuperadmin DashboardMode = "superadmin"
)

// DashboardQuery is built exclusively from trusted inbound Telegram metadata
// plus server-side configuration. Default* fields are used only to seed a
// brand-new private route and are excluded from the durable mapping identity.
type DashboardQuery struct {
	Mode                 DashboardMode
	OwnerUserID          string
	ChatID               string
	BotAccount           string
	Account              string
	AgentID              string
	IncludeLegacyUnknown bool
	DefaultSessionKey    string
	DefaultScope         *SessionScope
	DefaultAliases       []string
}

func (q DashboardQuery) valid() bool {
	if q.Mode != DashboardModePersonal && q.Mode != DashboardModeSuperadmin {
		return false
	}
	if strings.TrimSpace(q.OwnerUserID) == "" || strings.TrimSpace(q.ChatID) == "" ||
		strings.TrimSpace(q.BotAccount) == "" || strings.TrimSpace(q.AgentID) == "" {
		return false
	}
	return true
}

func dashboardSignature(q DashboardQuery) string {
	if !q.valid() {
		return ""
	}
	raw := strings.Join([]string{
		"v=1",
		"mode=" + string(q.Mode),
		"owner=" + strings.TrimSpace(q.OwnerUserID),
		"chat=" + strings.TrimSpace(q.ChatID),
		"bot=" + strings.ToLower(strings.TrimSpace(q.BotAccount)),
		"account=" + routing.NormalizeAccountID(q.Account),
		"agent=" + routing.NormalizeAgentID(q.AgentID),
	}, "|")
	sum := sha256.Sum256([]byte(raw))
	return "dashboard_v1_" + hex.EncodeToString(sum[:])
}

func (b *JSONLBackend) ListDashboardSessions(q DashboardQuery) ([]SessionRecord, error) {
	if !q.valid() {
		return nil, ErrSessionNotInScope
	}
	if _, err := b.catalogStore(); err != nil {
		return nil, err
	}
	b.ensureDashboardDefault(q)

	keys := b.ListSessions()
	records := make([]SessionRecord, 0, len(keys))
	for _, key := range keys {
		record, err := b.sessionRecord(key, nil, nil)
		if err != nil {
			continue
		}
		if record.Scope == nil {
			record.LegacyUnknown = true
		}
		if !dashboardRecordAllowed(record, q) {
			continue
		}
		records = append(records, record)
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].UpdatedAt.Equal(records[j].UpdatedAt) {
			return records[i].Key < records[j].Key
		}
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	return records, nil
}

func (b *JSONLBackend) ensureDashboardDefault(q DashboardQuery) {
	key := strings.TrimSpace(q.DefaultSessionKey)
	if key == "" || q.DefaultScope == nil {
		return
	}
	b.EnsureSessionMetadata(key, q.DefaultScope, q.DefaultAliases)
}

func (b *JSONLBackend) ActiveDashboardSession(q DashboardQuery) string {
	if !q.valid() {
		return ""
	}
	store, err := b.catalogStore()
	if err != nil {
		return ""
	}
	b.ensureDashboardDefault(q)
	signature := dashboardSignature(q)
	key, err := store.GetActiveSession(context.Background(), signature)
	if err == nil && strings.TrimSpace(key) != "" {
		record, recordErr := b.sessionRecord(strings.TrimSpace(key), nil, nil)
		if recordErr == nil && dashboardRecordAllowed(record, q) {
			return record.Key
		}
		_ = store.ClearActiveSession(context.Background(), signature)
	}
	if fallback := strings.TrimSpace(q.DefaultSessionKey); fallback != "" {
		record, recordErr := b.sessionRecord(fallback, q.DefaultScope, q.DefaultAliases)
		if recordErr == nil && dashboardRecordAllowed(record, q) {
			return record.Key
		}
	}
	return ""
}

func (b *JSONLBackend) SetActiveDashboardSession(q DashboardQuery, sessionKey string) error {
	if !q.valid() {
		return ErrSessionNotInScope
	}
	store, err := b.catalogStore()
	if err != nil {
		return err
	}
	record, err := b.sessionRecord(strings.TrimSpace(sessionKey), nil, nil)
	if err != nil || !dashboardRecordAllowed(record, q) {
		return ErrSessionNotInScope
	}
	return store.SetActiveSession(context.Background(), dashboardSignature(q), record.Key)
}

func (b *JSONLBackend) RenameDashboardSession(q DashboardQuery, sessionKey, name string) error {
	cleanName := SanitizeSessionName(name)
	if cleanName == "" {
		return ErrInvalidSessionName
	}
	store, err := b.catalogStore()
	if err != nil {
		return err
	}
	record, err := b.sessionRecord(strings.TrimSpace(sessionKey), nil, nil)
	if err != nil || !dashboardRecordAllowed(record, q) {
		return ErrSessionNotInScope
	}
	return store.SetSessionName(context.Background(), record.Key, cleanName, "custom", false)
}

func (b *JSONLBackend) ResolveDashboardSelector(q DashboardQuery, selector string) (SessionRecord, error) {
	records, err := b.ListDashboardSessions(q)
	if err != nil {
		return SessionRecord{}, err
	}
	selector = strings.TrimSpace(strings.ToLower(selector))
	if n, parseErr := strconv.Atoi(selector); parseErr == nil {
		if n > 0 && n <= len(records) {
			return records[n-1], nil
		}
		return SessionRecord{}, ErrSessionNotInScope
	}
	var matched *SessionRecord
	for i := range records {
		if !strings.EqualFold(records[i].ShortID, selector) {
			continue
		}
		if matched != nil {
			return SessionRecord{}, ErrAmbiguousSessionSelector
		}
		candidate := records[i]
		matched = &candidate
	}
	if matched == nil {
		return SessionRecord{}, ErrSessionNotInScope
	}
	return *matched, nil
}

func dashboardRecordAllowed(record SessionRecord, q DashboardQuery) bool {
	if !q.valid() {
		return false
	}
	if record.Scope == nil {
		return q.Mode == DashboardModeSuperadmin && q.IncludeLegacyUnknown && record.LegacyUnknown
	}
	scope := record.Scope
	if routing.NormalizeAgentID(scope.AgentID) != routing.NormalizeAgentID(q.AgentID) {
		return false
	}
	if routing.NormalizeAccountID(scope.Account) != routing.NormalizeAccountID(q.Account) {
		return false
	}

	bot := SessionBotAccount(scope)
	if q.Mode == DashboardModeSuperadmin {
		// The configured Telegram bot account is the authorization entrypoint.
		// Telegram-origin sessions remain bound to that bot account, while other
		// channels may appear in the same authorized agent/account catalog.
		if isTelegramSessionScope(scope, q.BotAccount) {
			return bot != "" && strings.EqualFold(bot, strings.TrimSpace(q.BotAccount))
		}
		return true
	}
	if !isTelegramSessionScope(scope, q.BotAccount) ||
		bot == "" ||
		!strings.EqualFold(bot, strings.TrimSpace(q.BotAccount)) {
		return false
	}
	owner, ok := VerifiedTelegramOwner(scope, q.BotAccount)
	return ok && owner == strings.TrimSpace(q.OwnerUserID)
}

func isTelegramSessionScope(scope *SessionScope, authorizedBotAccount string) bool {
	if scope == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(scope.Platform), "telegram") || strings.TrimSpace(scope.BotAccount) != "" {
		return true
	}
	// Older Telegram metadata has no Platform/BotAccount. Matching the
	// authorized Telegram channel instance is sufficient proof for migration.
	return strings.EqualFold(strings.TrimSpace(scope.Channel), strings.TrimSpace(authorizedBotAccount))
}

// SessionBotAccount returns the durable Telegram bot/channel-instance boundary.
// Older structured metadata did not have BotAccount, so Channel is accepted as
// a backward-compatible fallback when the caller already knows the Telegram
// channel instance being queried.
func SessionBotAccount(scope *SessionScope) string {
	if scope == nil {
		return ""
	}
	if account := strings.TrimSpace(scope.BotAccount); account != "" {
		return account
	}
	return strings.TrimSpace(scope.Channel)
}

// VerifiedTelegramOwner extracts a numeric owner only when durable metadata
// proves a user-scoped Telegram session. Shared group/topic sessions with no
// sender ownership dimension deliberately fail closed.
func VerifiedTelegramOwner(scope *SessionScope, botAccount string) (string, bool) {
	if scope == nil {
		return "", false
	}
	if botAccount != "" && !strings.EqualFold(SessionBotAccount(scope), strings.TrimSpace(botAccount)) {
		return "", false
	}
	if owner := positiveTelegramID(scope.OwnerUserID); owner != "" {
		return owner, true
	}

	telegramProven := strings.EqualFold(strings.TrimSpace(scope.Platform), "telegram") ||
		strings.EqualFold(strings.TrimSpace(scope.Channel), strings.TrimSpace(botAccount))
	if !telegramProven {
		return "", false
	}

	if hasScopeDimension(scope, "sender") {
		if owner := positiveTelegramID(scope.Values["sender"]); owner != "" {
			return owner, true
		}
	}
	if strings.EqualFold(strings.TrimSpace(scope.OriginChatType), "direct") {
		if owner := positiveTelegramID(scope.OriginSenderID); owner != "" {
			return owner, true
		}
	}
	if hasScopeDimension(scope, "chat") {
		value := strings.TrimSpace(scope.Values["chat"])
		if strings.HasPrefix(strings.ToLower(value), "direct:") {
			if owner := positiveTelegramID(strings.TrimSpace(value[len("direct:"):])); owner != "" {
				return owner, true
			}
		}
	}
	return "", false
}

func positiveTelegramID(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.LastIndex(value, ":"); idx >= 0 {
		value = strings.TrimSpace(value[idx+1:])
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func hasScopeDimension(scope *SessionScope, target string) bool {
	if scope == nil {
		return false
	}
	for _, dimension := range scope.Dimensions {
		if strings.EqualFold(strings.TrimSpace(dimension), target) {
			return true
		}
	}
	return false
}
