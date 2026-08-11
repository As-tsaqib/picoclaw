package evolution

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

type Store struct {
	paths Paths
}

func NewStore(paths Paths) *Store {
	return &Store{paths: paths}
}

var storeFileLocks sync.Map

func (s *Store) AppendLearningRecord(ctx context.Context, record LearningRecord) error {
	switch record.Kind {
	case RecordKindPattern, legacyRecordKindRule:
		return s.AppendPatternRecords([]LearningRecord{record})
	default:
		return s.AppendTaskRecord(ctx, record)
	}
}

func (s *Store) AppendLearningRecords(records []LearningRecord) error {
	taskRecords := make([]LearningRecord, 0, len(records))
	patternRecords := make([]LearningRecord, 0, len(records))
	for _, record := range records {
		switch record.Kind {
		case RecordKindPattern, legacyRecordKindRule:
			patternRecords = append(patternRecords, record)
		default:
			taskRecords = append(taskRecords, record)
		}
	}
	if err := s.AppendTaskRecords(context.Background(), taskRecords); err != nil {
		return err
	}
	return s.AppendPatternRecords(patternRecords)
}

func (s *Store) AppendTaskRecord(ctx context.Context, record LearningRecord) error {
	return s.AppendTaskRecords(ctx, []LearningRecord{record})
}

func (s *Store) AppendTaskRecords(ctx context.Context, records []LearningRecord) error {
	return s.appendJSONLRecords(ctx, s.paths.TaskRecords, records)
}

func (s *Store) AppendPatternRecords(records []LearningRecord) error {
	return s.appendJSONLRecords(context.Background(), s.paths.PatternRecords, records)
}

func (s *Store) appendJSONLRecords(ctx context.Context, path string, records []LearningRecord) error {
	if len(records) == 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	unlock, err := lockStoreFile(path)
	if err != nil {
		return err
	}
	defer unlock()

	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o700); mkdirErr != nil {
		return mkdirErr
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
		_ = f.Close()
		return chmodErr
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	enc := json.NewEncoder(f)
	for _, record := range records {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if encodeErr := enc.Encode(sanitizeLearningRecordForPersistence(record)); encodeErr != nil {
			return encodeErr
		}
	}
	return nil
}

func (s *Store) LoadLearningRecords() ([]LearningRecord, error) {
	taskRecords, err := s.LoadTaskRecords()
	if err != nil {
		return nil, err
	}
	patternRecords, err := s.LoadPatternRecords()
	if err != nil {
		return nil, err
	}
	return append(taskRecords, patternRecords...), nil
}

func (s *Store) LoadTaskRecords() ([]LearningRecord, error) {
	records, err := s.loadRecordsFromPath(s.paths.TaskRecords)
	if err != nil {
		return nil, err
	}
	legacy, err := s.loadLegacyTaskRecords()
	if err != nil {
		return nil, err
	}
	return mergeLearningRecordsByID(legacy, records), nil
}

func (s *Store) LoadPatternRecords() ([]LearningRecord, error) {
	records, err := s.loadRecordsFromPath(s.paths.PatternRecords)
	if err != nil {
		return nil, err
	}
	legacy, err := s.loadLegacyPatternRecords()
	if err != nil {
		return nil, err
	}
	return mergeLearningRecordsByID(legacy, records), nil
}

