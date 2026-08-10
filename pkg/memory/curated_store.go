package memory

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

const curatedDocumentVersion = 1

type CuratedStoreOptions struct {
	WorkspaceCharLimit int
	PerUserCharLimit   int
	Now                func() time.Time
	Random             io.Reader
}

type curatedDocument struct {
	Version     int                    `json:"version"`
	ScopeDigest string                 `json:"scope_digest"`
	Entries     []CuratedEntry         `json:"entries"`
	Pending     []PendingCuratedChange `json:"pending,omitempty"`
}

// CuratedStore owns structured workspace and per-user memory for one agent
// workspace. All read/modify/write operations are serialized and mutations are
// written atomically with owner-only permissions.
type CuratedStore struct {
	root               string
	usersDir           string
	workspaceCharLimit int
	perUserCharLimit   int
	now                func() time.Time
	random             io.Reader
	mu                 sync.RWMutex
}

func NewCuratedStore(root string, opts CuratedStoreOptions) (*CuratedStore, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil, fmt.Errorf("curated memory root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create curated memory directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure curated memory directory: %w", err)
	}
	usersDir := filepath.Join(root, "users")
	if err := os.MkdirAll(usersDir, 0o700); err != nil {
		return nil, fmt.Errorf("create curated user memory directory: %w", err)
	}
	if err := os.Chmod(usersDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure curated user memory directory: %w", err)
	}
	if opts.WorkspaceCharLimit <= 0 {
		opts.WorkspaceCharLimit = 12_000
	}
	if opts.PerUserCharLimit <= 0 {
		opts.PerUserCharLimit = 8_000
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Random == nil {
		opts.Random = rand.Reader
	}
	return &CuratedStore{
		root:               root,
		usersDir:           usersDir,
		workspaceCharLimit: opts.WorkspaceCharLimit,
		perUserCharLimit:   opts.PerUserCharLimit,
		now:                opts.Now,
		random:             opts.Random,
	}, nil
}

func (s *CuratedStore) scopePath(target string, caller CallerScope) (string, string, int, error) {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case CuratedTargetWorkspace:
		return filepath.Join(s.root, "workspace.json"), "workspace", s.workspaceCharLimit, nil
	case CuratedTargetCurrentUser:
		userKey := strings.TrimSpace(caller.UserKey)
		if userKey == "" {
			return "", "", 0, ErrUserScopeUnavailable
		}
		if len(userKey) > 1_024 || strings.ContainsRune(userKey, '\x00') {
			return "", "", 0, ErrUserScopeUnavailable
		}
		digest := sha256.Sum256([]byte(userKey))
		hexDigest := hex.EncodeToString(digest[:])
		name := "user_" + hexDigest + ".json"
		if !filepath.IsLocal(name) {
			return "", "", 0, ErrUserScopeUnavailable
		}
		return filepath.Join(s.usersDir, name), hexDigest, s.perUserCharLimit, nil
	default:
		return "", "", 0, ErrCuratedInvalidTarget
	}
}

func (s *CuratedStore) readDocument(path, digest string) (curatedDocument, error) {
	doc := curatedDocument{
		Version:     curatedDocumentVersion,
		ScopeDigest: digest,
		Entries:     []CuratedEntry{},
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return curatedDocument{}, fmt.Errorf("read curated memory: %w", err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return curatedDocument{}, fmt.Errorf("decode curated memory: %w", err)
	}
	if doc.Version == 0 {
		doc.Version = curatedDocumentVersion
	}
	if doc.ScopeDigest != "" && doc.ScopeDigest != digest {
		return curatedDocument{}, fmt.Errorf("curated memory scope mismatch")
	}
	doc.ScopeDigest = digest
	if doc.Entries == nil {
		doc.Entries = []CuratedEntry{}
	}
	return doc, nil
}

func (s *CuratedStore) writeDocument(path string, doc curatedDocument) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode curated memory: %w", err)
	}
	if err := fileutil.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write curated memory: %w", err)
	}
	return os.Chmod(path, 0o600)
}

