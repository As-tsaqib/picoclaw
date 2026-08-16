package session

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

var _ DashboardSessionStore = (*SessionManager)(nil)

func (sm *SessionManager) ensureDashboardDefault(q DashboardQuery) {
	key := strings.TrimSpace(q.DefaultSessionKey)
	if key == "" || q.DefaultScope == nil {
		return
	}
	sm.EnsureSessionMetadata(key, q.DefaultScope, q.DefaultAliases)
}

func sessionManagerDashboardAllowed(stored *Session, q DashboardQuery) (bool, bool) {
	if stored == nil || !q.valid() {
		return false, false
	}
	if stored.Scope != nil {
		return dashboardScopeAllowed(stored.Scope, q), false
	}
	legacyUnknown := true
	if q.Mode != DashboardModeSuperadmin || !q.IncludeLegacyUnknown {
		return false, legacyUnknown
	}
	return legacyDashboardAliasAllowed(stored.Key, stored.Aliases, q), legacyUnknown
}

func (sm *SessionManager) ListDashboardSessions(q DashboardQuery) ([]SessionRecord, error) {
	if !q.valid() {
		return nil, ErrSessionNotInScope
	}
	sm.ensureDashboardDefault(q)

	sm.mu.Lock()
	records := make([]SessionRecord, 0, len(sm.sessions))
	dirty := make([]string, 0)
	for key, stored := range sm.sessions {
		allowed, legacyUnknown := sessionManagerDashboardAllowed(stored, q)
		if !allowed {
			continue
		}
		// Authorization is decided using scope/alias metadata before any history
		// is inspected, matching the JSONL dashboard path.
		name := SanitizeSessionName(stored.Name)
		if name == "" {
			name = firstVisibleUserName(stored.Messages)
			stored.AutoNamePending = name == ""
			if name == "" {
				name = TemporarySessionName(stored.Created)
			}
			stored.Name = name
			stored.NameSource = "auto"
			dirty = append(dirty, key)
		}
		count, updated := visibleSessionStats(stored.Messages)
		if updated.IsZero() {
			updated = stored.Updated
		}
		records = append(records, SessionRecord{
			Key: key, ShortID: ShortSessionID(key), Name: name,
			NameSource: normalizedNameSource(stored.NameSource), MessageCount: count,
			CreatedAt: stored.Created, UpdatedAt: updated, Scope: CloneScope(stored.Scope),
			LegacyUnknown: legacyUnknown,
		})
	}
	sm.mu.Unlock()

	for _, key := range dirty {
		_ = sm.Save(key)
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].UpdatedAt.Equal(records[j].UpdatedAt) {
			return records[i].Key < records[j].Key
		}
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	return records, nil
}

func (sm *SessionManager) ActiveDashboardSession(q DashboardQuery) string {
	if !q.valid() {
		return ""
	}
	sm.ensureDashboardDefault(q)
	signature := dashboardSignature(q)

	sm.mu.Lock()
	if key := strings.TrimSpace(sm.active[signature]); key != "" {
		if allowed, _ := sessionManagerDashboardAllowed(sm.sessions[key], q); allowed {
			sm.mu.Unlock()
			return key
		}
		snapshot := make(map[string]string, len(sm.active))
		for route, active := range sm.active {
			if route != signature {
				snapshot[route] = active
			}
		}
		if err := sm.saveActiveSessions(snapshot); err == nil {
			delete(sm.active, signature)
		}
	}
	fallback := strings.TrimSpace(q.DefaultSessionKey)
	if fallback != "" {
		if allowed, _ := sessionManagerDashboardAllowed(sm.sessions[fallback], q); allowed {
			sm.mu.Unlock()
			return fallback
		}
	}
	sm.mu.Unlock()
	return ""
}

func (sm *SessionManager) SetActiveDashboardSession(q DashboardQuery, sessionKey string) error {
	if !q.valid() {
		return ErrSessionNotInScope
	}
	sm.ensureDashboardDefault(q)
	key := strings.TrimSpace(sessionKey)
	signature := dashboardSignature(q)

	sm.mu.Lock()
	defer sm.mu.Unlock()
	if allowed, _ := sessionManagerDashboardAllowed(sm.sessions[key], q); !allowed {
		return ErrSessionNotInScope
	}
	snapshot := make(map[string]string, len(sm.active)+1)
	for route, active := range sm.active {
		snapshot[route] = active
	}
	snapshot[signature] = key
	if err := sm.saveActiveSessions(snapshot); err != nil {
		return err
	}
	sm.active[signature] = key
	return nil
}

func (sm *SessionManager) RenameDashboardSession(q DashboardQuery, sessionKey, name string) error {
	cleanName := SanitizeSessionName(name)
	if cleanName == "" {
		return ErrInvalidSessionName
	}
	if !q.valid() {
		return ErrSessionNotInScope
	}
	key := strings.TrimSpace(sessionKey)

	sm.mu.Lock()
	stored := sm.sessions[key]
	if allowed, _ := sessionManagerDashboardAllowed(stored, q); !allowed {
		sm.mu.Unlock()
		return ErrSessionNotInScope
	}
	stored.Name = cleanName
	stored.NameSource = "custom"
	stored.AutoNamePending = false
	stored.Updated = time.Now()
	sm.mu.Unlock()
	return sm.Save(key)
}

func (sm *SessionManager) ResolveDashboardSelector(q DashboardQuery, selector string) (SessionRecord, error) {
	records, err := sm.ListDashboardSessions(q)
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
