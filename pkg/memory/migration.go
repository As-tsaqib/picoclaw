package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/providers"
)

// jsonSession mirrors pkg/session.Session for migration purposes.
type jsonSession struct {
	Key             string              `json:"key"`
	Name            string              `json:"name,omitempty"`
	NameSource      string              `json:"name_source,omitempty"`
	AutoNamePending bool                `json:"auto_name_pending,omitempty"`
	Messages        []providers.Message `json:"messages"`
	Summary         string              `json:"summary,omitempty"`
	Created         time.Time           `json:"created"`
	Updated         time.Time           `json:"updated"`
	Scope           json.RawMessage     `json:"scope,omitempty"`
	Aliases         []string            `json:"aliases,omitempty"`
}

type migrationMetadataStore interface {
	UpsertSessionMeta(ctx context.Context, sessionKey string, scope json.RawMessage, aliases []string) error
	SetSessionName(ctx context.Context, sessionKey, name, source string, autoNamePending bool) error
}

type migrationActiveSessionStore interface {
	SetActiveSession(ctx context.Context, routeSignature, sessionKey string) error
}

// MigrateFromJSON reads legacy sessions/*.json files from sessionsDir,
// writes them into the Store, and renames each migrated file to
// .json.migrated as a backup. Returns the number of sessions migrated.
//
// Files that fail to parse are logged and skipped. Already-migrated
// files (.json.migrated) are ignored, making the function idempotent.
func MigrateFromJSON(
	ctx context.Context, sessionsDir string, store Store,
) (int, error) {
	entries, err := os.ReadDir(sessionsDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("memory: read sessions dir: %w", err)
	}

	migrated := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		// The durable route mapping is shared by the JSON and JSONL backends;
		// it is metadata, not a legacy conversation snapshot.
		if name == activeSessionsFilename {
			continue
		}
		if name == ".active-sessions-legacy.json" {
			activeStore, ok := store.(migrationActiveSessionStore)
			if !ok {
				continue
			}
			data, readErr := os.ReadFile(filepath.Join(sessionsDir, name))
			if readErr != nil {
				return migrated, fmt.Errorf("memory: migrate active sessions: %w", readErr)
			}
			var mapping map[string]string
			if decodeErr := json.Unmarshal(data, &mapping); decodeErr != nil {
				return migrated, fmt.Errorf("memory: decode active sessions: %w", decodeErr)
			}
			for routeSignature, sessionKey := range mapping {
				if setErr := activeStore.SetActiveSession(ctx, routeSignature, sessionKey); setErr != nil {
					return migrated, fmt.Errorf("memory: migrate active session mapping: %w", setErr)
				}
			}
			if renameErr := os.Rename(
				filepath.Join(sessionsDir, name),
				filepath.Join(sessionsDir, name+".migrated"),
			); renameErr != nil {
				log.Printf("memory: migrate: rename %s: %v", name, renameErr)
			}
			continue
		}
		// Skip JSONL metadata files. They are part of the new storage format,
		// not legacy session snapshots, and re-importing them would overwrite
		// the paired .jsonl history with an empty message list.
		if strings.HasSuffix(name, ".meta.json") {
			continue
		}
		// Skip already-migrated files.
		if strings.HasSuffix(name, ".migrated") {
			continue
		}

		srcPath := filepath.Join(sessionsDir, name)

		data, readErr := os.ReadFile(srcPath)
		if readErr != nil {
			log.Printf("memory: migrate: skip %s: %v", name, readErr)
			continue
		}

		var sess jsonSession
		if parseErr := json.Unmarshal(data, &sess); parseErr != nil {
			log.Printf("memory: migrate: skip %s: %v", name, parseErr)
			continue
		}

		// Use the key from the JSON content, not the filename.
		// Filenames are sanitized (":" → "_") but keys are not.
		key := sess.Key
		if key == "" {
			key = strings.TrimSuffix(name, ".json")
		}

		// Use SetHistory (atomic replace) instead of per-message
		// AddFullMessage. This makes migration idempotent: if the
		// process crashes after writing messages but before the
		// rename below, a retry replaces the partial data cleanly
		// instead of duplicating messages.
		if setErr := store.SetHistory(ctx, key, sess.Messages); setErr != nil {
			return migrated, fmt.Errorf(
				"memory: migrate %s: set history: %w",
				name, setErr,
			)
		}

		if sess.Summary != "" {
			if sumErr := store.SetSummary(ctx, key, sess.Summary); sumErr != nil {
				return migrated, fmt.Errorf(
					"memory: migrate %s: set summary: %w",
					name, sumErr,
				)
			}
		}

		if metadataStore, ok := store.(migrationMetadataStore); ok {
			if len(sess.Scope) > 0 || len(sess.Aliases) > 0 {
				if metaErr := metadataStore.UpsertSessionMeta(ctx, key, sess.Scope, sess.Aliases); metaErr != nil {
					return migrated, fmt.Errorf("memory: migrate %s: metadata: %w", name, metaErr)
				}
			}
			if strings.TrimSpace(sess.Name) != "" {
				if nameErr := metadataStore.SetSessionName(
					ctx,
					key,
					sess.Name,
					sess.NameSource,
					sess.AutoNamePending,
				); nameErr != nil {
					return migrated, fmt.Errorf("memory: migrate %s: session name: %w", name, nameErr)
				}
			}
		}

		// Rename to .migrated as backup (not delete).
		renameErr := os.Rename(srcPath, srcPath+".migrated")
		if renameErr != nil {
			log.Printf("memory: migrate: rename %s: %v", name, renameErr)
		}

		migrated++
	}

	return migrated, nil
}
