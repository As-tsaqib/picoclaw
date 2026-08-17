package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type modelOverrideMetaStore interface {
	GetSessionModelOverride(ctx context.Context, sessionKey string) (json.RawMessage, bool, error)
	SetSessionModelOverride(ctx context.Context, sessionKey string, raw json.RawMessage) error
	ClearSessionModelOverride(ctx context.Context, sessionKey string) error
}

func (b *JSONLBackend) GetModelOverride(sessionKey string) (ModelOverride, bool, error) {
	store, ok := b.store.(modelOverrideMetaStore)
	if !ok {
		return ModelOverride{}, false, fmt.Errorf("session: model override persistence is unavailable")
	}
	sessionKey = b.resolveSessionKey(strings.TrimSpace(sessionKey))
	raw, found, err := store.GetSessionModelOverride(context.Background(), sessionKey)
	if err != nil || !found {
		return ModelOverride{}, false, err
	}
	var override ModelOverride
	if err := json.Unmarshal(raw, &override); err != nil {
		return ModelOverride{}, false, fmt.Errorf("session: decode model override: %w", err)
	}
	override = normalizeModelOverride(override)
	if !override.Valid() {
		return ModelOverride{}, false, nil
	}
	return override, true, nil
}

func (b *JSONLBackend) SetModelOverride(sessionKey string, override ModelOverride) error {
	store, ok := b.store.(modelOverrideMetaStore)
	if !ok {
		return fmt.Errorf("session: model override persistence is unavailable")
	}
	sessionKey = b.resolveSessionKey(strings.TrimSpace(sessionKey))
	override = normalizeModelOverride(override)
	if sessionKey == "" || !override.Valid() {
		return fmt.Errorf("session: session key and valid model override are required")
	}
	raw, err := json.Marshal(override)
	if err != nil {
		return fmt.Errorf("session: encode model override: %w", err)
	}
	return store.SetSessionModelOverride(context.Background(), sessionKey, raw)
}

func (b *JSONLBackend) ClearModelOverride(sessionKey string) error {
	store, ok := b.store.(modelOverrideMetaStore)
	if !ok {
		return fmt.Errorf("session: model override persistence is unavailable")
	}
	sessionKey = b.resolveSessionKey(strings.TrimSpace(sessionKey))
	return store.ClearSessionModelOverride(context.Background(), sessionKey)
}

var _ ModelOverrideStore = (*JSONLBackend)(nil)
