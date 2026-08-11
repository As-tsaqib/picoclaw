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

const curatedDocumentVersion = 2

var curatedDocumentLocks sync.Map

type CuratedStoreOptions struct {
	WorkspaceCharLimit int
	PerUserCharLimit   int
	Now                func() time.Time
	Random             io.Reader
}

type curatedDocument struct {
	Version     int                    `json:"version"`
	Revision    uint64                 `json:"revision,omitempty"`
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
	profileCache       sync.Map
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
	for i := range doc.Entries {
		doc.Entries[i] = normalizedCuratedEntry(doc.Entries[i])
	}
	return doc, nil
}

func (s *CuratedStore) writeDocument(path string, doc curatedDocument) error {
	doc.Version = curatedDocumentVersion
	doc.Revision++
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
	path, digest, limit, scopeErr := s.scopePath(target, caller)
	if scopeErr != nil {
		return CuratedBatchResult{}, scopeErr
	}

	unlockDocument, lockErr := lockCuratedDocument(path)
	if lockErr != nil {
		return CuratedBatchResult{}, lockErr
	}
	defer unlockDocument()
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, readErr := s.readDocument(path, digest)
	if readErr != nil {
		return CuratedBatchResult{}, readErr
	}
	now := s.now().UTC()
	prepared, prepareErr := s.prepareMutations(target, doc, mutations, caller, now)
	if prepareErr != nil {
		return CuratedBatchResult{}, prepareErr
	}
	conflicts := findCuratedConflicts(doc.Entries, prepared)

	projected := cloneCuratedEntries(doc.Entries)
	for _, pending := range doc.Pending {
		var mutationErr error
		projected, _, mutationErr = applyCuratedMutations(projected, pending.Mutations, now)
		if mutationErr != nil {
			return CuratedBatchResult{}, fmt.Errorf("invalid pending curated memory state: %w", mutationErr)
		}
	}
	projected, _, projectionErr := applyCuratedMutations(projected, prepared, now)
	if projectionErr != nil {
		return CuratedBatchResult{}, projectionErr
	}
	if capacityErr := enforceCuratedCapacity(
		target,
		doc.Entries,
		projected,
		limit,
	); capacityErr != nil {
		return CuratedBatchResult{}, capacityErr
	}

	if stage {
		pendingID, idErr := s.newStableID("pm", pendingIDs(doc.Pending))
		if idErr != nil {
			return CuratedBatchResult{}, idErr
		}
		pending := PendingCuratedChange{ID: pendingID, Mutations: prepared, CreatedAt: now}
		doc.Pending = append(doc.Pending, pending)
		if writeErr := s.writeDocument(path, doc); writeErr != nil {
			return CuratedBatchResult{}, writeErr
		}
		return CuratedBatchResult{Pending: &pending, Conflicts: conflicts}, nil
	}

	// Re-apply only the requested batch to the actual entries. Pending batches
	// remain staged and do not silently become visible.
	entries, applied, applyErr := applyCuratedMutations(cloneCuratedEntries(doc.Entries), prepared, now)
	if applyErr != nil {
		return CuratedBatchResult{}, applyErr
	}
	doc.Entries = entries
	if capacityErr := enforceCuratedCapacity(
		target,
		nil,
		doc.Entries,
		limit,
	); capacityErr != nil {
		return CuratedBatchResult{}, capacityErr
	}
	if writeErr := s.writeDocument(path, doc); writeErr != nil {
		return CuratedBatchResult{}, writeErr
	}
	return CuratedBatchResult{Applied: applied, Conflicts: conflicts}, nil
}

