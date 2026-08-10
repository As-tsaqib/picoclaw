package memory

import (
	"crypto/rand"
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

const (
	CheckpointStatusActive    = "active"
	CheckpointStatusSuspended = "suspended"
	CheckpointStatusCompleted = "completed"
	CheckpointStatusArchived  = "archived"

	CheckpointActionCreate   = "create"
	CheckpointActionUpdate   = "update"
	CheckpointActionSuspend  = "suspend"
	CheckpointActionResume   = "resume"
	CheckpointActionComplete = "complete"
	CheckpointActionArchive  = "archive"
	CheckpointActionDelete   = "delete"

	checkpointDocumentVersion = 1
)

var (
	ErrCheckpointNotFound     = errors.New("task checkpoint not found in the current session")
	ErrCheckpointInvalid      = errors.New("invalid task checkpoint mutation")
	ErrCheckpointCapacity     = errors.New("task checkpoint capacity exceeded")
	ErrCheckpointNotResumable = errors.New("task checkpoint is not resumable")
)

type CheckpointProvenance struct {
	SessionKey string `json:"session_key"`
	SessionRef string `json:"session_ref"`
	Channel    string `json:"channel,omitempty"`
	Account    string `json:"account,omitempty"`
	ChatID     string `json:"chat_id,omitempty"`
	GroupID    string `json:"group_id,omitempty"`
	TopicID    string `json:"topic_id,omitempty"`
	TopicName  string `json:"topic_name,omitempty"`
}

type CheckpointDelivery struct {
	Excerpt     string    `json:"excerpt"`
	MessageRef  string    `json:"message_ref,omitempty"`
	DeliveredAt time.Time `json:"delivered_at"`
}

type TaskCheckpoint struct {
	ID               string               `json:"id"`
	Kind             string               `json:"kind"`
	Title            string               `json:"title"`
	Objective        string               `json:"objective"`
	Status           string               `json:"status"`
	CompletedItems   []string             `json:"completed_items,omitempty"`
	CurrentStep      string               `json:"current_step,omitempty"`
	NextStep         string               `json:"next_step,omitempty"`
	ImportantContext string               `json:"important_context,omitempty"`
	LastDelivered    *CheckpointDelivery  `json:"last_delivered,omitempty"`
	Provenance       CheckpointProvenance `json:"provenance"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

// CheckpointMutation uses pointer fields for updates so omitted values are
// distinct from explicit clears.
type CheckpointMutation struct {
	Action           string
	ID               string
	Kind             *string
	Title            *string
	Objective        *string
	CompletedItems   *[]string
	CurrentStep      *string
	NextStep         *string
	ImportantContext *string
	Query            string
}

type CheckpointStoreOptions struct {
	MaxCount           int
	MaxContextChars    int
	CompletedRetention time.Duration
	Now                func() time.Time
	Random             io.Reader
}

type checkpointDocument struct {
	Version     int              `json:"version"`
	Checkpoints []TaskCheckpoint `json:"checkpoints"`
}

type pendingCheckpointDocument struct {
	TurnID       string
	SessionKey   string
	PreviousTurn string
	Document     checkpointDocument
	Touched      map[string]checkpointTouch
}

type checkpointTouch struct {
	UpdatedAt time.Time
	Deleted   bool
}

type AmbiguousCheckpointError struct {
	Candidates []TaskCheckpoint
}

func (e *AmbiguousCheckpointError) Error() string {
	return "multiple task checkpoints are equally plausible"
}

// CheckpointStore stages turn mutations in memory and commits them only from
// the post-delivery acknowledgement path. A canceled or interrupted turn can
// discard its staged snapshot without falsely advancing task progress.
type CheckpointStore struct {
	path               string
	maxCount           int
	maxContextChars    int
	completedRetention time.Duration
	now                func() time.Time
	random             io.Reader
	mu                 sync.Mutex
	pending            map[string]*pendingCheckpointDocument
	pendingBySession   map[string]string
}

func NewCheckpointStore(root string, opts CheckpointStoreOptions) (*CheckpointStore, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil, fmt.Errorf("checkpoint root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create checkpoint directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure checkpoint directory: %w", err)
	}
	if opts.MaxCount <= 0 {
		opts.MaxCount = 100
	}
	if opts.MaxContextChars <= 0 {
		opts.MaxContextChars = 2_000
	}
	if opts.CompletedRetention <= 0 {
		opts.CompletedRetention = 90 * 24 * time.Hour
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Random == nil {
		opts.Random = rand.Reader
	}
	return &CheckpointStore{
		path:               filepath.Join(root, "checkpoints.json"),
		maxCount:           opts.MaxCount,
		maxContextChars:    opts.MaxContextChars,
		completedRetention: opts.CompletedRetention,
		now:                opts.Now,
		random:             opts.Random,
		pending:            make(map[string]*pendingCheckpointDocument),
		pendingBySession:   make(map[string]string),
	}, nil
}

func (s *CheckpointStore) readDocument() (checkpointDocument, error) {
	doc := checkpointDocument{Version: checkpointDocumentVersion, Checkpoints: []TaskCheckpoint{}}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return checkpointDocument{}, fmt.Errorf("read checkpoints: %w", err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return checkpointDocument{}, fmt.Errorf("decode checkpoints: %w", err)
	}
	if doc.Version == 0 {
		doc.Version = checkpointDocumentVersion
	}
	if doc.Checkpoints == nil {
		doc.Checkpoints = []TaskCheckpoint{}
	}
	return doc, nil
}

func (s *CheckpointStore) writeDocument(doc checkpointDocument) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode checkpoints: %w", err)
	}
	if err := fileutil.WriteFileAtomic(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write checkpoints: %w", err)
	}
	return os.Chmod(s.path, 0o600)
}

func (s *CheckpointStore) Apply(
	caller CallerScope,
	turnID string,
	mutation CheckpointMutation,
) (TaskCheckpoint, error) {
	if strings.TrimSpace(caller.SessionKey) == "" {
		return TaskCheckpoint{}, ErrCheckpointInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		doc, err := s.readDocument()
		if err != nil {
			return TaskCheckpoint{}, err
		}
		checkpoint, err := s.applyMutation(&doc, caller, mutation)
		if err != nil {
			return TaskCheckpoint{}, err
		}
		if err := s.writeDocument(doc); err != nil {
			return TaskCheckpoint{}, err
		}
		return checkpoint, nil
	}

	pending, err := s.pendingForTurn(turnID, caller.SessionKey)
	if err != nil {
		return TaskCheckpoint{}, err
	}
	checkpoint, err := s.applyMutation(&pending.Document, caller, mutation)
	if err != nil {
		return TaskCheckpoint{}, err
	}
	if checkpoint.ID != "" {
		pending.Touched[checkpoint.ID] = checkpointTouch{
			UpdatedAt: checkpoint.UpdatedAt,
			Deleted:   mutation.Action == CheckpointActionDelete,
		}
	}
	return checkpoint, nil
}

func (s *CheckpointStore) pendingForTurn(turnID, sessionKey string) (*pendingCheckpointDocument, error) {
	if pending := s.pending[turnID]; pending != nil {
		if pending.SessionKey != sessionKey {
			return nil, ErrCheckpointInvalid
		}
		return pending, nil
	}
	doc, err := s.readDocument()
	if err != nil {
		return nil, err
	}
	previousTurn := s.pendingBySession[sessionKey]
	if previous := s.pending[previousTurn]; previous != nil {
		doc = cloneCheckpointDocument(previous.Document)
	}
	pending := &pendingCheckpointDocument{
		TurnID:       turnID,
		SessionKey:   sessionKey,
		PreviousTurn: previousTurn,
		Document:     doc,
		Touched:      make(map[string]checkpointTouch),
	}
	s.pending[turnID] = pending
	s.pendingBySession[sessionKey] = turnID
	return pending, nil
}

func (s *CheckpointStore) applyMutation(
	doc *checkpointDocument,
	caller CallerScope,
	mutation CheckpointMutation,
) (TaskCheckpoint, error) {
	mutation.Action = strings.ToLower(strings.TrimSpace(mutation.Action))
	mutation.ID = strings.TrimSpace(mutation.ID)
	now := s.now().UTC()
	pruneExpiredCheckpoints(doc, now, s.completedRetention)

	if mutation.Action == CheckpointActionCreate {
		if len(doc.Checkpoints) >= s.maxCount {
			return TaskCheckpoint{}, ErrCheckpointCapacity
		}
		kind := pointerString(mutation.Kind)
		title := pointerString(mutation.Title)
		objective := pointerString(mutation.Objective)
		if kind == "" || title == "" || objective == "" {
			return TaskCheckpoint{}, ErrCheckpointInvalid
		}
		if err := validateCheckpointFields(kind, title, objective); err != nil {
			return TaskCheckpoint{}, err
		}
		id, err := s.newCheckpointID(doc.Checkpoints)
		if err != nil {
			return TaskCheckpoint{}, err
		}
		checkpoint := TaskCheckpoint{
			ID:               id,
			Kind:             compactSingleLine(kind, 80),
			Title:            compactSingleLine(title, 240),
			Objective:        truncateRunes(objective, 1_000),
			Status:           CheckpointStatusActive,
			CompletedItems:   cloneStringSlice(pointerStrings(mutation.CompletedItems)),
			CurrentStep:      truncateRunes(pointerString(mutation.CurrentStep), 1_000),
			NextStep:         truncateRunes(pointerString(mutation.NextStep), 1_000),
			ImportantContext: truncateRunes(pointerString(mutation.ImportantContext), s.maxContextChars),
			Provenance:       checkpointProvenance(caller),
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := validateCheckpoint(checkpoint, s.maxContextChars); err != nil {
			return TaskCheckpoint{}, err
		}
		doc.Checkpoints = append(doc.Checkpoints, checkpoint)
		return checkpoint, nil
	}

	idx := checkpointIndex(doc.Checkpoints, mutation.ID, caller.SessionKey)
	if idx < 0 {
		return TaskCheckpoint{}, ErrCheckpointNotFound
	}
	checkpoint := &doc.Checkpoints[idx]
	switch mutation.Action {
	case CheckpointActionUpdate:
		if checkpoint.Status == CheckpointStatusArchived {
			return TaskCheckpoint{}, ErrCheckpointInvalid
		}
		applyCheckpointFields(checkpoint, mutation, s.maxContextChars)
	case CheckpointActionSuspend:
		if checkpoint.Status != CheckpointStatusActive {
			return TaskCheckpoint{}, ErrCheckpointInvalid
		}
		checkpoint.Status = CheckpointStatusSuspended
	case CheckpointActionResume:
		if checkpoint.Status != CheckpointStatusActive && checkpoint.Status != CheckpointStatusSuspended {
			return TaskCheckpoint{}, ErrCheckpointNotResumable
		}
		checkpoint.Status = CheckpointStatusActive
		applyCheckpointFields(checkpoint, mutation, s.maxContextChars)
	case CheckpointActionComplete:
		if checkpoint.Status == CheckpointStatusArchived {
			return TaskCheckpoint{}, ErrCheckpointInvalid
		}
		applyCheckpointFields(checkpoint, mutation, s.maxContextChars)
		checkpoint.Status = CheckpointStatusCompleted
	case CheckpointActionArchive:
		checkpoint.Status = CheckpointStatusArchived
	case CheckpointActionDelete:
		deleted := *checkpoint
		deleted.UpdatedAt = now
		doc.Checkpoints = append(doc.Checkpoints[:idx], doc.Checkpoints[idx+1:]...)
		return deleted, nil
	default:
		return TaskCheckpoint{}, ErrCheckpointInvalid
	}
	checkpoint.UpdatedAt = now
	if err := validateCheckpoint(*checkpoint, s.maxContextChars); err != nil {
		return TaskCheckpoint{}, err
	}
	return *checkpoint, nil
}

func (s *CheckpointStore) List(caller CallerScope, includeCompleted bool) ([]TaskCheckpoint, error) {
	return s.ListForTurn(caller, "", includeCompleted)
}

// ListForTurn includes mutations already staged by the current turn so a model
// can create a checkpoint and then inspect or update it again before delivery.
// A blank turn ID intentionally exposes durable state only.
func (s *CheckpointStore) ListForTurn(
	caller CallerScope,
	turnID string,
	includeCompleted bool,
) ([]TaskCheckpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pending := s.pending[strings.TrimSpace(turnID)]; pending != nil && pending.SessionKey == caller.SessionKey {
		return listCheckpoints(pending.Document, caller.SessionKey, includeCompleted), nil
	}
	doc, err := s.readDocument()
	if err != nil {
		return nil, err
	}
	return listCheckpoints(doc, caller.SessionKey, includeCompleted), nil
}

func (s *CheckpointStore) Get(caller CallerScope, id string) (TaskCheckpoint, error) {
	return s.GetForTurn(caller, "", id)
}

func (s *CheckpointStore) GetForTurn(caller CallerScope, turnID, id string) (TaskCheckpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var doc checkpointDocument
	if pending := s.pending[strings.TrimSpace(turnID)]; pending != nil && pending.SessionKey == caller.SessionKey {
		doc = pending.Document
	} else {
		var err error
		doc, err = s.readDocument()
		if err != nil {
			return TaskCheckpoint{}, err
		}
	}
	idx := checkpointIndex(doc.Checkpoints, strings.TrimSpace(id), caller.SessionKey)
	if idx < 0 {
		return TaskCheckpoint{}, ErrCheckpointNotFound
	}
	return cloneCheckpoint(doc.Checkpoints[idx]), nil
}

// ResolveContinuation selects the latest relevant active/suspended checkpoint.
// Equal top scores are surfaced as an ambiguity so the agent can ask the user.
func (s *CheckpointStore) ResolveContinuation(caller CallerScope, query string) (TaskCheckpoint, error) {
	return s.ResolveContinuationForTurn(caller, "", query)
}

func (s *CheckpointStore) ResolveContinuationForTurn(
	caller CallerScope,
	turnID string,
	query string,
) (TaskCheckpoint, error) {
	checkpoints, err := s.ListForTurn(caller, turnID, false)
	if err != nil {
		return TaskCheckpoint{}, err
	}
	queryTokens := lexicalTokens(query)
	type scoredCheckpoint struct {
		checkpoint TaskCheckpoint
		score      int
	}
	scored := make([]scoredCheckpoint, 0, len(checkpoints))
	for _, checkpoint := range checkpoints {
		if checkpoint.Status != CheckpointStatusActive && checkpoint.Status != CheckpointStatusSuspended {
			continue
		}
		counts := lexicalTokenCounts(strings.Join([]string{
			checkpoint.Kind,
			checkpoint.Title,
			checkpoint.Objective,
			checkpoint.CurrentStep,
			checkpoint.NextStep,
		}, " "))
		score := 0
		for token := range queryTokens {
			score += counts[token]
		}
		scored = append(scored, scoredCheckpoint{checkpoint: checkpoint, score: score})
	}
	if len(scored) == 0 {
		return TaskCheckpoint{}, ErrCheckpointNotFound
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].checkpoint.UpdatedAt.After(scored[j].checkpoint.UpdatedAt)
	})
	if len(scored) > 1 && scored[0].score == scored[1].score {
		// If the query has no useful task terms, recency is the deterministic
		// tie-breaker only when there is a clearly newer checkpoint.
		if scored[0].score > 0 || scored[0].checkpoint.UpdatedAt.Equal(scored[1].checkpoint.UpdatedAt) {
			candidates := []TaskCheckpoint{scored[0].checkpoint, scored[1].checkpoint}
			return TaskCheckpoint{}, &AmbiguousCheckpointError{Candidates: candidates}
		}
	}
	return scored[0].checkpoint, nil
}

// CommitDelivered merges only the delivered turn's touched checkpoints into
// durable state. This preserves updates committed concurrently by other
// sessions/topics instead of replacing the shared document with a stale
// snapshot. Older same-session commits cannot overwrite newer checkpoint
// mutations because UpdatedAt is used as a conflict guard.
func (s *CheckpointStore) CommitDelivered(
	turnID string,
	sessionKey string,
	excerpt string,
	messageRef string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	turnID = strings.TrimSpace(turnID)
	pending := s.pending[turnID]
	if pending == nil || pending.SessionKey != sessionKey {
		return nil
	}
	defer s.removePendingLocked(pending)

	doc, err := s.readDocument()
	if err != nil {
		return err
	}
	pruneExpiredCheckpoints(&doc, s.now().UTC(), s.completedRetention)
	delivery := &CheckpointDelivery{
		Excerpt:     sanitizeRecallContent(excerpt),
		MessageRef:  strings.TrimSpace(messageRef),
		DeliveredAt: s.now().UTC(),
	}
	ids := make([]string, 0, len(pending.Touched))
	for id := range pending.Touched {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		touch := pending.Touched[id]
		durableIdx := checkpointIndex(doc.Checkpoints, id, sessionKey)
		if touch.Deleted {
			if durableIdx >= 0 && !doc.Checkpoints[durableIdx].UpdatedAt.After(touch.UpdatedAt) {
				doc.Checkpoints = append(doc.Checkpoints[:durableIdx], doc.Checkpoints[durableIdx+1:]...)
			}
			continue
		}

		pendingIdx := checkpointIndex(pending.Document.Checkpoints, id, sessionKey)
		if pendingIdx < 0 {
			continue
		}
		candidate := cloneCheckpoint(pending.Document.Checkpoints[pendingIdx])
		if durableIdx >= 0 && doc.Checkpoints[durableIdx].UpdatedAt.After(candidate.UpdatedAt) {
			continue
		}
		copyDelivery := *delivery
		candidate.LastDelivered = &copyDelivery
		candidate.UpdatedAt = delivery.DeliveredAt
		if durableIdx >= 0 {
			doc.Checkpoints[durableIdx] = candidate
			continue
		}
		if len(doc.Checkpoints) >= s.maxCount {
			return ErrCheckpointCapacity
		}
		doc.Checkpoints = append(doc.Checkpoints, candidate)
	}
	sort.SliceStable(doc.Checkpoints, func(i, j int) bool {
		return doc.Checkpoints[i].CreatedAt.Before(doc.Checkpoints[j].CreatedAt)
	})
	if err := s.writeDocument(doc); err != nil {
		return err
	}
	return nil
}

func (s *CheckpointStore) DiscardTurn(turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending[strings.TrimSpace(turnID)]
	if pending == nil {
		return
	}
	s.removePendingLocked(pending)
}

func (s *CheckpointStore) removePendingLocked(pending *pendingCheckpointDocument) {
	if pending == nil {
		return
	}
	delete(s.pending, pending.TurnID)
	if s.pendingBySession[pending.SessionKey] == pending.TurnID {
		if previous := s.pending[pending.PreviousTurn]; previous != nil {
			s.pendingBySession[pending.SessionKey] = previous.TurnID
		} else {
			delete(s.pendingBySession, pending.SessionKey)
		}
	}
}

func (s *CheckpointStore) DiscardSession(sessionKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearPendingSessionLocked(sessionKey)
}

func (s *CheckpointStore) clearPendingSessionLocked(sessionKey string) {
	for turnID, pending := range s.pending {
		if pending.SessionKey == sessionKey {
			delete(s.pending, turnID)
		}
	}
	delete(s.pendingBySession, sessionKey)
}

func checkpointIndex(checkpoints []TaskCheckpoint, id, sessionKey string) int {
	if !validCheckpointID(id) {
		return -1
	}
	for i := range checkpoints {
		if checkpoints[i].ID == id && checkpoints[i].Provenance.SessionKey == sessionKey {
			return i
		}
	}
	return -1
}

func listCheckpoints(doc checkpointDocument, sessionKey string, includeCompleted bool) []TaskCheckpoint {
	out := make([]TaskCheckpoint, 0)
	for _, checkpoint := range doc.Checkpoints {
		if checkpoint.Provenance.SessionKey != sessionKey || checkpoint.Status == CheckpointStatusArchived {
			continue
		}
		if !includeCompleted && checkpoint.Status == CheckpointStatusCompleted {
			continue
		}
		out = append(out, cloneCheckpoint(checkpoint))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func applyCheckpointFields(checkpoint *TaskCheckpoint, mutation CheckpointMutation, maxContextChars int) {
	if mutation.Kind != nil {
		checkpoint.Kind = compactSingleLine(*mutation.Kind, 80)
	}
	if mutation.Title != nil {
		checkpoint.Title = compactSingleLine(*mutation.Title, 240)
	}
	if mutation.Objective != nil {
		checkpoint.Objective = truncateRunes(*mutation.Objective, 1_000)
	}
	if mutation.CompletedItems != nil {
		checkpoint.CompletedItems = cloneStringSlice(*mutation.CompletedItems)
	}
	if mutation.CurrentStep != nil {
		checkpoint.CurrentStep = truncateRunes(*mutation.CurrentStep, 1_000)
	}
	if mutation.NextStep != nil {
		checkpoint.NextStep = truncateRunes(*mutation.NextStep, 1_000)
	}
	if mutation.ImportantContext != nil {
		checkpoint.ImportantContext = truncateRunes(*mutation.ImportantContext, maxContextChars)
	}
}

func validateCheckpoint(checkpoint TaskCheckpoint, maxContextChars int) error {
	if checkpoint.Kind == "" || checkpoint.Title == "" || checkpoint.Objective == "" {
		return ErrCheckpointInvalid
	}
	if utf8.RuneCountInString(checkpoint.ImportantContext) > maxContextChars {
		return ErrCheckpointCapacity
	}
	values := []string{
		checkpoint.Kind,
		checkpoint.Title,
		checkpoint.Objective,
		checkpoint.CurrentStep,
		checkpoint.NextStep,
		checkpoint.ImportantContext,
	}
	values = append(values, checkpoint.CompletedItems...)
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if err := ValidateCuratedContent(value); err != nil {
			return err
		}
	}
	return nil
}

func validateCheckpointFields(values ...string) error {
	for _, value := range values {
		if err := ValidateCuratedContent(value); err != nil {
			return err
		}
	}
	return nil
}

func checkpointProvenance(caller CallerScope) CheckpointProvenance {
	return CheckpointProvenance{
		SessionKey: caller.SessionKey,
		SessionRef: caller.SessionRef,
		Channel:    caller.Channel,
		Account:    caller.Account,
		ChatID:     caller.ChatID,
		GroupID:    caller.GroupID,
		TopicID:    caller.TopicID,
		TopicName:  caller.TopicName,
	}
}

func pruneExpiredCheckpoints(doc *checkpointDocument, now time.Time, retention time.Duration) {
	if retention <= 0 {
		return
	}
	cutoff := now.Add(-retention)
	filtered := doc.Checkpoints[:0]
	for _, checkpoint := range doc.Checkpoints {
		if (checkpoint.Status == CheckpointStatusCompleted || checkpoint.Status == CheckpointStatusArchived) &&
			checkpoint.UpdatedAt.Before(cutoff) {
			continue
		}
		filtered = append(filtered, checkpoint)
	}
	doc.Checkpoints = append([]TaskCheckpoint(nil), filtered...)
}

func (s *CheckpointStore) newCheckpointID(checkpoints []TaskCheckpoint) (string, error) {
	known := make(map[string]struct{}, len(checkpoints))
	for _, checkpoint := range checkpoints {
		known[checkpoint.ID] = struct{}{}
	}
	for range 8 {
		var raw [8]byte
		if _, err := io.ReadFull(s.random, raw[:]); err != nil {
			return "", fmt.Errorf("generate checkpoint id: %w", err)
		}
		id := "cp_" + hex.EncodeToString(raw[:])
		if _, exists := known[id]; !exists {
			return id, nil
		}
	}
	return "", fmt.Errorf("generate unique checkpoint id")
}

func validCheckpointID(id string) bool {
	if !strings.HasPrefix(id, "cp_") || len(id) != len("cp_")+16 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, "cp_"))
	return err == nil
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func pointerStrings(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}

func cloneStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, truncateRunes(value, 500))
		}
	}
	return out
}

func cloneCheckpoint(checkpoint TaskCheckpoint) TaskCheckpoint {
	checkpoint.CompletedItems = append([]string(nil), checkpoint.CompletedItems...)
	if checkpoint.LastDelivered != nil {
		delivery := *checkpoint.LastDelivered
		checkpoint.LastDelivered = &delivery
	}
	return checkpoint
}

func cloneCheckpointDocument(doc checkpointDocument) checkpointDocument {
	out := checkpointDocument{Version: doc.Version, Checkpoints: make([]TaskCheckpoint, len(doc.Checkpoints))}
	for i := range doc.Checkpoints {
		out.Checkpoints[i] = cloneCheckpoint(doc.Checkpoints[i])
	}
	return out
}
