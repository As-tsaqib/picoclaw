package session

import (
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/memory"
)

func TestSessionManagerModelOverridePersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	sm := NewSessionManager(dir)
	override := ModelOverride{
		Provider:  "openai",
		Model:     "gpt-test",
		Alias:     "Test",
		ConfigRef: "primary",
		Source:    "configured",
	}
	if setErr := sm.SetModelOverride("session-a", override); setErr != nil {
		t.Fatalf("set override: %v", setErr)
	}
	got, ok, err := sm.GetModelOverride("session-a")
	if err != nil || !ok {
		t.Fatalf("get override: ok=%v err=%v", ok, err)
	}
	if got != override {
		t.Fatalf("override mismatch: got %#v want %#v", got, override)
	}

	reopened := NewSessionManager(dir)
	got, ok, err = reopened.GetModelOverride("session-a")
	if err != nil || !ok || got != override {
		t.Fatalf("reopened override mismatch: got=%#v ok=%v err=%v", got, ok, err)
	}
	if clearErr := reopened.ClearModelOverride("session-a"); clearErr != nil {
		t.Fatalf("clear override: %v", clearErr)
	}
	if _, ok, err := reopened.GetModelOverride("session-a"); err != nil || ok {
		t.Fatalf("override survived clear: ok=%v err=%v", ok, err)
	}
}

func TestJSONLBackendModelOverridePersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.NewJSONLStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	backend := NewJSONLBackend(store)
	override := ModelOverride{Provider: "gemini", Model: "gemini-test", ConfigRef: "google", Source: "discovered"}
	if setErr := backend.SetModelOverride("session-b", override); setErr != nil {
		t.Fatalf("set override: %v", setErr)
	}
	if closeErr := backend.Close(); closeErr != nil {
		t.Fatalf("close backend: %v", closeErr)
	}

	store2, err := memory.NewJSONLStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	backend2 := NewJSONLBackend(store2)
	got, ok, err := backend2.GetModelOverride("session-b")
	if err != nil || !ok || got != override {
		t.Fatalf("reopened override mismatch: got=%#v ok=%v err=%v", got, ok, err)
	}
	if clearErr := backend2.ClearModelOverride("session-b"); clearErr != nil {
		t.Fatalf("clear override: %v", clearErr)
	}
	if _, ok, err := backend2.GetModelOverride("session-b"); err != nil || ok {
		t.Fatalf("override survived clear: ok=%v err=%v", ok, err)
	}
}
