package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/fileutil"
)

const modelOverridesFilename = ".model-overrides.json"

func (s *JSONLStore) modelOverridesPath() string {
	return filepath.Join(s.dir, modelOverridesFilename)
}

func (s *JSONLStore) readModelOverridesLocked() (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(s.modelOverridesPath())
	if os.IsNotExist(err) {
		return make(map[string]json.RawMessage), nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: read model overrides: %w", err)
	}
	var overrides map[string]json.RawMessage
	if err := json.Unmarshal(data, &overrides); err != nil {
		return nil, fmt.Errorf("memory: decode model overrides: %w", err)
	}
	if overrides == nil {
		overrides = make(map[string]json.RawMessage)
	}
	return overrides, nil
}

func (s *JSONLStore) writeModelOverridesLocked(overrides map[string]json.RawMessage) error {
	data, err := json.MarshalIndent(overrides, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: encode model overrides: %w", err)
	}
	return fileutil.WriteFileAtomic(s.modelOverridesPath(), data, 0o600)
}

// GetSessionModelOverride returns the credential-free model override payload.
// activeMu is reused as the small sidecar-state lock so concurrent session
// switches cannot race file replacement.
func (s *JSONLStore) GetSessionModelOverride(_ context.Context, sessionKey string) (json.RawMessage, bool, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil, false, nil
	}
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	overrides, err := s.readModelOverridesLocked()
	if err != nil {
		return nil, false, err
	}
	raw, ok := overrides[sessionKey]
	if !ok || len(raw) == 0 {
		return nil, false, nil
	}
	return append(json.RawMessage(nil), raw...), true, nil
}

func (s *JSONLStore) SetSessionModelOverride(_ context.Context, sessionKey string, raw json.RawMessage) error {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || len(raw) == 0 {
		return fmt.Errorf("memory: session key and model override are required")
	}
	var validated any
	if err := json.Unmarshal(raw, &validated); err != nil {
		return fmt.Errorf("memory: invalid model override: %w", err)
	}
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	overrides, err := s.readModelOverridesLocked()
	if err != nil {
		return err
	}
	overrides[sessionKey] = append(json.RawMessage(nil), raw...)
	return s.writeModelOverridesLocked(overrides)
}

func (s *JSONLStore) ClearSessionModelOverride(_ context.Context, sessionKey string) error {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	overrides, err := s.readModelOverridesLocked()
	if err != nil {
		return err
	}
	delete(overrides, sessionKey)
	return s.writeModelOverridesLocked(overrides)
}