func (s *CuratedStore) ApplyBatch(
	target string,
	caller CallerScope,
	mutations []CuratedMutation,
	stage bool,
) (CuratedBatchResult, error) {
	if len(mutations) == 0 {
		return CuratedBatchResult{}, ErrCuratedInvalidAction
	}
	path, digest, limit, err := s.scopePath(target, caller)
	if err != nil {
		return CuratedBatchResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDocument(path, digest)
	if err != nil {
		return CuratedBatchResult{}, err
	}
	now := s.now().UTC()
	prepared, err := s.prepareMutations(doc, mutations, caller, now)
	if err != nil {
		return CuratedBatchResult{}, err
	}

	projected := cloneCuratedEntries(doc.Entries)
	for _, pending := range doc.Pending {
		projected, _, err = applyCuratedMutations(projected, pending.Mutations, now)
		if err != nil {
			return CuratedBatchResult{}, fmt.Errorf("invalid pending curated memory state: %w", err)
		}
	}
	projected, applied, err := applyCuratedMutations(projected, prepared, now)
	if err != nil {
		return CuratedBatchResult{}, err
	}
	if err := enforceCuratedCapacity(target, doc.Entries, projected, limit); err != nil {
		return CuratedBatchResult{}, err
	}

	if stage {
		pendingID, err := s.newStableID("pm", pendingIDs(doc.Pending))
		if err != nil {
			return CuratedBatchResult{}, err
		}
		pending := PendingCuratedChange{ID: pendingID, Mutations: prepared, CreatedAt: now}
		doc.Pending = append(doc.Pending, pending)
		if err := s.writeDocument(path, doc); err != nil {
			return CuratedBatchResult{}, err
		}
		return CuratedBatchResult{Pending: &pending}, nil
	}

	// Re-apply only the requested batch to the actual entries. Pending batches
	// remain staged and do not silently become visible.
	doc.Entries, applied, err = applyCuratedMutations(cloneCuratedEntries(doc.Entries), prepared, now)
	if err != nil {
		return CuratedBatchResult{}, err
	}
	if err := enforceCuratedCapacity(target, nil, doc.Entries, limit); err != nil {
		return CuratedBatchResult{}, err
	}
	if err := s.writeDocument(path, doc); err != nil {
		return CuratedBatchResult{}, err
	}
	return CuratedBatchResult{Applied: applied}, nil
}

func (s *CuratedStore) prepareMutations(
	doc curatedDocument,
	mutations []CuratedMutation,
	caller CallerScope,
	now time.Time,
) ([]CuratedMutation, error) {
	known := make(map[string]struct{}, len(doc.Entries)+len(mutations))
	for _, entry := range doc.Entries {
		known[entry.ID] = struct{}{}
	}
	prepared := make([]CuratedMutation, 0, len(mutations))
	for _, mutation := range mutations {
		mutation.Action = strings.ToLower(strings.TrimSpace(mutation.Action))
		mutation.ID = strings.TrimSpace(mutation.ID)
		switch mutation.Action {
		case CuratedActionAdd:
			mutation.Content = strings.TrimSpace(mutation.Content)
			if err := ValidateCuratedContent(mutation.Content); err != nil {
				return nil, err
			}
			if mutation.ID == "" {
				id, err := s.newStableID("mem", known)
				if err != nil {
					return nil, err
				}
				mutation.ID = id
			}
			if !validStableEntryID(mutation.ID) {
				return nil, ErrCuratedInvalidAction
			}
			if _, exists := known[mutation.ID]; exists {
				return nil, ErrCuratedDuplicate
			}
			known[mutation.ID] = struct{}{}
		case CuratedActionReplace:
			if !validStableEntryID(mutation.ID) {
				return nil, ErrCuratedInvalidAction
			}
			mutation.Content = strings.TrimSpace(mutation.Content)
			if err := ValidateCuratedContent(mutation.Content); err != nil {
				return nil, err
			}
		case CuratedActionRemove:
			if !validStableEntryID(mutation.ID) {
				return nil, ErrCuratedInvalidAction
			}
			mutation.Content = ""
		default:
			return nil, ErrCuratedInvalidAction
		}
		if mutation.Provenance.Source == "" {
			mutation.Provenance.Source = "agent"
		}
		if mutation.Provenance.SessionRef == "" {
			mutation.Provenance.SessionRef = caller.SessionRef
		}
		if mutation.Provenance.Channel == "" {
			mutation.Provenance.Channel = caller.Channel
		}
		if mutation.Provenance.Account == "" {
			mutation.Provenance.Account = caller.Account
		}
		if mutation.Provenance.TopicID == "" {
			mutation.Provenance.TopicID = caller.TopicID
		}
		if mutation.Provenance.TopicName == "" {
			mutation.Provenance.TopicName = caller.TopicName
		}
		if mutation.Provenance.MessageRef == "" {
			mutation.Provenance.MessageRef = caller.MessageRef
		}
		mutation.Provenance.RecordedAt = now
		prepared = append(prepared, mutation)
	}
	return prepared, nil
}

func applyCuratedMutations(
	entries []CuratedEntry,
	mutations []CuratedMutation,
	now time.Time,
) ([]CuratedEntry, []CuratedEntry, error) {
	applied := make([]CuratedEntry, 0, len(mutations))
	for _, mutation := range mutations {
		idx := -1
		for i := range entries {
			if entries[i].ID == mutation.ID {
				idx = i
				break
			}
		}
		switch mutation.Action {
		case CuratedActionAdd:
			if idx >= 0 {
				return nil, nil, ErrCuratedDuplicate
			}
			if duplicateCuratedContent(entries, mutation.Content, "") {
				return nil, nil, ErrCuratedDuplicate
			}
			entry := CuratedEntry{
				ID:         mutation.ID,
				Content:    mutation.Content,
				Provenance: mutation.Provenance,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			entries = append(entries, entry)
			applied = append(applied, entry)
		case CuratedActionReplace:
			if idx < 0 {
				return nil, nil, ErrCuratedEntryNotFound
			}
			if duplicateCuratedContent(entries, mutation.Content, mutation.ID) {
				return nil, nil, ErrCuratedDuplicate
			}
			entries[idx].Content = mutation.Content
			entries[idx].Provenance = mutation.Provenance
			entries[idx].UpdatedAt = now
			applied = append(applied, entries[idx])
		case CuratedActionRemove:
			if idx < 0 {
				return nil, nil, ErrCuratedEntryNotFound
			}
			removed := entries[idx]
			entries = append(entries[:idx], entries[idx+1:]...)
			applied = append(applied, removed)
		default:
			return nil, nil, ErrCuratedInvalidAction
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})
	return entries, applied, nil
}

func enforceCuratedCapacity(target string, current, projected []CuratedEntry, limit int) error {
	currentChars := curatedCharacters(current)
	projectedChars := curatedCharacters(projected)
	if projectedChars <= limit {
		return nil
	}
	return &CapacityError{
		Target:    target,
		Limit:     limit,
		Current:   currentChars,
		Requested: projectedChars - currentChars,
	}
}

func (s *CuratedStore) List(target string, caller CallerScope) ([]CuratedEntry, error) {
	path, digest, _, err := s.scopePath(target, caller)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, err := s.readDocument(path, digest)
	if err != nil {
		return nil, err
	}
	entries := cloneCuratedEntries(doc.Entries)
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
	})
	return entries, nil
}