func (s *Store) loadRecordsFromPath(path string) ([]LearningRecord, error) {
	var records []LearningRecord
	if err := decodeJSONLLines(path, func(line []byte) error {
		var record LearningRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return err
		}
		records = append(records, sanitizeLearningRecordForPersistence(record))
		return nil
	}); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *Store) loadLegacyTaskRecords() ([]LearningRecord, error) {
	records, err := s.loadRecordsFromPath(s.paths.LearningRecords)
	if err != nil {
		return nil, err
	}
	out := make([]LearningRecord, 0, len(records))
	for _, record := range records {
		if isTaskRecordKind(record.Kind) {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s *Store) loadLegacyPatternRecords() ([]LearningRecord, error) {
	records, err := s.loadRecordsFromPath(s.paths.LearningRecords)
	if err != nil {
		return nil, err
	}
	out := make([]LearningRecord, 0, len(records))
	for _, record := range records {
		if isPatternRecordKind(record.Kind) {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s *Store) SaveTaskRecords(records []LearningRecord) error {
	return s.saveJSONLRecords(s.paths.TaskRecords, records)
}

func (s *Store) MarkTaskRecordsClustered(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	target := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		target[id] = struct{}{}
	}
	if len(target) == 0 {
		return nil
	}

	unlock, err := lockStoreFile(s.paths.TaskRecords)
	if err != nil {
		return err
	}
	defer unlock()

	current, err := s.loadRecordsFromPath(s.paths.TaskRecords)
	if err != nil {
		return err
	}
	legacy, err := s.loadLegacyTaskRecords()
	if err != nil {
		return err
	}
	records := mergeLearningRecordsByID(legacy, current)

	hasTargetRecordInWorkspace := make(map[string]bool, len(target))
	if strings.TrimSpace(s.paths.Workspace) != "" {
		for _, record := range records {
			if _, ok := target[record.ID]; !ok {
				continue
			}
			if record.WorkspaceID == s.paths.Workspace {
				hasTargetRecordInWorkspace[record.ID] = true
			}
		}
	}

	changed := false
	for i := range records {
		if _, ok := target[records[i].ID]; !ok {
			continue
		}
		if hasTargetRecordInWorkspace[records[i].ID] && records[i].WorkspaceID != s.paths.Workspace {
			continue
		}
		records[i].Status = RecordStatus("clustered")
		changed = true
	}
	if !changed {
		return nil
	}
	return s.saveJSONLRecordsLocked(s.paths.TaskRecords, records)
}

func (s *Store) SavePatternRecords(records []LearningRecord) error {
	return s.saveJSONLRecords(s.paths.PatternRecords, records)
}

func (s *Store) MergePatternRecords(records []LearningRecord) error {
	if len(records) == 0 {
		return nil
	}

	unlock, err := lockStoreFile(s.paths.PatternRecords)
	if err != nil {
		return err
	}
	defer unlock()

	current, err := s.loadRecordsFromPath(s.paths.PatternRecords)
	if err != nil {
		return err
	}
	legacy, err := s.loadLegacyPatternRecords()
	if err != nil {
		return err
	}
	merged := mergeLearningRecordsByID(mergeLearningRecordsByID(legacy, current), records)
	return s.saveJSONLRecordsLocked(s.paths.PatternRecords, merged)
}

func (s *Store) saveJSONLRecords(path string, records []LearningRecord) error {
	unlock, err := lockStoreFile(path)
	if err != nil {
		return err
	}
	defer unlock()

	return s.saveJSONLRecordsLocked(path, records)
}

func (s *Store) saveJSONLRecordsLocked(path string, records []LearningRecord) error {
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o700); mkdirErr != nil {
		return mkdirErr
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, record := range records {
		if err := enc.Encode(sanitizeLearningRecordForPersistence(record)); err != nil {
			return err
		}
	}
	return fileutil.WriteFileAtomic(path, buf.Bytes(), 0o600)
}

func mergeLearningRecordsByID(base, updates []LearningRecord) []LearningRecord {
	out := append([]LearningRecord(nil), base...)
	indexByID := make(map[string]int, len(out)+len(updates))
	for i, record := range out {
		key := learningRecordMergeKey(record)
		if key == "" {
			continue
		}
		indexByID[key] = i
	}
	for _, record := range updates {
		key := learningRecordMergeKey(record)
		if key == "" {
			out = append(out, record)
			continue
		}
		if idx, ok := indexByID[key]; ok {
			out[idx] = record
			continue
		}
		indexByID[key] = len(out)
		out = append(out, record)
	}
	return out
}

func learningRecordMergeKey(record LearningRecord) string {
	id := strings.TrimSpace(record.ID)
	if id == "" {
		return ""
	}
	return strings.TrimSpace(record.WorkspaceID) + "\x00" + id
}

func (s *Store) SaveDrafts(drafts []SkillDraft) error {
	unlock, err := lockStoreFile(s.paths.SkillDrafts)
	if err != nil {
		return err
	}
	defer unlock()

	existing, err := s.LoadDrafts()
	if err != nil {
		return err
	}

	indexByKey := make(map[string]int, len(existing))
	for i, draft := range existing {
		indexByKey[draftKey(draft.WorkspaceID, draft.ID)] = i
	}

	for _, draft := range drafts {
		draft, _ = sanitizeSkillDraftForPersistence(draft)
		key := draftKey(draft.WorkspaceID, draft.ID)
		if idx, ok := indexByKey[key]; ok {
			existing[idx] = draft
			continue
		}
		indexByKey[key] = len(existing)
		existing = append(existing, draft)
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(s.paths.SkillDrafts, data, 0o600)
}

func (s *Store) LoadDrafts() ([]SkillDraft, error) {
	data, err := os.ReadFile(s.paths.SkillDrafts)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}

	var drafts []SkillDraft
	if err := json.Unmarshal(data, &drafts); err != nil {
		return nil, err
	}
	for i := range drafts {
		drafts[i], _ = sanitizeSkillDraftForPersistence(drafts[i])
	}
	return drafts, nil
}

func (s *Store) UpdateDraft(
	workspace, id string,
	update func(*SkillDraft) error,
) (SkillDraft, error) {
	workspace = strings.TrimSpace(workspace)
	id = strings.TrimSpace(id)
	if workspace == "" || !validEvolutionControlID(id) || update == nil {
		return SkillDraft{}, fmt.Errorf("invalid evolution draft update")
	}
	unlock, err := lockStoreFile(s.paths.SkillDrafts)
	if err != nil {
		return SkillDraft{}, err
	}
	defer unlock()
	drafts, err := s.LoadDrafts()
	if err != nil {
		return SkillDraft{}, err
	}
	index := -1
	for i := range drafts {
		if drafts[i].WorkspaceID == workspace && drafts[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return SkillDraft{}, os.ErrNotExist
	}
	if updateErr := update(&drafts[index]); updateErr != nil {
		return SkillDraft{}, updateErr
	}
	drafts[index], _ = sanitizeSkillDraftForPersistence(drafts[index])
	data, err := json.MarshalIndent(drafts, "", "  ")
	if err != nil {
		return SkillDraft{}, err
	}
	if writeErr := fileutil.WriteFileAtomic(s.paths.SkillDrafts, data, 0o600); writeErr != nil {
		return SkillDraft{}, writeErr
	}
	return drafts[index], nil
}

func (s *Store) AppendAudit(event AuditEvent) error {
	if strings.TrimSpace(event.Action) == "" || strings.TrimSpace(event.Workspace) == "" {
		return fmt.Errorf("evolution audit action and workspace are required")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.ID == "" {
		sum := sha256.Sum256([]byte(strings.Join([]string{
			event.Workspace, event.Action, event.DraftID, event.SkillName,
			event.Timestamp.UTC().Format(time.RFC3339Nano),
		}, "\x00")))
		event.ID = "audit-" + hex.EncodeToString(sum[:8])
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	unlock, err := lockStoreFile(s.paths.AuditLog)
	if err != nil {
		return err
	}
	defer unlock()
	if mkdirErr := os.MkdirAll(filepath.Dir(s.paths.AuditLog), 0o700); mkdirErr != nil {
		return mkdirErr
	}
	f, err := os.OpenFile(s.paths.AuditLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, writeErr := f.Write(append(data, '\n')); writeErr != nil {
		return writeErr
	}
	return os.Chmod(s.paths.AuditLog, 0o600)
}

func (s *Store) LoadAudit(limit int) ([]AuditEvent, error) {
	var events []AuditEvent
	if err := decodeJSONLLines(s.paths.AuditLog, func(line []byte) error {
		var event AuditEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return err
		}
		events = append(events, event)
		return nil
	}); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}

func (s *Store) LoadAuditForWorkspace(workspace string, limit int) ([]AuditEvent, error) {
	filtered := make([]AuditEvent, 0)
	if err := decodeJSONLLines(s.paths.AuditLog, func(line []byte) error {
		var event AuditEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return err
		}
		if event.Workspace == workspace {
			filtered = append(filtered, event)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered, nil
}

func (s *Store) SaveSkillVersion(snapshot SkillVersionSnapshot) error {
	path, err := s.skillVersionPath(snapshot.Workspace, snapshot.SkillName, snapshot.Version)
	if err != nil {
		return err
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now().UTC()
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	unlock, err := lockStoreFile(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(path, data, 0o600)
}

func (s *Store) LoadSkillVersion(workspace, skillName, version string) (SkillVersionSnapshot, error) {
	path, err := s.skillVersionPath(workspace, skillName, version)
	if err != nil {
		return SkillVersionSnapshot{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillVersionSnapshot{}, err
	}
	var snapshot SkillVersionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return SkillVersionSnapshot{}, err
	}
	if snapshot.Workspace != workspace || snapshot.SkillName != skillName || snapshot.Version != version {
		return SkillVersionSnapshot{}, fmt.Errorf("evolution version scope mismatch")
	}
	return snapshot, nil
}

func (s *Store) PruneSkillVersions(
	workspace,
	skillName string,
	keep int,
	protectedVersions ...string,
) error {
	probe, err := s.skillVersionPath(workspace, skillName, "retention-probe")
	if err != nil {
		return err
	}
	dir := filepath.Dir(probe)
	unlock, err := lockStoreFile(dir)
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	protected := make(map[string]struct{}, len(protectedVersions))
	for _, version := range protectedVersions {
		if version = strings.TrimSpace(version); version != "" {
			protected[version] = struct{}{}
		}
	}
	type versionFile struct {
		path     string
		snapshot SkillVersionSnapshot
	}
	versions := make([]versionFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var snapshot SkillVersionSnapshot
		if decodeErr := json.Unmarshal(data, &snapshot); decodeErr != nil {
			return decodeErr
		}
		if snapshot.Workspace != workspace || snapshot.SkillName != skillName {
			return fmt.Errorf("evolution version scope mismatch")
		}
		versions = append(versions, versionFile{path: path, snapshot: snapshot})
	}
	sort.SliceStable(versions, func(i, j int) bool {
		if !versions[i].snapshot.CreatedAt.Equal(versions[j].snapshot.CreatedAt) {
			return versions[i].snapshot.CreatedAt.After(versions[j].snapshot.CreatedAt)
		}
		return versions[i].snapshot.Version < versions[j].snapshot.Version
	})
	if keep < 1 {
		keep = 1
	}
	kept := 0
	for _, version := range versions {
		_, preserve := protected[version.snapshot.Version]
		if preserve || kept < keep {
			kept++
			continue
		}
		if err := os.Remove(version.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) skillVersionPath(workspace, skillName, version string) (string, error) {
	if err := validateEvolutionSkillTarget(skillName); err != nil {
		return "", err
	}
	workspace = strings.TrimSpace(workspace)
	version = strings.TrimSpace(version)
	if workspace == "" || version == "" || len(version) > 256 || strings.ContainsRune(version, '\x00') {
		return "", fmt.Errorf("invalid evolution version scope")
	}
	digest := sha256.Sum256([]byte(version))
	name := "version-" + hex.EncodeToString(digest[:16]) + ".json"
	return filepath.Join(s.paths.VersionsDir, workspaceScopeDir(workspace), skillName, name), nil
}

func (s *Store) SaveProfile(profile SkillProfile) error {
	path, err := s.profilePath(profile.WorkspaceID, profile.SkillName)
	if err != nil {
		return err
	}
	unlock, err := lockStoreFile(path)
	if err != nil {
		return err
	}
	defer unlock()

	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o700); mkdirErr != nil {
		return mkdirErr
	}

	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(path, data, 0o600)
}

func (s *Store) DeleteProfile(workspaceID, skillName string) error {
	path, err := s.profilePath(workspaceID, skillName)
	if err != nil {
		return err
	}
	unlock, err := lockStoreFile(path)
	if err != nil {
		return err
	}
	defer unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) LoadProfile(skillName string) (SkillProfile, error) {
	return s.loadProfileForWorkspace(strings.TrimSpace(s.paths.Workspace), skillName)
}

func (s *Store) UpdateProfile(
	workspaceID, skillName string,
	update func(profile *SkillProfile, exists bool) error,
) error {
	targetPath, err := s.profilePath(workspaceID, skillName)
	if err != nil {
		return err
	}

	unlock, err := lockStoreFile(targetPath)
	if err != nil {
		return err
	}
	defer unlock()

	profile, err := s.loadProfileForWorkspace(workspaceID, skillName)
	exists := err == nil
	if errors.Is(err, os.ErrNotExist) {
		profile = SkillProfile{}
	} else if err != nil {
		return err
	}

	if updateErr := update(&profile, exists); updateErr != nil {
		return updateErr
	}
	if !exists && isZeroSkillProfile(profile) {
		return nil
	}
	if mkdirErr := os.MkdirAll(filepath.Dir(targetPath), 0o700); mkdirErr != nil {
		return mkdirErr
	}

	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(targetPath, data, 0o600)
}

func (s *Store) loadProfileForWorkspace(workspaceID, skillName string) (SkillProfile, error) {
	paths, err := s.profileLookupPaths(workspaceID, skillName)
	if err != nil {
		return SkillProfile{}, err
	}
	for _, path := range paths {
		profile, loadErr := s.loadProfileFromPath(path)
		if errors.Is(loadErr, os.ErrNotExist) {
			continue
		}
		if loadErr != nil {
			return SkillProfile{}, loadErr
		}
		return profile, nil
	}
	return SkillProfile{}, os.ErrNotExist
}

func isZeroSkillProfile(profile SkillProfile) bool {
	return profile.SkillName == "" &&
		profile.WorkspaceID == "" &&
		profile.CurrentVersion == "" &&
		profile.Status == "" &&
		profile.Origin == "" &&
		profile.HumanSummary == "" &&
		profile.ChangeReason == "" &&
		len(profile.IntendedUseCases) == 0 &&
		len(profile.PreferredEntryPath) == 0 &&
		len(profile.AvoidPatterns) == 0 &&
		profile.LastUsedAt.IsZero() &&
		profile.UseCount == 0 &&
		profile.RetentionScore == 0 &&
		len(profile.VersionHistory) == 0
}

func (s *Store) LoadProfiles() ([]SkillProfile, error) {
	entries, err := os.ReadDir(s.paths.ProfilesDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	profiles := make([]SkillProfile, 0, len(entries))
	for _, entry := range entries {
		entryPath := filepath.Join(s.paths.ProfilesDir, entry.Name())
		if entry.IsDir() {
			nestedProfiles, loadErr := s.loadProfilesFromDir(entryPath)
			if loadErr != nil {
				return nil, loadErr
			}
			profiles = append(profiles, nestedProfiles...)
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		profile, err := s.loadProfileFromPath(entryPath)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}

	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].SkillName != profiles[j].SkillName {
			return profiles[i].SkillName < profiles[j].SkillName
		}
		return profiles[i].WorkspaceID < profiles[j].WorkspaceID
	})
	return profiles, nil
}

func decodeJSONLLines(path string, decode func(line []byte) error) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines [][]byte
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		lines = append(lines, append([]byte(nil), line...))
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	for i, line := range lines {
		if err := decode(line); err != nil {
			if i == len(lines)-1 && isInvalidJSON(err) {
				return nil
			}
			return err
		}
	}
	return nil
}

func draftKey(workspaceID, id string) string {
	return workspaceID + "\x00" + id
}

func isInvalidJSON(err error) bool {
	var syntaxErr *json.SyntaxError
	return errors.As(err, &syntaxErr)
}

func lockStoreFile(path string) (func(), error) {
	for {
		actual, _ := storeFileLocks.LoadOrStore(path, &sync.Mutex{})
		mu, ok := actual.(*sync.Mutex)
		if !ok || mu == nil {
			// Corrupted entry (wrong type or nil *sync.Mutex).
			// Atomically swap in a fresh mutex via CompareAndSwap.
			// If CAS fails, another goroutine already replaced it —
			// just retry the loop to pick up the valid entry.
			storeFileLocks.CompareAndSwap(path, actual, &sync.Mutex{})
			continue
		}
		mu.Lock()
		fileUnlock, err := fileutil.LockFile(path)
		if err != nil {
			mu.Unlock()
			return nil, fmt.Errorf("lock evolution state: %w", err)
		}
		return func() {
			if fileUnlock != nil {
				_ = fileUnlock()
			}
			mu.Unlock()
		}, nil
	}
}

func (s *Store) profilePath(workspaceID, skillName string) (string, error) {
	if err := validateEvolutionSkillTarget(skillName); err != nil {
		return "", err
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return filepath.Join(s.paths.ProfilesDir, skillName+".json"), nil
	}
	return filepath.Join(s.paths.ProfilesDir, workspaceScopeDir(workspaceID), skillName+".json"), nil
}

func (s *Store) loadProfilesFromDir(dir string) ([]SkillProfile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	profiles := make([]SkillProfile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		profile, err := s.loadProfileFromPath(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (s *Store) loadProfileFromPath(path string) (SkillProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillProfile{}, err
	}

	var profile SkillProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return SkillProfile{}, err
	}
	return profile, nil
}

func (s *Store) profileLookupPaths(workspaceID, skillName string) ([]string, error) {
	if err := validateEvolutionSkillTarget(skillName); err != nil {
		return nil, err
	}

	paths := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	appendPath := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		paths = append(paths, path)
		seen[path] = struct{}{}
	}

	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID != "" {
		path, err := s.profilePath(workspaceID, skillName)
		if err != nil {
			return nil, err
		}
		appendPath(path)
		if !usesDefaultWorkspaceState(s.paths, workspaceID) {
			return paths, nil
		}
	}

	legacyPath, err := s.profilePath("", skillName)
	if err != nil {
		return nil, err
	}
	appendPath(legacyPath)
	return paths, nil
}

func workspaceScopeDir(workspaceID string) string {
	sum := sha1.Sum([]byte(workspaceID))
	base := filepath.Base(filepath.Clean(workspaceID))
	base = sanitizeWorkspaceComponent(base)
	if base == "" || base == "." {
		base = "workspace"
	}
	return base + "-" + hex.EncodeToString(sum[:6])
}

func sanitizeWorkspaceComponent(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func validEvolutionControlID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// ValidControlID reports whether value is safe to use as an evolution draft
// or version selector. Store methods still enforce workspace ownership.
func ValidControlID(value string) bool {
	return validEvolutionControlID(value)
}
