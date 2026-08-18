package memory

import (
	"encoding/json"
	"hash/fnv"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const UserProfileVersion = 1

// UserProfileField is a compact derived view of one authoritative curated
// memory entry. SourceID keeps the profile auditable without turning the
// profile itself into a second source of truth.
type UserProfileField struct {
	Key          string  `json:"key,omitempty"`
	Value        string  `json:"value,omitempty"`
	Content      string  `json:"content,omitempty"`
	EvidenceKind string  `json:"evidence_kind"`
	Confidence   float64 `json:"confidence"`
	SourceID     string  `json:"source_id"`
}

// UserProfileSnapshot is a small current-user interaction model derived from
// active curated memory. It intentionally excludes project/episodic details
// that should remain query-retrieved.
type UserProfileSnapshot struct {
	Version       int                `json:"version"`
	Identity      []UserProfileField `json:"identity,omitempty"`
	Communication []UserProfileField `json:"communication,omitempty"`
	Workflow      []UserProfileField `json:"workflow,omitempty"`
	Interaction   []UserProfileField `json:"interaction,omitempty"`
	Boundaries    []UserProfileField `json:"boundaries,omitempty"`
	UpdatedAt     time.Time          `json:"updated_at,omitempty"`
	SourceIDs     []string           `json:"source_ids,omitempty"`
	Characters    int                `json:"characters"`
}

type UserProfileOptions struct {
	MaxChars      int
	MinConfidence float64
	Now           time.Time
}

type cachedUserProfile struct {
	Revision   uint64
	MaxChars   int
	MinScore   float64
	ValidUntil time.Time
	LastAccess uint64
	Snapshot   UserProfileSnapshot
}

type profileCandidate struct {
	category string
	entry    CuratedEntry
}

// CompileUserProfile creates a bounded deterministic profile for the trusted
// current user. Curated memory remains authoritative; the snapshot is only a
// cached representation for prompt assembly and observability.
func (s *CuratedStore) CompileUserProfile(
	caller CallerScope,
	opts UserProfileOptions,
) (UserProfileSnapshot, error) {
	path, digest, _, err := s.scopePath(CuratedTargetCurrentUser, caller)
	if err != nil {
		return UserProfileSnapshot{}, err
	}
	if opts.MaxChars <= 0 {
		opts.MaxChars = 1_200
	}
	if opts.MinConfidence <= 0 {
		opts.MinConfidence = 0.65
	}
	if opts.MinConfidence > 1 {
		opts.MinConfidence = 1
	}
	useCache := opts.Now.IsZero() && !IsSharedMemoryContext(caller)
	if opts.Now.IsZero() {
		opts.Now = s.now()
	}
	now := opts.Now.UTC()

	unlockDocument, lockErr := lockCuratedDocument(path)
	if lockErr != nil {
		return UserProfileSnapshot{}, lockErr
	}
	defer unlockDocument()
	// Profile compilation may be the first v1 reader after upgrade. Own the
	// write lock for the bounded one-time atomic schema rewrite.
	s.mu.Lock()
	doc, readErr := s.readDocument(path, digest, true)
	s.mu.Unlock()
	if readErr != nil {
		return UserProfileSnapshot{}, readErr
	}
	profileRevision := userProfileRevision(doc.Entries)
	var validUntil time.Time
	for _, raw := range doc.Entries {
		entry := normalizedCuratedEntry(raw)
		if entry.EffectiveStatus() != CuratedStatusActive || profileCategory(entry) == "" || entry.ExpiresAt == nil {
			continue
		}
		expires := entry.ExpiresAt.UTC()
		if expires.After(now) && (validUntil.IsZero() || expires.Before(validUntil)) {
			validUntil = expires
		}
	}

	if useCache {
		if item, valid := s.loadCachedUserProfile(path); valid {
			cacheTimeValid := item.ValidUntil.IsZero() || now.Before(item.ValidUntil)
			if cacheTimeValid && item.Revision == profileRevision && item.MaxChars == opts.MaxChars &&
				item.MinScore == opts.MinConfidence {
				return cloneUserProfileSnapshot(item.Snapshot), nil
			}
		}
	}

	candidates := make([]profileCandidate, 0, len(doc.Entries))
	for _, raw := range doc.Entries {
		entry := normalizedCuratedEntry(raw)
		if IsSharedMemoryContext(caller) && entry.EffectiveVisibility() != CuratedVisibilityBehavioral {
			continue
		}
		if !entry.PromptEligible(now) || !profileEligibleEntry(entry, opts.MinConfidence) {
			continue
		}
		category := profileCategory(entry)
		if category == "" {
			continue
		}
		candidates = append(candidates, profileCandidate{category: category, entry: entry})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i].entry, candidates[j].entry
		if left.EvidenceAuthority() != right.EvidenceAuthority() {
			return left.EvidenceAuthority() > right.EvidenceAuthority()
		}
		if left.EffectiveConfidence() != right.EffectiveConfidence() {
			return left.EffectiveConfidence() > right.EffectiveConfidence()
		}
		if !left.UpdatedAt.Equal(right.UpdatedAt) {
			return left.UpdatedAt.After(right.UpdatedAt)
		}
		return left.ID < right.ID
	})

	snapshot := UserProfileSnapshot{Version: UserProfileVersion}
	seenKeys := make(map[string]struct{})
	for _, candidate := range candidates {
		entry := candidate.entry
		key := NormalizePreferenceKey(entry.PreferenceKey)
		if key != "" {
			if _, seen := seenKeys[key]; seen {
				continue
			}
			seenKeys[key] = struct{}{}
		}
		field := UserProfileField{
			Key:          key,
			Value:        strings.TrimSpace(entry.PreferenceValue),
			EvidenceKind: entry.EffectiveEvidenceKind(),
			Confidence:   entry.EffectiveConfidence(),
			SourceID:     entry.ID,
		}
		if field.Value == "" || key == "" {
			field.Content = strings.TrimSpace(entry.Content)
		}
		candidateSnapshot := cloneUserProfileSnapshot(snapshot)
		appendUserProfileField(&candidateSnapshot, candidate.category, field)
		candidateSnapshot.SourceIDs = append(candidateSnapshot.SourceIDs, entry.ID)
		if candidateSnapshot.UpdatedAt.IsZero() || entry.UpdatedAt.After(candidateSnapshot.UpdatedAt) {
			candidateSnapshot.UpdatedAt = entry.UpdatedAt
		}
		finalizeUserProfileCharacters(&candidateSnapshot)
		if candidateSnapshot.Characters <= 0 || candidateSnapshot.Characters > opts.MaxChars {
			continue
		}
		snapshot = candidateSnapshot
	}

	if useCache {
		s.storeCachedUserProfile(path, cachedUserProfile{
			Revision:   profileRevision,
			MaxChars:   opts.MaxChars,
			MinScore:   opts.MinConfidence,
			ValidUntil: validUntil,
			Snapshot:   cloneUserProfileSnapshot(snapshot),
		})
	}
	return snapshot, nil
}

