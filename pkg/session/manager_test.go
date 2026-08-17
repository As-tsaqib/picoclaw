package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"telegram:123456", "telegram_123456"},
		{"discord:987654321", "discord_987654321"},
		{"slack:C01234", "slack_C01234"},
		{"no-colons-here", "no-colons-here"},
		{"multiple:colons:here", "multiple_colons_here"},
		{"agent:main:telegram:group:-1003822706455/12", "agent_main_telegram_group_-1003822706455_12"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSave_WithColonInKey(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSessionManager(tmpDir)

	// Create a session with a key containing colon (typical channel session key).
	key := "telegram:123456"
	sm.GetOrCreate(key)
	sm.AddMessage(key, "user", "hello")

	// Save should succeed even though the key contains ':'
	if err := sm.Save(key); err != nil {
		t.Fatalf("Save(%q) failed: %v", key, err)
	}

	// The file on disk should use sanitized name.
	expectedFile := filepath.Join(tmpDir, "telegram_123456.json")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Fatalf("expected session file %s to exist", expectedFile)
	}

	// Load into a fresh manager and verify the session round-trips.
	sm2 := NewSessionManager(tmpDir)
	history := sm2.GetHistory(key)
	if len(history) != 1 {
		t.Fatalf("expected 1 message after reload, got %d", len(history))
	}
	if history[0].Content != "hello" {
		t.Errorf("expected message content %q, got %q", "hello", history[0].Content)
	}
}

func TestSave_RejectsPathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSessionManager(tmpDir)

	// Invalid names that must still be rejected.
	badKeys := []string{"", ".", ".."}
	for _, key := range badKeys {
		sm.GetOrCreate(key)
		if err := sm.Save(key); err == nil {
			t.Errorf("Save(%q) should have failed but didn't", key)
		}
	}

	// Keys containing path separators are sanitized (no subdirs created).
	sm.GetOrCreate("foo/bar")
	if err := sm.Save("foo/bar"); err != nil {
		t.Fatalf("Save(\"foo/bar\") after sanitize should succeed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "foo_bar.json")); os.IsNotExist(err) {
		t.Errorf("expected foo_bar.json in storage (sanitized from foo/bar)")
	}
}

func TestLoadSessions_NormalizesMissingCreatedAt(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "telegram_legacy.json")
	legacy := `{
  "key": "telegram:legacy",
  "messages": [
    {
      "role": "user",
      "content": "hello"
    }
  ],
  "created": "2026-01-01T00:00:00Z",
  "updated": "2026-01-01T00:00:00Z"
}`

	if err := os.WriteFile(sessionPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sm := NewSessionManager(tmpDir)
	history := sm.GetHistory("telegram:legacy")
	if len(history) != 1 {
		t.Fatalf("history = %d, want 1", len(history))
	}
	if history[0].CreatedAt == nil || history[0].CreatedAt.IsZero() {
		t.Fatalf("history[0].CreatedAt = %v, want non-zero timestamp", history[0].CreatedAt)
	}
}

func TestSessionManagerNamedCatalogAndActiveMappingPersist(t *testing.T) {
	dir := t.TempDir()
	scope := &SessionScope{
		Version: ScopeVersionV1, AgentID: "main", Channel: "telegram", Account: "bot-a",
		Dimensions: []string{"chat"}, Values: map[string]string{"chat": "direct:42"},
	}
	sm := NewSessionManager(dir)
	record, err := sm.CreateScopedSession(scope, "Fallback backend")
	if err != nil {
		t.Fatalf("CreateScopedSession: %v", err)
	}
	if setErr := sm.SetActiveScopedSession(scope, nil, record.Key); setErr != nil {
		t.Fatalf("SetActiveScopedSession: %v", setErr)
	}

	reopened := NewSessionManager(dir)
	if got := reopened.ActiveScopedSession(scope, nil); got != record.Key {
		t.Fatalf("active session = %q, want %q", got, record.Key)
	}
	records, err := reopened.ListScopedSessions(scope, nil)
	if err != nil {
		t.Fatalf("ListScopedSessions: %v", err)
	}
	found := false
	for _, got := range records {
		if got.Key == record.Key {
			found = true
			if got.Name != "Fallback backend" {
				t.Fatalf("name = %q", got.Name)
			}
		}
	}
	if !found {
		t.Fatal("named session missing after reopen")
	}
	if _, err := os.Stat(filepath.Join(dir, activeSessionsFilename)); err != nil {
		t.Fatalf("active mapping missing: %v", err)
	}
}

func TestSessionManagerRemoveScopedSessionPersistsFallbackAndCleanup(t *testing.T) {
	dir := t.TempDir()
	scope := &SessionScope{
		Version: ScopeVersionV1, AgentID: "main", Channel: "telegram", Account: "bot-a",
		Dimensions: []string{"chat"}, Values: map[string]string{"chat": "direct:42"},
	}
	sm := NewSessionManager(dir)
	record, err := sm.CreateScopedSession(scope, "Disposable")
	if err != nil {
		t.Fatalf("CreateScopedSession: %v", err)
	}
	sm.AddMessage(record.Key, "user", "history")
	if err := sm.Save(record.Key); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := sm.SetActiveScopedSession(scope, nil, record.Key); err != nil {
		t.Fatalf("SetActiveScopedSession: %v", err)
	}
	if err := sm.SetModelOverride(record.Key, ModelOverride{
		Provider: "openai", Model: "gpt-test", ConfigRef: "cfg:v1:test",
	}); err != nil {
		t.Fatalf("SetModelOverride: %v", err)
	}

	if err := sm.RemoveScopedSession(scope, nil, record.Key); err != nil {
		t.Fatalf("RemoveScopedSession: %v", err)
	}
	if got := sm.ActiveScopedSession(scope, nil); got != BuildSessionKey(*scope) {
		t.Fatalf("active fallback = %q", got)
	}
	if history := sm.GetHistory(record.Key); len(history) != 0 {
		t.Fatalf("removed history length = %d", len(history))
	}
	if _, found, err := sm.GetModelOverride(record.Key); err != nil || found {
		t.Fatalf("removed model override found=%t err=%v", found, err)
	}
	if _, err := os.Stat(filepath.Join(dir, sanitizeFilename(record.Key)+".json")); !os.IsNotExist(err) {
		t.Fatalf("removed session file still exists: %v", err)
	}

	reopened := NewSessionManager(dir)
	for _, got := range mustListScopedSessions(t, reopened, scope) {
		if got.Key == record.Key {
			t.Fatal("removed session reappeared after reopen")
		}
	}
}

func mustListScopedSessions(t *testing.T, sm *SessionManager, scope *SessionScope) []SessionRecord {
	t.Helper()
	records, err := sm.ListScopedSessions(scope, nil)
	if err != nil {
		t.Fatalf("ListScopedSessions: %v", err)
	}
	return records
}
