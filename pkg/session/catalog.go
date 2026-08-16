package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/As-tsaqib/picoclaw/pkg/memory"
	"github.com/As-tsaqib/picoclaw/pkg/providers"
)

const MaxSessionNameRunes = 60

var (
	ErrSessionCatalogUnavailable = errors.New("session catalog is unavailable")
	ErrSessionNotInScope         = errors.New("session is not in the current scope")
	ErrInvalidSessionName        = errors.New("session name is required")
	ErrAmbiguousSessionSelector  = errors.New("session selector is ambiguous")
)

type SessionRecord struct {
	Key           string
	ShortID       string
	Name          string
	NameSource    string
	MessageCount  int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Scope         *SessionScope
	LegacyUnknown bool
}

type catalogStore interface {
	GetSessionMeta(ctx context.Context, sessionKey string) (memory.SessionMeta, error)
	UpsertSessionMeta(ctx context.Context, sessionKey string, scope json.RawMessage, aliases []string) error
	SetSessionName(ctx context.Context, sessionKey, name, source string, autoNamePending bool) error
	SetSessionNameIfEmpty(ctx context.Context, sessionKey, name string) (bool, error)
	GetActiveSession(ctx context.Context, routeSignature string) (string, error)
	SetActiveSession(ctx context.Context, routeSignature, sessionKey string) error
	ClearActiveSession(ctx context.Context, routeSignature string) error
}

// SanitizeSessionName normalizes whitespace and enforces the 60-rune storage
// limit. Names remain plain data; platform adapters must not parse them as
// Markdown or HTML.
func SanitizeSessionName(name string) string {
	name = strings.ToValidUTF8(name, "")
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	if name == "" {
		return ""
	}
	runes := []rune(name)
	if len(runes) > MaxSessionNameRunes {
		name = string(runes[:MaxSessionNameRunes])
	}
	// Keep this defensive check close to the storage boundary even though
	// strings.ToValidUTF8 above normally guarantees it.
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "")
	}
	return name
}

func TemporarySessionName(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return "Session " + now.Local().Format("02 Jan 15:04")
}

func AutoNameFromMessage(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	return SanitizeSessionName(content)
}

func ScopeMatches(a, b *SessionScope) bool {
	if a == nil || b == nil {
		return false
	}
	return CanonicalScopeSignature(*a) == CanonicalScopeSignature(*b)
}

func routeSignature(scope *SessionScope) string {
	if scope == nil {
		return ""
	}
	return CanonicalScopeSignature(*scope)
}

func (b *JSONLBackend) catalogStore() (catalogStore, error) {
	store, ok := b.store.(catalogStore)
	if !ok {
		return nil, ErrSessionCatalogUnavailable
	}
	return store, nil
}

