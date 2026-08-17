package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/As-tsaqib/picoclaw/pkg/fileutil"
)

// ModelOverride is a durable, credential-free model selection for one session.
// ConfigRef identifies the configured model entry whose provider credentials
// and transport settings should be reused. Model may differ from that entry
// when it came from live provider discovery.
type ModelOverride struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Alias     string `json:"alias,omitempty"`
	ConfigRef string `json:"config_ref"`
	Source    string `json:"source,omitempty"`
}

func (o ModelOverride) Valid() bool {
	return strings.TrimSpace(o.Provider) != "" && strings.TrimSpace(o.Model) != "" &&
		strings.TrimSpace(o.ConfigRef) != ""
}

// ModelOverrideStore is an optional extension implemented by official session
// backends. It intentionally stays outside SessionStore so third-party stores
// keep compiling and /model can fail gracefully when persistence is absent.
type ModelOverrideStore interface {
	GetModelOverride(sessionKey string) (ModelOverride, bool, error)
	SetModelOverride(sessionKey string, override ModelOverride) error
	ClearModelOverride(sessionKey string) error
}

const (
	managerModelOverridesFilename = ".model-overrides.json"
	managerModelOverrideMemoryKey = "\x00model-override:"
)

// Disk-backed SessionManagers share the same sidecar path when they point at
// the same storage directory, so serialize read-modify-write operations across
// instances. In-memory managers keep their overrides on the manager itself via
// its existing active map, avoiding package-global references that would keep
// discarded managers alive indefinitely.
var managerModelOverridesMu sync.Mutex

func lockManagerModelOverrides(path string) (func(), error) {
	managerModelOverridesMu.Lock()
	fileUnlock, err := fileutil.LockFile(path)
	if err != nil {
		managerModelOverridesMu.Unlock()
		return nil, fmt.Errorf("session: lock model overrides: %w", err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = fileUnlock()
			managerModelOverridesMu.Unlock()
		})
	}, nil
}

func normalizeModelOverride(override ModelOverride) ModelOverride {
	override.Provider = strings.TrimSpace(override.Provider)
	override.Model = strings.TrimSpace(override.Model)
	override.Alias = strings.TrimSpace(override.Alias)
	override.ConfigRef = strings.TrimSpace(override.ConfigRef)
	override.Source = strings.ToLower(strings.TrimSpace(override.Source))
	return override
}

func (sm *SessionManager) modelOverridesPath() string {
	return filepath.Join(sm.storage, managerModelOverridesFilename)
}

func (sm *SessionManager) readModelOverridesLocked() (map[string]ModelOverride, error) {
	data, err := os.ReadFile(sm.modelOverridesPath())
	if os.IsNotExist(err) {
		return make(map[string]ModelOverride), nil
	}
	if err != nil {
		return nil, fmt.Errorf("session: read model overrides: %w", err)
	}
	var overrides map[string]ModelOverride
	if err := json.Unmarshal(data, &overrides); err != nil {
		return nil, fmt.Errorf("session: decode model overrides: %w", err)
	}
	if overrides == nil {
		overrides = make(map[string]ModelOverride)
	}
	return overrides, nil
}

func (sm *SessionManager) writeModelOverridesLocked(overrides map[string]ModelOverride) error {
	data, err := json.MarshalIndent(overrides, "", "  ")
	if err != nil {
		return fmt.Errorf("session: encode model overrides: %w", err)
	}
	return fileutil.WriteFileAtomic(sm.modelOverridesPath(), data, 0o600)
}

func (sm *SessionManager) getMemoryModelOverride(sessionKey string) (ModelOverride, bool, error) {
	sm.mu.RLock()
	raw, ok := sm.active[managerModelOverrideMemoryKey+sessionKey]
	sm.mu.RUnlock()
	if !ok || strings.TrimSpace(raw) == "" {
		return ModelOverride{}, false, nil
	}
	var override ModelOverride
	if err := json.Unmarshal([]byte(raw), &override); err != nil {
		return ModelOverride{}, false, fmt.Errorf("session: decode in-memory model override: %w", err)
	}
	override = normalizeModelOverride(override)
	if !override.Valid() {
		return ModelOverride{}, false, nil
	}
	return override, true, nil
}

func (sm *SessionManager) setMemoryModelOverride(sessionKey string, override ModelOverride) error {
	raw, err := json.Marshal(override)
	if err != nil {
		return fmt.Errorf("session: encode in-memory model override: %w", err)
	}
	sm.mu.Lock()
	sm.active[managerModelOverrideMemoryKey+sessionKey] = string(raw)
	sm.mu.Unlock()
	return nil
}

func (sm *SessionManager) GetModelOverride(sessionKey string) (ModelOverride, bool, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ModelOverride{}, false, nil
	}
	if strings.TrimSpace(sm.storage) == "" {
		return sm.getMemoryModelOverride(sessionKey)
	}

	unlock, err := lockManagerModelOverrides(sm.modelOverridesPath())
	if err != nil {
		return ModelOverride{}, false, err
	}
	defer unlock()
	overrides, err := sm.readModelOverridesLocked()
	if err != nil {
		return ModelOverride{}, false, err
	}
	override, ok := overrides[sessionKey]
	override = normalizeModelOverride(override)
	if !ok || !override.Valid() {
		return ModelOverride{}, false, nil
	}
	return override, true, nil
}

func (sm *SessionManager) SetModelOverride(sessionKey string, override ModelOverride) error {
	sessionKey = strings.TrimSpace(sessionKey)
	override = normalizeModelOverride(override)
	if sessionKey == "" {
		return fmt.Errorf("session: session key is required")
	}
	if !override.Valid() {
		return fmt.Errorf("session: provider, model, and config_ref are required")
	}
	if strings.TrimSpace(sm.storage) == "" {
		return sm.setMemoryModelOverride(sessionKey, override)
	}

	unlock, err := lockManagerModelOverrides(sm.modelOverridesPath())
	if err != nil {
		return err
	}
	defer unlock()
	overrides, err := sm.readModelOverridesLocked()
	if err != nil {
		return err
	}
	overrides[sessionKey] = override
	return sm.writeModelOverridesLocked(overrides)
}

func (sm *SessionManager) ClearModelOverride(sessionKey string) error {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}
	if strings.TrimSpace(sm.storage) == "" {
		sm.mu.Lock()
		delete(sm.active, managerModelOverrideMemoryKey+sessionKey)
		sm.mu.Unlock()
		return nil
	}

	unlock, err := lockManagerModelOverrides(sm.modelOverridesPath())
	if err != nil {
		return err
	}
	defer unlock()
	overrides, err := sm.readModelOverridesLocked()
	if err != nil {
		return err
	}
	delete(overrides, sessionKey)
	return sm.writeModelOverridesLocked(overrides)
}

var _ ModelOverrideStore = (*SessionManager)(nil)
