package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMigrateFromJSONSkipsModelOverridesSidecar(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	want := json.RawMessage(`{"provider":"openai","model":"gpt-4.1","config_ref":"primary"}`)
	if setErr := store.SetSessionModelOverride(ctx, "session-a", want); setErr != nil {
		t.Fatal(setErr)
	}
	path := filepath.Join(dir, modelOverridesFilename)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	count, err := MigrateFromJSON(ctx, dir, store)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("migrated %d sidecar files, want 0", count)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("sidecar changed during migration: before=%s after=%s", before, after)
	}
	if _, statErr := os.Stat(path + ".migrated"); !os.IsNotExist(statErr) {
		t.Fatalf("sidecar was renamed: %v", statErr)
	}
	got, ok, readErr := store.GetSessionModelOverride(ctx, "session-a")
	if readErr != nil || !ok {
		t.Fatalf("override after migration missing: ok=%v, err=%v", ok, readErr)
	}
	var gotValue, wantValue any
	if err = json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode migrated override: %v", err)
	}
	if err = json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("decode expected override: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("override after migration = %s, want semantic JSON %s", got, want)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("migration created bogus histories: %v", files)
	}
}