func (s *CuratedStore) Search(target string, caller CallerScope, query string, limit int) ([]CuratedEntry, error) {
	entries, err := s.List(target, caller)
	if err != nil {
		return nil, err
	}
	queryTokens := lexicalTokens(query)
	if len(queryTokens) == 0 {
		return []CuratedEntry{}, nil
	}
	type scored struct {
		entry CuratedEntry
		score int
	}
	results := make([]scored, 0)
	for _, entry := range entries {
		contentTokens := lexicalTokenCounts(entry.Content)
		score := 0
		for token := range queryTokens {
			score += contentTokens[token]
		}
		if score > 0 {
			results = append(results, scored{entry: entry, score: score})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].entry.UpdatedAt.After(results[j].entry.UpdatedAt)
	})
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if len(results) > limit {
		results = results[:limit]
	}
	out := make([]CuratedEntry, len(results))
	for i := range results {
		out[i] = results[i].entry
	}
	return out, nil
}

func (s *CuratedStore) Pending(target string, caller CallerScope) ([]PendingCuratedChange, error) {
	path, digest, _, err := s.scopePath(target, caller)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, err := s.readDocument(path, digest)
	if err != nil {
		return nil, err
	}
	return clonePendingChanges(doc.Pending), nil
}

func (s *CuratedStore) Approve(target string, caller CallerScope, id string) ([]CuratedEntry, error) {
	return s.resolvePending(target, caller, id, true)
}

