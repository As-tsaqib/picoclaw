package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/fileutil"
	"github.com/As-tsaqib/picoclaw/pkg/providers"
	"github.com/As-tsaqib/picoclaw/pkg/providers/messageutil"
)

type Session struct {
	Key             string              `json:"key"`
	Name            string              `json:"name,omitempty"`
	NameSource      string              `json:"name_source,omitempty"`
	AutoNamePending bool                `json:"auto_name_pending,omitempty"`
	Messages        []providers.Message `json:"messages"`
	Summary         string              `json:"summary,omitempty"`
	Created         time.Time           `json:"created"`
	Updated         time.Time           `json:"updated"`
	Scope           *SessionScope       `json:"scope,omitempty"`
	Aliases         []string            `json:"aliases,omitempty"`
}

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	storage  string
	active   map[string]string
}

func NewSessionManager(storage string) *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*Session),
		storage:  storage,
		active:   make(map[string]string),
	}

	if storage != "" {
		os.MkdirAll(storage, 0o700)
		sm.loadSessions()
		sm.loadActiveSessions()
	}

	return sm
}

func (sm *SessionManager) GetOrCreate(key string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[key]
	if ok {
		return session
	}

	session = &Session{
		Key:      key,
		Messages: []providers.Message{},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	sm.sessions[key] = session

	return session
}

func ensureMessageCreatedAt(msg *providers.Message, fallback time.Time) {
	if msg.CreatedAt != nil && !msg.CreatedAt.IsZero() {
		return
	}
	ts := fallback
	msg.CreatedAt = &ts
}

func normalizeHistoryCreatedAt(history []providers.Message) {
	now := time.Now()
	for i := range history {
		ensureMessageCreatedAt(&history[i], now)
	}
}

func (sm *SessionManager) AddMessage(sessionKey, role, content string) {
	sm.AddFullMessage(sessionKey, providers.Message{
		Role:    role,
		Content: content,
	})
}

// AddFullMessage adds a complete message with tool calls and tool call ID to the session.
// This is used to save the full conversation flow including tool calls and tool results.
func (sm *SessionManager) AddFullMessage(sessionKey string, msg providers.Message) {
	if messageutil.IsTransientAssistantThoughtMessage(msg) {
		return
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[sessionKey]
	if !ok {
		session = &Session{
			Key:      sessionKey,
			Messages: []providers.Message{},
			Created:  time.Now(),
		}
		sm.sessions[sessionKey] = session
	}

	now := time.Now()
	ensureMessageCreatedAt(&msg, now)

	session.Messages = append(session.Messages, msg)
	session.Updated = now
}

func (sm *SessionManager) GetHistory(key string) []providers.Message {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.sessions[key]
	if !ok {
		return []providers.Message{}
	}

	history := make([]providers.Message, len(session.Messages))
	copy(history, session.Messages)
	return history
}

func (sm *SessionManager) GetSummary(key string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.sessions[key]
	if !ok {
		return ""
	}
	return session.Summary
}

func (sm *SessionManager) SetSummary(key string, summary string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[key]
	if ok {
		session.Summary = summary
		session.Updated = time.Now()
	}
}

func (sm *SessionManager) TruncateHistory(key string, keepLast int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[key]
	if !ok {
		return
	}

	if keepLast <= 0 {
		session.Messages = []providers.Message{}
		session.Updated = time.Now()
		return
	}

	if len(session.Messages) <= keepLast {
		return
	}

	session.Messages = session.Messages[len(session.Messages)-keepLast:]
	session.Updated = time.Now()
}

func (sm *SessionManager) ListSessions() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	keys := make([]string, 0, len(sm.sessions))
	for k := range sm.sessions {
		keys = append(keys, k)
	}
	return keys
}

// sanitizeFilename converts a session key into a cross-platform safe filename.
// Replaces ':' with '_' (session key separator) and '/' and '\' with '_' so
// composite IDs (e.g. Telegram forum "chatID/threadID") do not create
// subdirectories or break on Windows. The original key is preserved inside
// the JSON file, so loadSessions still maps back to the right in-memory key.
func sanitizeFilename(key string) string {
	s := strings.ReplaceAll(key, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

func (sm *SessionManager) Save(key string) error {
	if sm.storage == "" {
		return nil
	}

	filename := sanitizeFilename(key)

	// filepath.IsLocal rejects empty names, "..", absolute paths, and
	// OS-reserved device names (NUL, COM1 … on Windows). sanitizeFilename
	// already replaced '/' and '\' with '_', so no subdirs are created.
	if filename == "." || !filepath.IsLocal(filename) {
		return os.ErrInvalid
	}

	// Snapshot under read lock, then perform slow file I/O after unlock.
	sm.mu.RLock()
	stored, ok := sm.sessions[key]
	if !ok {
		sm.mu.RUnlock()
		return nil
	}

	snapshot := Session{
		Key:             stored.Key,
		Name:            stored.Name,
		NameSource:      stored.NameSource,
		AutoNamePending: stored.AutoNamePending,
		Summary:         stored.Summary,
		Created:         stored.Created,
		Updated:         stored.Updated,
		Scope:           CloneScope(stored.Scope),
		Aliases:         append([]string(nil), stored.Aliases...),
	}
	if len(stored.Messages) > 0 {
		snapshot.Messages = messageutil.FilterInvalidHistoryMessages(stored.Messages)
	} else {
		snapshot.Messages = []providers.Message{}
	}
	sm.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	sessionPath := filepath.Join(sm.storage, filename+".json")
	tmpFile, err := os.CreateTemp(sm.storage, "session-*.tmp")
	if err != nil {
		return err
	}

	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, sessionPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (sm *SessionManager) loadSessions() error {
	files, err := os.ReadDir(sm.storage)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if filepath.Ext(file.Name()) != ".json" {
			continue
		}

		sessionPath := filepath.Join(sm.storage, file.Name())
		data, err := os.ReadFile(sessionPath)
		if err != nil {
			continue
		}

		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}
		if strings.TrimSpace(session.Key) == "" {
			continue
		}
		session.Messages = messageutil.FilterInvalidHistoryMessages(session.Messages)
		normalizeHistoryCreatedAt(session.Messages)

		sm.sessions[session.Key] = &session
	}

	return nil
}

// Close is a no-op for the in-memory SessionManager; it satisfies the
// SessionStore interface so callers can release resources uniformly.
func (sm *SessionManager) Close() error {
	return nil
}

// SetHistory updates the messages of a session.
func (sm *SessionManager) SetHistory(key string, history []providers.Message) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[key]
	if ok {
		history = messageutil.FilterInvalidHistoryMessages(history)
		// Create a deep copy to strictly isolate internal state
		// from the caller's slice.
		msgs := make([]providers.Message, len(history))
		copy(msgs, history)
		normalizeHistoryCreatedAt(msgs)
		session.Messages = msgs
		session.Updated = time.Now()
	}
}

const (
	activeSessionsFilename       = ".active-sessions.json"
	legacyActiveSessionsFilename = ".active-sessions-legacy.json"
)

func (sm *SessionManager) activeSessionsPath() string {
	return filepath.Join(sm.storage, activeSessionsFilename)
}

func (sm *SessionManager) loadActiveSessions() {
	if sm.storage == "" {
		return
	}
	data, err := os.ReadFile(sm.activeSessionsPath())
	if os.IsNotExist(err) {
		data, err = os.ReadFile(filepath.Join(sm.storage, legacyActiveSessionsFilename))
	}
	if err != nil {
		return
	}
	var active map[string]string
	if json.Unmarshal(data, &active) == nil && active != nil {
		sm.active = active
	}
}

func (sm *SessionManager) saveActiveSessions(snapshot map[string]string) error {
	if sm.storage == "" {
		return nil
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(sm.activeSessionsPath(), data, 0o600)
}

func (sm *SessionManager) EnsureSessionMetadata(key string, scope *SessionScope, aliases []string) {
	key = strings.TrimSpace(key)
	if key == "" || scope == nil {
		return
	}
	sm.mu.Lock()
	stored, ok := sm.sessions[key]
	if !ok {
		now := time.Now()
		stored = &Session{Key: key, Messages: []providers.Message{}, Created: now, Updated: now}
		sm.sessions[key] = stored
	}
	if stored.Scope != nil && !ScopeMatches(stored.Scope, scope) {
		sm.mu.Unlock()
		return
	}
	stored.Scope = CloneScope(scope)
	stored.Aliases = metadataAliasesForScope(scope, aliases)
	stored.Updated = time.Now()
	sm.mu.Unlock()
	_ = sm.Save(key)
}

func (sm *SessionManager) ResolveSessionKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if _, ok := sm.sessions[key]; ok {
		return key
	}
	for canonical, stored := range sm.sessions {
		for _, alias := range stored.Aliases {
			if alias == key {
				return canonical
			}
		}
	}
	return key
}

func (sm *SessionManager) GetSessionScope(key string) *SessionScope {
	key = sm.ResolveSessionKey(key)
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if stored := sm.sessions[key]; stored != nil {
		return CloneScope(stored.Scope)
	}
	return nil
}

func (sm *SessionManager) CreateScopedSession(scope *SessionScope, name string) (SessionRecord, error) {
	if scope == nil {
		return SessionRecord{}, ErrSessionNotInScope
	}
	key, err := BuildSessionInstanceKey()
	if err != nil {
		return SessionRecord{}, err
	}
	cleanName := SanitizeSessionName(name)
	source := "custom"
	pending := false
	if cleanName == "" {
		cleanName = TemporarySessionName(time.Now())
		source = "auto"
		pending = true
	}
	now := time.Now()
	sm.mu.Lock()
	sm.sessions[key] = &Session{
		Key: key, Name: cleanName, NameSource: source, AutoNamePending: pending,
		Messages: []providers.Message{}, Created: now, Updated: now, Scope: CloneScope(scope),
	}
	sm.mu.Unlock()
	if err := sm.Save(key); err != nil {
		return SessionRecord{}, err
	}
	return SessionRecord{
		Key: key, ShortID: ShortSessionID(key), Name: cleanName, NameSource: source,
		CreatedAt: now, UpdatedAt: now, Scope: CloneScope(scope),
	}, nil
}

func (sm *SessionManager) RenameScopedSession(scope *SessionScope, aliases []string, key, name string) error {
	cleanName := SanitizeSessionName(name)
	if cleanName == "" {
		return ErrInvalidSessionName
	}
	sm.mu.Lock()
	stored := sm.sessions[key]
	if stored == nil || !legacySessionMatchesScope(stored, scope, aliases) {
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

func (sm *SessionManager) SetAutomaticSessionName(key, content string) error {
	name := AutoNameFromMessage(content)
	if name == "" {
		return nil
	}
	sm.mu.Lock()
	stored := sm.sessions[key]
	if stored == nil {
		now := time.Now()
		stored = &Session{Key: key, Messages: []providers.Message{}, Created: now, Updated: now}
		sm.sessions[key] = stored
	}
	if stored.Name != "" && !stored.AutoNamePending {
		sm.mu.Unlock()
		return nil
	}
	stored.Name = name
	stored.NameSource = "auto"
	stored.AutoNamePending = false
	stored.Updated = time.Now()
	sm.mu.Unlock()
	return sm.Save(key)
}

func (sm *SessionManager) ActiveScopedSession(scope *SessionScope, aliases []string) string {
	if scope == nil {
		return ""
	}
	defaultKey := BuildSessionKey(*scope)
	signature := CanonicalScopeSignature(*scope)
	sm.mu.RLock()
	active := sm.active[signature]
	stored := sm.sessions[active]
	valid := stored != nil && legacySessionMatchesScope(stored, scope, aliases)
	sm.mu.RUnlock()
	if valid {
		return active
	}
	return defaultKey
}

func (sm *SessionManager) SetActiveScopedSession(scope *SessionScope, aliases []string, key string) error {
	if scope == nil {
		return ErrSessionNotInScope
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	stored := sm.sessions[key]
	if stored == nil || !legacySessionMatchesScope(stored, scope, aliases) {
		return ErrSessionNotInScope
	}
	route := CanonicalScopeSignature(*scope)
	snapshot := make(map[string]string, len(sm.active)+1)
	for route, active := range sm.active {
		snapshot[route] = active
	}
	snapshot[route] = key
	if err := sm.saveActiveSessions(snapshot); err != nil {
		return err
	}
	// Publish the new in-memory selection only after its durable snapshot has
	// been committed. Holding sm.mu across both operations keeps concurrent
	// switches ordered consistently in memory and on disk.
	sm.active[route] = key
	return nil
}

func (sm *SessionManager) ListScopedSessions(scope *SessionScope, aliases []string) ([]SessionRecord, error) {
	if scope == nil {
		return nil, ErrSessionNotInScope
	}
	defaultKey := BuildSessionKey(*scope)
	sm.EnsureSessionMetadata(defaultKey, scope, aliases)
	sm.mu.Lock()
	records := make([]SessionRecord, 0, len(sm.sessions))
	dirty := make([]string, 0)
	for key, stored := range sm.sessions {
		if !legacySessionMatchesScope(stored, scope, aliases) {
			continue
		}
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

func (sm *SessionManager) ResolveScopedSelector(
	scope *SessionScope,
	aliases []string,
	selector string,
) (SessionRecord, error) {
	records, err := sm.ListScopedSessions(scope, aliases)
	if err != nil {
		return SessionRecord{}, err
	}
	selector = strings.TrimSpace(strings.ToLower(selector))
	if n, err := strconv.Atoi(selector); err == nil {
		if n > 0 && n <= len(records) {
			return records[n-1], nil
		}
		return SessionRecord{}, ErrSessionNotInScope
	}
	var match *SessionRecord
	for i := range records {
		record := records[i]
		if strings.EqualFold(record.ShortID, selector) {
			if match != nil {
				return SessionRecord{}, ErrAmbiguousSessionSelector
			}
			matched := record
			match = &matched
		}
	}
	if match != nil {
		return *match, nil
	}
	return SessionRecord{}, ErrSessionNotInScope
}

func legacySessionMatchesScope(stored *Session, scope *SessionScope, routeAliases []string) bool {
	if stored == nil || scope == nil {
		return false
	}
	if stored.Scope != nil {
		return ScopeMatches(stored.Scope, scope)
	}
	if stored.Key == BuildSessionKey(*scope) {
		return true
	}
	allowed := make(map[string]struct{})
	for _, alias := range LegacyAliasesProveScope(scope, routeAliases) {
		allowed[alias] = struct{}{}
	}
	if _, ok := allowed[strings.ToLower(strings.TrimSpace(stored.Key))]; ok {
		return true
	}
	for _, alias := range stored.Aliases {
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(alias))]; ok {
			return true
		}
	}
	return false
}