func (s *CuratedStore) loadCachedUserProfile(path string) (cachedUserProfile, bool) {
	if s == nil {
		return cachedUserProfile{}, false
	}
	s.profileCacheMu.Lock()
	defer s.profileCacheMu.Unlock()
	item, ok := s.profileCache[path]
	if !ok {
		return cachedUserProfile{}, false
	}
	s.profileCacheClock++
	item.LastAccess = s.profileCacheClock
	s.profileCache[path] = item
	return item, true
}

func (s *CuratedStore) storeCachedUserProfile(path string, item cachedUserProfile) {
	if s == nil || s.profileCacheLimit <= 0 {
		return
	}
	s.profileCacheMu.Lock()
	defer s.profileCacheMu.Unlock()
	s.profileCacheClock++
	item.LastAccess = s.profileCacheClock
	if _, exists := s.profileCache[path]; !exists && len(s.profileCache) >= s.profileCacheLimit {
		oldestPath := ""
		oldestAccess := ^uint64(0)
		for candidatePath, candidate := range s.profileCache {
			if candidate.LastAccess < oldestAccess {
				oldestPath = candidatePath
				oldestAccess = candidate.LastAccess
			}
		}
		if oldestPath != "" {
			delete(s.profileCache, oldestPath)
		}
	}
	s.profileCache[path] = item
}