func (b *JSONLBackend) CreateScopedSession(
	scope *SessionScope,
	name string,
) (SessionRecord, error) {
	store, err := b.catalogStore()
	if err != nil {
		return SessionRecord{}, err
	}
	if scope == nil || routeSignature(scope) == "" {
		return SessionRecord{}, ErrSessionNotInScope
	}
	key, err := BuildSessionInstanceKey()
	if err != nil {
		return SessionRecord{}, err
	}
	rawScope, err := json.Marshal(scope)
	if err != nil {
		return SessionRecord{}, fmt.Errorf("encode session scope: %w", err)
	}
	if err := store.UpsertSessionMeta(context.Background(), key, rawScope, nil); err != nil {
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
	if err := store.SetSessionName(context.Background(), key, cleanName, source, pending); err != nil {
		return SessionRecord{}, err
	}
	return b.sessionRecord(key, scope, nil)
}

func (b *JSONLBackend) RenameScopedSession(
	scope *SessionScope,
	routeAliases []string,
	sessionKey, name string,
) error {
	cleanName := SanitizeSessionName(name)
	if cleanName == "" {
		return ErrInvalidSessionName
	}
	store, err := b.catalogStore()
	if err != nil {
		return err
	}
	if !b.sessionBelongsToScope(sessionKey, scope, routeAliases) {
		return ErrSessionNotInScope
	}
	return store.SetSessionName(context.Background(), sessionKey, cleanName, "custom", false)
}

func (b *JSONLBackend) SetAutomaticSessionName(sessionKey, content string) error {
	name := AutoNameFromMessage(content)
	if name == "" {
		return nil
	}
	store, err := b.catalogStore()
	if err != nil {
		return err
	}
	_, err = store.SetSessionNameIfEmpty(context.Background(), sessionKey, name)
	return err
}

func (b *JSONLBackend) ActiveScopedSession(
	scope *SessionScope,
	routeAliases []string,
) string {
	defaultKey := ""
	if scope != nil {
		defaultKey = BuildSessionKey(*scope)
	}
	store, err := b.catalogStore()
	if err != nil || routeSignature(scope) == "" {
		return defaultKey
	}
	key, err := store.GetActiveSession(context.Background(), routeSignature(scope))
	if err != nil || strings.TrimSpace(key) == "" {
		return defaultKey
	}
	if !b.sessionBelongsToScope(key, scope, routeAliases) {
		_ = store.ClearActiveSession(context.Background(), routeSignature(scope))
		return defaultKey
	}
	return key
}

func (b *JSONLBackend) SetActiveScopedSession(
	scope *SessionScope,
	routeAliases []string,
	sessionKey string,
) error {
	store, err := b.catalogStore()
	if err != nil {
		return err
	}
	if !b.sessionBelongsToScope(sessionKey, scope, routeAliases) {
		return ErrSessionNotInScope
	}
	return store.SetActiveSession(context.Background(), routeSignature(scope), sessionKey)
}

func (b *JSONLBackend) ListScopedSessions(
	scope *SessionScope,
	routeAliases []string,
) ([]SessionRecord, error) {
	if _, err := b.catalogStore(); err != nil {
		return nil, err
	}
	keys := b.ListSessions()
	records := make([]SessionRecord, 0, len(keys))
	for _, key := range keys {
		if !b.sessionBelongsToScope(key, scope, routeAliases) {
			continue
		}
		record, err := b.sessionRecord(key, scope, routeAliases)
		if err != nil {
			continue
		}
		records = append(records, record)
	}
	defaultKey := ""
	if scope != nil {
		defaultKey = BuildSessionKey(*scope)
	}
	if defaultKey != "" && !containsSessionRecord(records, defaultKey) {
		// A brand-new route may not have a .meta.json yet. Include its
		// deterministic session so /session works before the first LLM turn.
		b.EnsureSessionMetadata(defaultKey, scope, routeAliases)
		if record, err := b.sessionRecord(defaultKey, scope, routeAliases); err == nil {
			records = append(records, record)
		}
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].UpdatedAt.Equal(records[j].UpdatedAt) {
			return records[i].Key < records[j].Key
		}
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	return records, nil
}

func (b *JSONLBackend) ResolveScopedSelector(
	scope *SessionScope,
	routeAliases []string,
	selector string,
) (SessionRecord, error) {
	records, err := b.ListScopedSessions(scope, routeAliases)
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

func (b *JSONLBackend) sessionRecord(
	key string,
	scope *SessionScope,
	routeAliases []string,
) (SessionRecord, error) {
	store, err := b.catalogStore()
	if err != nil {
		return SessionRecord{}, err
	}
	meta, err := store.GetSessionMeta(context.Background(), key)
	if err != nil {
		return SessionRecord{}, err
	}
	history := b.GetHistory(key)
	name := SanitizeSessionName(meta.Name)
	nameSource := normalizedNameSource(meta.NameSource)
	if name == "" {
		name = firstVisibleUserName(history)
		if name != "" {
			if stored, setErr := store.SetSessionNameIfEmpty(context.Background(), key, name); setErr == nil && stored {
				nameSource = "auto"
			}
		}
	}
	if name == "" {
		when := meta.CreatedAt
		if when.IsZero() {
			when = time.Now()
		}
		name = TemporarySessionName(when)
		if setErr := store.SetSessionName(context.Background(), key, name, "auto", true); setErr == nil {
			nameSource = "auto"
		}
	}
	messageCount, lastVisible := visibleSessionStats(history)
	updated := lastVisible
	if updated.IsZero() {
		updated = meta.UpdatedAt
	}
	var storedScope *SessionScope
	if len(meta.Scope) > 0 {
		var decoded SessionScope
		if json.Unmarshal(meta.Scope, &decoded) == nil {
			storedScope = CloneScope(&decoded)
		}
	}
	if storedScope == nil {
		storedScope = CloneScope(scope)
	}
	return SessionRecord{
		Key:          key,
		ShortID:      ShortSessionID(key),
		Name:         name,
		NameSource:   nameSource,
		MessageCount: messageCount,
		CreatedAt:    meta.CreatedAt,
		UpdatedAt:    updated,
		Scope:        storedScope,
	}, nil
}

func (b *JSONLBackend) sessionBelongsToScope(
	key string,
	scope *SessionScope,
	routeAliases []string,
) bool {
	if scope == nil || strings.TrimSpace(key) == "" {
		return false
	}
	store, err := b.catalogStore()
	if err != nil {
		return false
	}
	meta, err := store.GetSessionMeta(context.Background(), key)
	if err != nil {
		return false
	}
	if len(meta.Scope) > 0 {
		var stored SessionScope
		if json.Unmarshal(meta.Scope, &stored) != nil {
			return false
		}
		return ScopeMatches(&stored, scope)
	}

	// Metadata-free legacy sessions are visible only when a non-main legacy
	// alias proves that they belong to the current deterministic route.
	defaultKey := BuildSessionKey(*scope)
	if key == defaultKey {
		return true
	}
	allowed := make(map[string]struct{})
	for _, alias := range LegacyAliasesProveScope(scope, routeAliases) {
		allowed[alias] = struct{}{}
	}
	if _, ok := allowed[strings.ToLower(strings.TrimSpace(key))]; ok {
		return true
	}
	for _, alias := range meta.Aliases {
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(alias))]; ok {
			return true
		}
	}
	return false
}

