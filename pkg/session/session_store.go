package session

import "github.com/As-tsaqib/picoclaw/pkg/providers"

// SessionStore defines the persistence operations used by the agent loop.
// Both SessionManager (legacy JSON backend) and JSONLBackend satisfy this
// interface, allowing the storage layer to be swapped without touching the
// agent loop code.
//
// Write methods (Add*, Set*, Truncate*) are fire-and-forget: they do not
// return errors. Implementations should log failures internally. This
// matches the original SessionManager contract that the agent loop relies on.
type SessionStore interface {
	// AddMessage appends a simple role/content message to the session.
	AddMessage(sessionKey, role, content string)
	// AddFullMessage appends a complete message including tool calls.
	AddFullMessage(sessionKey string, msg providers.Message)
	// GetHistory returns the full message history for the session.
	GetHistory(key string) []providers.Message
	// GetSummary returns the conversation summary, or "" if none.
	GetSummary(key string) string
	// SetSummary replaces the conversation summary.
	SetSummary(key, summary string)
	// SetHistory replaces the full message history.
	SetHistory(key string, history []providers.Message)
	// TruncateHistory keeps only the last keepLast messages.
	TruncateHistory(key string, keepLast int)
	// Save persists any pending state to durable storage.
	Save(key string) error
	// ListSessions returns all known session keys.
	ListSessions() []string
	// Close releases resources held by the store.
	Close() error
}

// ScopedSessionStore is implemented by durable backends that support named
// multi-session routing. The legacy SessionStore contract remains unchanged so
// alternative backends continue to work with a graceful command fallback.
type ScopedSessionStore interface {
	SessionStore
	CreateScopedSession(scope *SessionScope, name string) (SessionRecord, error)
	RenameScopedSession(scope *SessionScope, routeAliases []string, sessionKey, name string) error
	SetAutomaticSessionName(sessionKey, content string) error
	ActiveScopedSession(scope *SessionScope, routeAliases []string) string
	SetActiveScopedSession(scope *SessionScope, routeAliases []string, sessionKey string) error
	ListScopedSessions(scope *SessionScope, routeAliases []string) ([]SessionRecord, error)
	ResolveScopedSelector(scope *SessionScope, routeAliases []string, selector string) (SessionRecord, error)
}

// DashboardSessionStore extends scoped session storage with private Telegram
// dashboard catalogs and durable dashboard-only active mappings.
type DashboardSessionStore interface {
	ScopedSessionStore
	ListDashboardSessions(query DashboardQuery) ([]SessionRecord, error)
	ActiveDashboardSession(query DashboardQuery) string
	SetActiveDashboardSession(query DashboardQuery, sessionKey string) error
	RenameDashboardSession(query DashboardQuery, sessionKey, name string) error
	ResolveDashboardSelector(query DashboardQuery, selector string) (SessionRecord, error)
}