func profileEligibleEntry(entry CuratedEntry, minConfidence float64) bool {
	switch entry.EffectiveType() {
	case CuratedTypeIdentity, CuratedTypeCommunicationPreference, CuratedTypeWorkflowPreference, CuratedTypeCorrection:
	default:
		return false
	}
	if entry.EffectiveEvidenceKind() == CuratedEvidenceInferred && entry.EffectiveConfidence() < 0.70 {
		return false
	}
	return entry.EffectiveConfidence() >= minConfidence || entry.EffectiveEvidenceKind() == CuratedEvidenceExplicit
}

func profileCategory(entry CuratedEntry) string {
	key := NormalizePreferenceKey(entry.PreferenceKey)
	switch {
	case strings.HasPrefix(key, "identity."):
		return "identity"
	case strings.HasPrefix(key, "communication."):
		return "communication"
	case strings.HasPrefix(key, "workflow."):
		return "workflow"
	case strings.HasPrefix(key, "interaction."):
		return "interaction"
	case strings.HasPrefix(key, "boundary."), strings.HasPrefix(key, "boundaries."):
		return "boundaries"
	}
	switch entry.EffectiveType() {
	case CuratedTypeIdentity:
		return "identity"
	case CuratedTypeCommunicationPreference:
		return "communication"
	case CuratedTypeWorkflowPreference:
		return "workflow"
	case CuratedTypeCorrection:
		if key != "" {
			return "interaction"
		}
	}
	return ""
}

func appendUserProfileField(snapshot *UserProfileSnapshot, category string, field UserProfileField) {
	switch category {
	case "identity":
		snapshot.Identity = append(snapshot.Identity, field)
	case "communication":
		snapshot.Communication = append(snapshot.Communication, field)
	case "workflow":
		snapshot.Workflow = append(snapshot.Workflow, field)
	case "interaction":
		snapshot.Interaction = append(snapshot.Interaction, field)
	case "boundaries":
		snapshot.Boundaries = append(snapshot.Boundaries, field)
	}
}

func userProfileJSONCharacters(snapshot UserProfileSnapshot) int {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return 0
	}
	return utf8.RuneCount(data)
}

func finalizeUserProfileCharacters(snapshot *UserProfileSnapshot) {
	if snapshot == nil {
		return
	}
	for i := 0; i < 4; i++ {
		next := userProfileJSONCharacters(*snapshot)
		if next == snapshot.Characters {
			return
		}
		snapshot.Characters = next
	}
	snapshot.Characters = userProfileJSONCharacters(*snapshot)
}

func userProfileRevision(entries []CuratedEntry) uint64 {
	h := fnv.New64a()
	for _, raw := range entries {
		entry := normalizedCuratedEntry(raw)
		if profileCategory(entry) == "" {
			continue
		}
		for _, value := range []string{
			entry.ID, entry.Content, entry.Type, entry.Status, entry.EvidenceKind, entry.EffectiveVisibility(),
			entry.PreferenceKey, entry.PreferenceValue, entry.Supersedes,
			entry.UpdatedAt.UTC().Format(time.RFC3339Nano),
		} {
			_, _ = h.Write([]byte(value))
			_, _ = h.Write([]byte{0})
		}
	}
	return h.Sum64()
}

func cloneUserProfileSnapshot(value UserProfileSnapshot) UserProfileSnapshot {
	value.Identity = append([]UserProfileField(nil), value.Identity...)
	value.Communication = append([]UserProfileField(nil), value.Communication...)
	value.Workflow = append([]UserProfileField(nil), value.Workflow...)
	value.Interaction = append([]UserProfileField(nil), value.Interaction...)
	value.Boundaries = append([]UserProfileField(nil), value.Boundaries...)
	value.SourceIDs = append([]string(nil), value.SourceIDs...)
	return value
}
