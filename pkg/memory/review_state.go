package memory

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

const reviewStateVersion = 1

type ReviewCursor struct {
	SessionRef             string    `json:"session_ref"`
	ScopeDigest            string    `json:"scope_digest"`
	SuccessfulTurns        int       `json:"successful_turns"`
	LastReviewedSequence   uint64    `json:"last_reviewed_sequence"`
	LastSuccessfulReviewAt time.Time `json:"last_successful_review_at,omitempty"`
	LastAttemptAt          time.Time `json:"last_attempt_at,omitempty"`
}

type reviewStateDocument struct {
	Version int                     `json:"version"`
	Cursors map[string]ReviewCursor `json:"cursors"`
}

// ReviewStateStore persists counters and successful-review cursors so restarts
// neither reset the interval nor cause already-reviewed transcripts to repeat.
type ReviewStateStore struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
}

func NewReviewStateStore(root string) (*ReviewStateStore, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil, fmt.Errorf("review state root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create review state directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure review state directory: %w", err)
	}
	return &ReviewStateStore{path: filepath.Join(root, "review_state.json"), now: time.Now}, nil
}

func (s *ReviewStateStore) readDocument() (reviewStateDocument, error) {
	doc := reviewStateDocument{Version: reviewStateVersion, Cursors: map[string]ReviewCursor{}}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return reviewStateDocument{}, fmt.Errorf("read memory review state: %w", err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return reviewStateDocument{}, fmt.Errorf("decode memory review state: %w", err)
	}
	if doc.Cursors == nil {
		doc.Cursors = map[string]ReviewCursor{}
	}
	if doc.Version == 0 {
		doc.Version = reviewStateVersion
	}
	return doc, nil
}

func (s *ReviewStateStore) writeDocument(doc reviewStateDocument) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode memory review state: %w", err)
	}
	if err := fileutil.WriteFileAtomic(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write memory review state: %w", err)
	}
	return os.Chmod(s.path, 0o600)
}

func (s *ReviewStateStore) RecordSuccessfulTurn(caller CallerScope) (ReviewCursor, error) {
	key, cursorIdentity, err := reviewCursorIdentity(caller)
	if err != nil {
		return ReviewCursor{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDocument()
	if err != nil {
		return ReviewCursor{}, err
	}
	cursor := doc.Cursors[key]
	cursor.SessionRef = cursorIdentity.SessionRef
	cursor.ScopeDigest = cursorIdentity.ScopeDigest
	cursor.SuccessfulTurns++
	doc.Cursors[key] = cursor
	if err := s.writeDocument(doc); err != nil {
		return ReviewCursor{}, err
	}
	return cursor, nil
}

func (s *ReviewStateStore) Get(caller CallerScope) (ReviewCursor, error) {
	key, cursorIdentity, err := reviewCursorIdentity(caller)
	if err != nil {
		return ReviewCursor{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDocument()
	if err != nil {
		return ReviewCursor{}, err
	}
	cursor := doc.Cursors[key]
	cursor.SessionRef = cursorIdentity.SessionRef
	cursor.ScopeDigest = cursorIdentity.ScopeDigest
	return cursor, nil
}

func (s *ReviewStateStore) MarkAttempt(caller CallerScope) error {
	key, cursorIdentity, err := reviewCursorIdentity(caller)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDocument()
	if err != nil {
		return err
	}
	cursor := doc.Cursors[key]
	cursor.SessionRef = cursorIdentity.SessionRef
	cursor.ScopeDigest = cursorIdentity.ScopeDigest
	cursor.LastAttemptAt = s.now().UTC()
	doc.Cursors[key] = cursor
	return s.writeDocument(doc)
}

func (s *ReviewStateStore) MarkSuccessfulReview(
	caller CallerScope,
	sequence uint64,
	reviewedTurns int,
) error {
	key, cursorIdentity, err := reviewCursorIdentity(caller)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDocument()
	if err != nil {
		return err
	}
	cursor := doc.Cursors[key]
	cursor.SessionRef = cursorIdentity.SessionRef
	cursor.ScopeDigest = cursorIdentity.ScopeDigest
	if reviewedTurns >= cursor.SuccessfulTurns {
		cursor.SuccessfulTurns = 0
	} else if reviewedTurns > 0 {
		cursor.SuccessfulTurns -= reviewedTurns
	}
	if sequence > cursor.LastReviewedSequence {
		cursor.LastReviewedSequence = sequence
	}
	cursor.LastSuccessfulReviewAt = s.now().UTC()
	doc.Cursors[key] = cursor
	return s.writeDocument(doc)
}

func (s *ReviewStateStore) ForgetSession(sessionRef string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDocument()
	if err != nil {
		return err
	}
	sessionRef = strings.TrimSpace(sessionRef)
	for key, cursor := range doc.Cursors {
		if cursor.SessionRef == sessionRef || key == sessionRef {
			delete(doc.Cursors, key)
		}
	}
	return s.writeDocument(doc)
}

func reviewCursorIdentity(caller CallerScope) (string, ReviewCursor, error) {
	sessionRef := strings.TrimSpace(caller.SessionRef)
	userDigest := digestUserKey(caller.UserKey)
	if sessionRef == "" || userDigest == "" {
		return "", ReviewCursor{}, ErrUserScopeUnavailable
	}
	material := strings.Join([]string{
		sessionRef,
		userDigest,
		normalizeRecallDimension(caller.Channel),
		normalizeRecallDimension(caller.Account),
	}, "\x00")
	digest := sha256.Sum256([]byte(material))
	scopeDigest := fmt.Sprintf("%x", digest[:])
	return "scope_" + scopeDigest, ReviewCursor{
		SessionRef:  sessionRef,
		ScopeDigest: scopeDigest,
	}, nil
}