func isLegacyMainSession(key string) bool {
	parsed := ParseLegacyAgentSessionKey(key)
	return parsed != nil && strings.EqualFold(strings.TrimSpace(parsed.Rest), "main")
}

func containsSessionRecord(records []SessionRecord, key string) bool {
	for _, record := range records {
		if record.Key == key {
			return true
		}
	}
	return false
}

func normalizedNameSource(source string) string {
	if strings.EqualFold(strings.TrimSpace(source), "custom") {
		return "custom"
	}
	return "auto"
}

func firstVisibleUserName(history []providers.Message) string {
	for _, message := range history {
		if !strings.EqualFold(strings.TrimSpace(message.Role), "user") {
			continue
		}
		if name := AutoNameFromMessage(message.Content); name != "" {
			return name
		}
	}
	return ""
}

func visibleSessionStats(history []providers.Message) (int, time.Time) {
	count := 0
	var last time.Time
	for _, message := range history {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		visible := false
		switch role {
		case "user":
			visible = strings.TrimSpace(message.Content) != "" || len(message.Media) > 0 || len(message.Attachments) > 0
		case "assistant":
			visible = strings.TrimSpace(message.Content) != "" && len(message.ToolCalls) == 0 && strings.TrimSpace(message.ToolCallID) == ""
		}
		if !visible {
			continue
		}
		count++
		if message.CreatedAt != nil && message.CreatedAt.After(last) {
			last = *message.CreatedAt
		}
	}
	return count, last
}