func (s *CuratedStore) prepareMutations(
	target string,
	doc curatedDocument,
	mutations []CuratedMutation,
	caller CallerScope,
	now time.Time,
) ([]CuratedMutation, error) {
	known := make(map[string]struct{}, len(doc.Entries)+len(mutations))
	knownStatus := make(map[string]string, len(doc.Entries)+len(mutations))
	knownPreferenceKey := make(map[string]string, len(doc.Entries)+len(mutations))
	for _, entry := range doc.Entries {
		known[entry.ID] = struct{}{}
		knownStatus[entry.ID] = entry.EffectiveStatus()
		knownPreferenceKey[entry.ID] = NormalizePreferenceKey(entry.PreferenceKey)
	}
	prepared := make([]CuratedMutation, 0, len(mutations))
	for _, mutation := range mutations {
		mutation.Action = strings.ToLower(strings.TrimSpace(mutation.Action))
		mutation.ID = strings.TrimSpace(mutation.ID)
		mutation.Type = strings.ToLower(strings.TrimSpace(mutation.Type))
		mutation.EvidenceKind = NormalizeEvidenceKind(mutation.EvidenceKind)
		mutation.PreferenceKey = NormalizePreferenceKey(mutation.PreferenceKey)
		mutation.PreferenceValue = strings.TrimSpace(mutation.PreferenceValue)
		mutation.Supersedes = strings.TrimSpace(mutation.Supersedes)
		if mutation.Action == CuratedActionAdd && mutation.EvidenceKind == "" {
			// Missing evidence is deliberately conservative. Old callers and the
			// background curator must not silently create fully verified facts.
			mutation.EvidenceKind = CuratedEvidenceInferred
		}
		if mutation.EvidenceKind != "" && !ValidEvidenceKind(mutation.EvidenceKind) {
			return nil, ErrCuratedInvalidEvidence
		}
		if mutation.PreferenceKey != "" {
			if !ValidPreferenceKey(mutation.PreferenceKey) ||
				!preferenceKeyAllowedForTarget(target, mutation.PreferenceKey) {
				return nil, ErrCuratedInvalidPreferenceKey
			}
			if mutation.PreferenceValue == "" || utf8.RuneCountInString(mutation.PreferenceValue) > 240 {
				return nil, ErrCuratedInvalidPreferenceKey
			}
		}
		if mutation.EvidenceCount < 0 || mutation.ObservationCount < 0 {
			return nil, ErrCuratedInvalidAction
		}
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
			if mutation.Type == "" {
				mutation.Type = CuratedTypeOther
			}
			if !ValidCuratedType(mutation.Type) {
				return nil, ErrCuratedInvalidType
			}
			if !curatedTypeAllowedForTarget(target, mutation.Type) {
				return nil, ErrCuratedInvalidTarget
			}
			if err := validateCuratedTargetContent(target, mutation.Type, mutation.Content); err != nil {
				return nil, err
			}
			if mutation.Confidence != nil && (*mutation.Confidence <= 0 || *mutation.Confidence > 1) {
				return nil, ErrCuratedInvalidAction
			}
			if mutation.Supersedes != "" {
				if !validStableEntryID(mutation.Supersedes) {
					return nil, ErrCuratedInvalidAction
				}
				if _, exists := known[mutation.Supersedes]; !exists {
					return nil, ErrCuratedEntryNotFound
				}
				if status := knownStatus[mutation.Supersedes]; status != CuratedStatusActive {
					return nil, ErrCuratedInvalidAction
				}
				if priorKey := knownPreferenceKey[mutation.Supersedes]; mutation.PreferenceKey != "" &&
					priorKey != "" && priorKey != mutation.PreferenceKey {
					return nil, ErrCuratedInvalidPreferenceKey
				}
			}
			if mutation.ExpiresAt != nil && !mutation.ExpiresAt.After(now) {
				return nil, ErrCuratedInvalidAction
			}
			known[mutation.ID] = struct{}{}
			knownStatus[mutation.ID] = CuratedStatusActive
			knownPreferenceKey[mutation.ID] = mutation.PreferenceKey
			if mutation.Supersedes != "" {
				knownStatus[mutation.Supersedes] = CuratedStatusSuperseded
			}
		case CuratedActionReplace:
			if !validStableEntryID(mutation.ID) {
				return nil, ErrCuratedInvalidAction
			}
			mutation.Content = strings.TrimSpace(mutation.Content)
			if err := ValidateCuratedContent(mutation.Content); err != nil {
				return nil, err
			}
			if mutation.Type != "" {
				if !ValidCuratedType(mutation.Type) {
					return nil, ErrCuratedInvalidType
				}
				if !curatedTypeAllowedForTarget(target, mutation.Type) {
					return nil, ErrCuratedInvalidTarget
				}
			}
			effectiveType := mutation.Type
			if effectiveType == "" {
				for _, entry := range doc.Entries {
					if entry.ID == mutation.ID {
						effectiveType = entry.EffectiveType()
						break
					}
				}
			}
			if err := validateCuratedTargetContent(target, effectiveType, mutation.Content); err != nil {
				return nil, err
			}
			if mutation.Confidence != nil && (*mutation.Confidence <= 0 || *mutation.Confidence > 1) {
				return nil, ErrCuratedInvalidAction
			}
			effectivePreferenceKey := mutation.PreferenceKey
			if effectivePreferenceKey == "" {
				effectivePreferenceKey = knownPreferenceKey[mutation.ID]
			}
			if mutation.Supersedes != "" && mutation.Supersedes != mutation.ID {
				if !validStableEntryID(mutation.Supersedes) {
					return nil, ErrCuratedInvalidAction
				}
				if _, exists := known[mutation.Supersedes]; !exists {
					return nil, ErrCuratedEntryNotFound
				}
				if status := knownStatus[mutation.Supersedes]; status != CuratedStatusActive {
					return nil, ErrCuratedInvalidAction
				}
				if priorKey := knownPreferenceKey[mutation.Supersedes]; effectivePreferenceKey != "" &&
					priorKey != "" && priorKey != effectivePreferenceKey {
					return nil, ErrCuratedInvalidPreferenceKey
				}
				knownStatus[mutation.Supersedes] = CuratedStatusSuperseded
			}
			knownPreferenceKey[mutation.ID] = effectivePreferenceKey
		case CuratedActionRemove,
			CuratedActionPin,
			CuratedActionUnpin,
			CuratedActionArchive,
			CuratedActionRestore:
			if !validStableEntryID(mutation.ID) {
				return nil, ErrCuratedInvalidAction
			}
			status, exists := knownStatus[mutation.ID]
			if !exists {
				return nil, ErrCuratedEntryNotFound
			}
			if mutation.Action == CuratedActionPin && status != CuratedStatusActive {
				return nil, ErrCuratedInvalidAction
			}
			if mutation.Action == CuratedActionRestore && status != CuratedStatusArchived {
				return nil, ErrCuratedInvalidAction
			}
			switch mutation.Action {
			case CuratedActionRemove:
				delete(knownStatus, mutation.ID)
			case CuratedActionArchive:
				knownStatus[mutation.ID] = CuratedStatusArchived
			case CuratedActionRestore:
				knownStatus[mutation.ID] = CuratedStatusActive
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
			evidence := NormalizeEvidenceKind(mutation.EvidenceKind)
			entry := CuratedEntry{
				ID:               mutation.ID,
				Content:          mutation.Content,
				Type:             NormalizeCuratedType(mutation.Type),
				Status:           CuratedStatusActive,
				Confidence:       DefaultConfidenceForEvidence(evidence),
				EvidenceKind:     evidence,
				EvidenceCount:    mutation.EvidenceCount,
				ObservationCount: mutation.ObservationCount,
				PreferenceKey:    NormalizePreferenceKey(mutation.PreferenceKey),
				PreferenceValue:  strings.TrimSpace(mutation.PreferenceValue),
				Supersedes:       mutation.Supersedes,
				Provenance:       mutation.Provenance,
				CreatedAt:        now,
				UpdatedAt:        now,
				LastVerifiedAt:   mutation.LastVerifiedAt,
				LastConfirmedAt:  mutation.LastConfirmedAt,
				ExpiresAt:        mutation.ExpiresAt,
			}
			if mutation.Confidence != nil {
				entry.Confidence = *mutation.Confidence
			}
			if entry.EvidenceCount == 0 {
				entry.EvidenceCount = 1
			}
			if evidence == CuratedEvidenceObserved && entry.ObservationCount == 0 {
				entry.ObservationCount = entry.EvidenceCount
			}
			if entry.LastConfirmedAt == nil && entry.LastVerifiedAt != nil {
				entry.LastConfirmedAt = entry.LastVerifiedAt
			}
			if evidence == CuratedEvidenceExplicit && entry.LastConfirmedAt == nil {
				confirmed := now
				entry.LastConfirmedAt = &confirmed
				entry.LastVerifiedAt = &confirmed
			}
			if mutation.Supersedes != "" {
				markCuratedSuperseded(entries, mutation.Supersedes, now)
			}
			entries = append(entries, normalizedCuratedEntry(entry))
			reconcilePreferenceKey(entries, mutation.ID, now)
			if current, ok := curatedEntryByID(entries, mutation.ID); ok {
				applied = append(applied, current)
			}
		case CuratedActionReplace:
			if idx < 0 {
				return nil, nil, ErrCuratedEntryNotFound
			}
			if duplicateCuratedContent(entries, mutation.Content, mutation.ID) {
				return nil, nil, ErrCuratedDuplicate
			}
			entries[idx].Content = mutation.Content
			if mutation.Type != "" {
				entries[idx].Type = NormalizeCuratedType(mutation.Type)
			}
			if mutation.EvidenceKind != "" {
				entries[idx].EvidenceKind = NormalizeEvidenceKind(mutation.EvidenceKind)
			}
			if mutation.Confidence != nil {
				entries[idx].Confidence = *mutation.Confidence
			} else if mutation.EvidenceKind != "" {
				entries[idx].Confidence = DefaultConfidenceForEvidence(entries[idx].EvidenceKind)
			}
			if mutation.EvidenceCount > 0 {
				entries[idx].EvidenceCount = mutation.EvidenceCount
			}
			if mutation.ObservationCount > 0 {
				entries[idx].ObservationCount = mutation.ObservationCount
			}
			if mutation.PreferenceKey != "" {
				entries[idx].PreferenceKey = NormalizePreferenceKey(mutation.PreferenceKey)
				entries[idx].PreferenceValue = strings.TrimSpace(mutation.PreferenceValue)
			}
			if mutation.ExpiresAt != nil {
				entries[idx].ExpiresAt = mutation.ExpiresAt
			}
			if mutation.LastConfirmedAt != nil {
				entries[idx].LastConfirmedAt = mutation.LastConfirmedAt
				entries[idx].LastVerifiedAt = mutation.LastConfirmedAt
			} else if mutation.LastVerifiedAt != nil {
				entries[idx].LastVerifiedAt = mutation.LastVerifiedAt
				entries[idx].LastConfirmedAt = mutation.LastVerifiedAt
			} else if NormalizeEvidenceKind(mutation.EvidenceKind) == CuratedEvidenceExplicit {
				// Only a mutation that explicitly carries direct-user evidence refreshes
				// confirmation. A curator rewrite of an existing explicit memory does not.
				confirmed := now
				entries[idx].LastConfirmedAt = &confirmed
				entries[idx].LastVerifiedAt = &confirmed
			} else if mutation.EvidenceKind != "" {
				entries[idx].LastConfirmedAt = nil
				entries[idx].LastVerifiedAt = nil
			}
			if mutation.Supersedes != "" && mutation.Supersedes != mutation.ID {
				entries[idx].Supersedes = mutation.Supersedes
				markCuratedSuperseded(entries, mutation.Supersedes, now)
			}
			entries[idx].Provenance = mutation.Provenance
			entries[idx].UpdatedAt = now
			entries[idx] = normalizedCuratedEntry(entries[idx])
			reconcilePreferenceKey(entries, mutation.ID, now)
			if current, ok := curatedEntryByID(entries, mutation.ID); ok {
				applied = append(applied, current)
			}
		case CuratedActionRemove:
			if idx < 0 {
				return nil, nil, ErrCuratedEntryNotFound
			}
			removed := entries[idx]
			entries = append(entries[:idx], entries[idx+1:]...)
			applied = append(applied, removed)
		case CuratedActionPin, CuratedActionUnpin:
			if idx < 0 {
				return nil, nil, ErrCuratedEntryNotFound
			}
			entries[idx].Pinned = mutation.Action == CuratedActionPin
			entries[idx].UpdatedAt = now
			entries[idx].Provenance = mutation.Provenance
			entries[idx] = normalizedCuratedEntry(entries[idx])
			applied = append(applied, entries[idx])
		case CuratedActionArchive:
			if idx < 0 {
				return nil, nil, ErrCuratedEntryNotFound
			}
			entries[idx].Status = CuratedStatusArchived
			archived := now
			entries[idx].ArchivedAt = &archived
			entries[idx].UpdatedAt = now
			entries[idx].Provenance = mutation.Provenance
			entries[idx] = normalizedCuratedEntry(entries[idx])
			applied = append(applied, entries[idx])
		case CuratedActionRestore:
			if idx < 0 {
				return nil, nil, ErrCuratedEntryNotFound
			}
			entries[idx].Status = CuratedStatusActive
			entries[idx].ArchivedAt = nil
			entries[idx].UpdatedAt = now
			entries[idx].Provenance = mutation.Provenance
			// Restoring a structured preference may also be a deliberate
			// reaffirmation. Only apply an evidence override when the caller
			// supplied one; background/generic restores otherwise preserve the
			// original authority and confirmation time.
			if NormalizePreferenceKey(entries[idx].PreferenceKey) != "" && strings.TrimSpace(mutation.EvidenceKind) != "" {
				entries[idx].EvidenceKind = NormalizeEvidenceKind(mutation.EvidenceKind)
				confidence := DefaultConfidenceForEvidence(entries[idx].EvidenceKind)
				if mutation.Confidence != nil {
					confidence = *mutation.Confidence
				}
				entries[idx].Confidence = normalizeConfidenceForEvidence(entries[idx].EvidenceKind, confidence)
				if entries[idx].EvidenceKind == CuratedEvidenceExplicit {
					confirmed := now
					entries[idx].LastConfirmedAt = &confirmed
					entries[idx].LastVerifiedAt = &confirmed
				} else {
					entries[idx].LastConfirmedAt = nil
					entries[idx].LastVerifiedAt = nil
				}
			}
			entries[idx] = normalizedCuratedEntry(entries[idx])
			reconcilePreferenceKey(entries, mutation.ID, now)
			if current, ok := curatedEntryByID(entries, mutation.ID); ok {
				applied = append(applied, current)
			}
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
	unlockDocument, lockErr := lockCuratedDocument(path)
	if lockErr != nil {
		return nil, lockErr
	}
	defer unlockDocument()
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, err := s.readDocument(path, digest)
	if err != nil {
		return nil, err
	}
	entries := cloneCuratedEntries(doc.Entries)
	for i := range entries {
		entries[i] = normalizedCuratedEntry(entries[i])
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].UpdatedAt.Equal(entries[j].UpdatedAt) {
			return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

func (s *CuratedStore) Inspect(target string, caller CallerScope, id string) (CuratedEntry, error) {
	if !validStableEntryID(strings.TrimSpace(id)) {
		return CuratedEntry{}, ErrCuratedInvalidAction
	}
	entries, err := s.List(target, caller)
	if err != nil {
		return CuratedEntry{}, err
	}
	for _, entry := range entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return CuratedEntry{}, ErrCuratedEntryNotFound
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
	unlockDocument, lockErr := lockCuratedDocument(path)
	if lockErr != nil {
		return nil, lockErr
	}
	defer unlockDocument()
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
	if id != "all" && !validStablePendingID(id) {
		return nil, ErrCuratedInvalidPending
	}
	unlockDocument, lockErr := lockCuratedDocument(path)
	if lockErr != nil {
		return nil, lockErr
	}
	defer unlockDocument()
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
	unlockDocument, lockErr := lockCuratedDocument(path)
	if lockErr != nil {
		return CuratedStats{}, lockErr
	}
	defer unlockDocument()
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

// MarkPresented records that a memory was included in a successfully delivered
// prompt. Presentation is intentionally weaker than confirmation and receives
// only a small retrieval bonus.
func (s *CuratedStore) MarkPresented(
	target string,
	caller CallerScope,
	ids []string,
	presentedAt time.Time,
) error {
	path, digest, _, err := s.scopePath(target, caller)
	if err != nil {
		return err
	}
	targetIDs := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !validStableEntryID(id) {
			return ErrCuratedInvalidAction
		}
		targetIDs[id] = struct{}{}
	}
	if len(targetIDs) == 0 {
		return nil
	}
	if presentedAt.IsZero() {
		presentedAt = s.now()
	}
	presentedAt = presentedAt.UTC()

	unlockDocument, lockErr := lockCuratedDocument(path)
	if lockErr != nil {
		return lockErr
	}
	defer unlockDocument()
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDocument(path, digest)
	if err != nil {
		return err
	}
	found := 0
	for i := range doc.Entries {
		if _, ok := targetIDs[doc.Entries[i].ID]; !ok {
			continue
		}
		value := presentedAt
		doc.Entries[i].LastPresentedAt = &value
		found++
	}
	if found == 0 {
		return nil
	}
	return s.writeDocument(path, doc)
}

// MarkUsed is kept for source compatibility. New code should use MarkPresented.
func (s *CuratedStore) MarkUsed(target string, caller CallerScope, ids []string, usedAt time.Time) error {
	return s.MarkPresented(target, caller, ids, usedAt)
}

func (s *CuratedStore) Maintain(
	target string,
	caller CallerScope,
	autoArchiveExpired bool,
	archivedRetention time.Duration,
	now time.Time,
) error {
	path, digest, _, err := s.scopePath(target, caller)
	if err != nil {
		return err
	}
	if now.IsZero() {
		now = s.now()
	}
	now = now.UTC()
	unlockDocument, lockErr := lockCuratedDocument(path)
	if lockErr != nil {
		return lockErr
	}
	defer unlockDocument()
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDocument(path, digest)
	if err != nil {
		return err
	}
	changed := false
	out := make([]CuratedEntry, 0, len(doc.Entries))
	for _, entry := range doc.Entries {
		status := entry.EffectiveStatus()
		if autoArchiveExpired && status == CuratedStatusActive &&
			entry.ExpiresAt != nil && !now.Before(entry.ExpiresAt.UTC()) {
			entry.Status = CuratedStatusArchived
			entry.Pinned = false
			archived := now
			entry.ArchivedAt = &archived
			entry.UpdatedAt = now
			status = CuratedStatusArchived
			changed = true
		}
		if archivedRetention > 0 && status == CuratedStatusArchived && entry.ArchivedAt != nil &&
			now.Sub(entry.ArchivedAt.UTC()) > archivedRetention {
			changed = true
			continue
		}
		out = append(out, entry)
	}
	if !changed {
		return nil
	}
	doc.Entries = out
	return s.writeDocument(path, doc)
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

func validStablePendingID(id string) bool {
	if !strings.HasPrefix(id, "pm_") || len(id) != len("pm_")+16 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, "pm_"))
	return err == nil
}

// ValidCuratedEntryID reports whether value has the stable identifier shape
// used for curated entries. It performs syntax validation only.
func ValidCuratedEntryID(value string) bool {
	return validStableEntryID(strings.TrimSpace(value))
}

// ValidPendingCuratedID reports whether value has the stable identifier shape
// used for staged curated-memory batches. It performs syntax validation only.
func ValidPendingCuratedID(value string) bool {
	return validStablePendingID(strings.TrimSpace(value))
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

func markCuratedSuperseded(entries []CuratedEntry, id string, now time.Time) {
	for i := range entries {
		if entries[i].ID != id {
			continue
		}
		entries[i].Status = CuratedStatusSuperseded
		entries[i].Pinned = false
		entries[i].UpdatedAt = now
		return
	}
}

func curatedEntryByID(entries []CuratedEntry, id string) (CuratedEntry, bool) {
	for _, entry := range entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return CuratedEntry{}, false
}

func reconcilePreferenceKey(entries []CuratedEntry, touchedID string, now time.Time) {
	touchedIndex := -1
	for i := range entries {
		if entries[i].ID == touchedID {
			touchedIndex = i
			break
		}
	}
	if touchedIndex < 0 {
		return
	}
	key := NormalizePreferenceKey(entries[touchedIndex].PreferenceKey)
	if key == "" {
		return
	}
	entries[touchedIndex].PreferenceKey = key

	candidates := make([]int, 0, 4)
	for i := range entries {
		if entries[i].EffectiveStatus() != CuratedStatusActive ||
			NormalizePreferenceKey(entries[i].PreferenceKey) != key {
			continue
		}
		candidates = append(candidates, i)
	}
	if len(candidates) <= 1 {
		return
	}

	winner := candidates[0]
	better := func(left, right CuratedEntry) bool {
		if left.EvidenceAuthority() != right.EvidenceAuthority() {
			return left.EvidenceAuthority() > right.EvidenceAuthority()
		}
		if left.EffectiveConfidence() != right.EffectiveConfidence() {
			return left.EffectiveConfidence() > right.EffectiveConfidence()
		}
		leftConfirmed := left.EffectiveLastConfirmedAt()
		rightConfirmed := right.EffectiveLastConfirmedAt()
		if leftConfirmed != nil || rightConfirmed != nil {
			if leftConfirmed == nil {
				return false
			}
			if rightConfirmed == nil {
				return true
			}
			if !leftConfirmed.Equal(*rightConfirmed) {
				return leftConfirmed.After(*rightConfirmed)
			}
		}
		if !left.UpdatedAt.Equal(right.UpdatedAt) {
			return left.UpdatedAt.After(right.UpdatedAt)
		}
		return left.ID > right.ID
	}
	for _, idx := range candidates[1:] {
		if better(entries[idx], entries[winner]) {
			winner = idx
		}
	}

	var strongestPrior string
	for _, idx := range candidates {
		if idx == winner {
			continue
		}
		if strongestPrior == "" && entries[idx].ID != touchedID {
			strongestPrior = entries[idx].ID
		}
		entries[idx].Status = CuratedStatusSuperseded
		entries[idx].Pinned = false
		entries[idx].UpdatedAt = now
	}
	if winner == touchedIndex && entries[winner].Supersedes == "" && strongestPrior != "" {
		entries[winner].Supersedes = strongestPrior
	}
}

func findCuratedConflicts(entries []CuratedEntry, mutations []CuratedMutation) []CuratedConflict {
	conflicts := make([]CuratedConflict, 0, 4)
	for mutationIndex, mutation := range mutations {
		if mutation.Action != CuratedActionAdd && mutation.Action != CuratedActionReplace {
			continue
		}
		for _, entry := range entries {
			if entry.ID == mutation.ID || entry.ID == mutation.Supersedes ||
				entry.EffectiveStatus() != CuratedStatusActive {
				continue
			}
			mutationKey := NormalizePreferenceKey(mutation.PreferenceKey)
			entryKey := NormalizePreferenceKey(entry.PreferenceKey)
			if mutationKey != "" && mutationKey == entryKey {
				conflicts = append(conflicts, CuratedConflict{
					MutationIndex: mutationIndex,
					EntryID:       entry.ID,
					Similarity:    1,
					Reason:        "same structured preference key; authority and recency choose one active value",
				})
			} else {
				similarity := curatedContentSimilarity(mutation.Content, entry.Content)
				if similarity < 0.72 {
					continue
				}
				conflicts = append(conflicts, CuratedConflict{
					MutationIndex: mutationIndex,
					EntryID:       entry.ID,
					Similarity:    mathRound(similarity, 3),
					Reason:        "likely near-duplicate or conflicting active entry",
				})
			}
			if len(conflicts) >= 8 {
				return conflicts
			}
		}
	}
	return conflicts
}

func curatedContentSimilarity(left, right string) float64 {
	leftTokens := lexicalTokenCounts(left)
	rightTokens := lexicalTokenCounts(right)
	if len(leftTokens) == 0 || len(rightTokens) == 0 {
		return trigramSimilarity(left, right)
	}
	intersection := 0
	union := len(leftTokens)
	for token := range rightTokens {
		if _, ok := leftTokens[token]; ok {
			intersection++
			continue
		}
		union++
	}
	jaccard := float64(intersection) / float64(union)
	fuzzy := trigramSimilarity(left, right)
	return (jaccard * 0.65) + (fuzzy * 0.35)
}

func mathRound(value float64, places int) float64 {
	factor := 1.0
	for range places {
		factor *= 10
	}
	return float64(int(value*factor+0.5)) / factor
}

func curatedCharacters(entries []CuratedEntry) int {
	total := 0
	for _, entry := range entries {
		total += utf8.RuneCountInString(entry.Content)
	}
	return total
}

func cloneCuratedEntries(entries []CuratedEntry) []CuratedEntry {
	out := make([]CuratedEntry, len(entries))
	copy(out, entries)
	return out
}

func clonePendingChanges(changes []PendingCuratedChange) []PendingCuratedChange {
	out := make([]PendingCuratedChange, len(changes))
	for i := range changes {
		out[i] = changes[i]
		out[i].Mutations = append([]CuratedMutation(nil), changes[i].Mutations...)
	}
	return out
}

// lockCuratedDocument serializes read-modify-write transactions across store
// instances in the same process. This matters when the gateway and
// authenticated dashboard open the same structured store independently.
func lockCuratedDocument(path string) (func(), error) {
	for {
		actual, _ := curatedDocumentLocks.LoadOrStore(path, &sync.Mutex{})
		mu, ok := actual.(*sync.Mutex)
		if !ok || mu == nil {
			curatedDocumentLocks.CompareAndSwap(path, actual, &sync.Mutex{})
			continue
		}
		mu.Lock()
		fileUnlock, err := fileutil.LockFile(curatedDocumentLockPath(path))
		if err != nil {
			mu.Unlock()
			return nil, fmt.Errorf("lock curated memory: %w", err)
		}
		return func() {
			_ = fileUnlock()
			mu.Unlock()
		}, nil
	}
}

func curatedDocumentLockPath(path string) string {
	root := filepath.Dir(path)
	if filepath.Base(root) == "users" {
		root = filepath.Dir(root)
	}
	digest := sha256.Sum256([]byte(filepath.Clean(path)))
	return filepath.Join(root, ".locks", "document_"+hex.EncodeToString(digest[:16]))
}

func pendingIDs(changes []PendingCuratedChange) map[string]struct{} {
	ids := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		ids[change.ID] = struct{}{}
	}
	return ids
}