func (s *CuratedStore) Reject(target string, caller CallerScope, id string) ([]CuratedEntry, error) {
	return s.resolvePending(target, caller, id, false)
}

func (s *CuratedStore) resolvePending(
	target string,
	caller CallerScope,
	id string,
	approve bool,
) ([]CuratedEntry, error) {
	path, digest, limit, err := s.scopePath(target, caller)
	if err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrCuratedInvalidPending
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDocument(path, digest)
	if err != nil {
		return nil, err
	}
	selected := make([]PendingCuratedChange, 0)
	remaining := make([]PendingCuratedChange, 0, len(doc.Pending))
	for _, pending := range doc.Pending {
		if id == "all" || pending.ID == id {
			selected = append(selected, pending)
		} else {
			remaining = append(remaining, pending)
		}
	}
	if len(selected) == 0 {
		return nil, ErrCuratedInvalidPending
	}
	var applied []CuratedEntry
	if approve {
		projected := cloneCuratedEntries(doc.Entries)
		for _, pending := range selected {
			var batchApplied []CuratedEntry
			projected, batchApplied, err = applyCuratedMutations(projected, pending.Mutations, s.now().UTC())
			if err != nil {
				return nil, err
			}
			applied = append(applied, batchApplied...)
		}
		if err := enforceCuratedCapacity(target, doc.Entries, projected, limit); err != nil {
			return nil, err
		}
		doc.Entries = projected
	}
	doc.Pending = remaining
	if err := s.writeDocument(path, doc); err != nil {
		return nil, err
	}
	return applied, nil
}

func (s *CuratedStore) Stats(target string, caller CallerScope) (CuratedStats, error) {
	path, digest, limit, err := s.scopePath(target, caller)
	if err != nil {
		return CuratedStats{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, err := s.readDocument(path, digest)
	if err != nil {
		return CuratedStats{}, err
	}
	return CuratedStats{
		Target:       target,
		Entries:      len(doc.Entries),
		Characters:   curatedCharacters(doc.Entries),
		Capacity:     limit,
		PendingCount: len(doc.Pending),
	}, nil
}

func (s *CuratedStore) newStableID(prefix string, known map[string]struct{}) (string, error) {
	for range 8 {
		var raw [8]byte
		if _, err := io.ReadFull(s.random, raw[:]); err != nil {
			return "", fmt.Errorf("generate curated memory id: %w", err)
		}
		id := prefix + "_" + hex.EncodeToString(raw[:])
		if _, exists := known[id]; !exists {
			return id, nil
		}
	}
	return "", fmt.Errorf("generate unique curated memory id")
}

func validStableEntryID(id string) bool {
	if !strings.HasPrefix(id, "mem_") || len(id) != len("mem_")+16 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, "mem_"))
	return err == nil
}

func duplicateCuratedContent(entries []CuratedEntry, content, exceptID string) bool {
	normalized := normalizeCuratedContent(content)
	for _, entry := range entries {
		if entry.ID != exceptID && normalizeCuratedContent(entry.Content) == normalized {
			return true
		}
	}
	return false
}

func curatedCharacters(entries []CuratedEntry) int {
	total := 0
	for _, entry := range entries {
		total += utf8.RuneCountInString(entry.Content)
	}
	return total
}

func cloneCuratedEntries(entries []CuratedEntry) []CuratedEntry {
	return append([]CuratedEntry(nil), entries...)
}

func clonePendingChanges(changes []PendingCuratedChange) []PendingCuratedChange {
	out := make([]PendingCuratedChange, len(changes))
	for i := range changes {
		out[i] = changes[i]
		out[i].Mutations = append([]CuratedMutation(nil), changes[i].Mutations...)
	}
	return out
}

func pendingIDs(changes []PendingCuratedChange) map[string]struct{} {
	ids := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		ids[change.ID] = struct{}{}
	}
	return ids
}
