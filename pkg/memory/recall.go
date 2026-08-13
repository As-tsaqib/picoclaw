package memory

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/As-tsaqib/picoclaw/pkg/fileutil"
)

const (
	RecallModeIsolated    = "isolated"
	RecallModeUserRecall  = "user_recall"
	RecallModeGroupRecall = "group_recall"

	recallDocumentVersion = 1
	maxRecallRecordChars  = 2_000
	maxRecallExcerptChars = 600
)

type RecallRecord struct {
	ID         string    `json:"id"`
	Sequence   uint64    `json:"sequence"`
	TurnID     string    `json:"turn_id"`
	UserDigest string    `json:"user_digest,omitempty"`
	Channel    string    `json:"channel,omitempty"`
	Account    string    `json:"account,omitempty"`
	GroupID    string    `json:"group_id,omitempty"`
	TopicID    string    `json:"topic_id,omitempty"`
	TopicName  string    `json:"topic_name,omitempty"`
	SessionRef string    `json:"session_ref"`
	Timestamp  time.Time `json:"timestamp"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	MessageRef string    `json:"message_ref,omitempty"`
}

type RecallResult struct {
	TopicID    string    `json:"topic_id,omitempty"`
	TopicName  string    `json:"topic_name,omitempty"`
	SessionRef string    `json:"session_ref"`
	Timestamp  time.Time `json:"timestamp"`
	Role       string    `json:"role"`
	Excerpt    string    `json:"excerpt"`
	MessageRef string    `json:"message_ref,omitempty"`
	Score      float64   `json:"score"`
}

type RecallSearchOptions struct {
	Mode       string
	MaxResults int
	MaxChars   int
}

type recallDocument struct {
	Version      int            `json:"version"`
	NextSequence uint64         `json:"next_sequence"`
	Records      []RecallRecord `json:"records"`
}

// RecallStore is a lightweight bounded lexical archive of successfully
// delivered turns. It does not merge histories into prompts; callers must use
// Search, whose server-side scope checks cannot be overridden by model args.
type RecallStore struct {
	path       string
	maxRecords int
	now        func() time.Time
	random     io.Reader
	mu         sync.RWMutex
}

func NewRecallStore(root string, maxRecords int) (*RecallStore, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil, fmt.Errorf("recall root is required")
	}
	if maxRecords <= 0 {
		maxRecords = 2_000
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create recall directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure recall directory: %w", err)
	}
	return &RecallStore{
		path:       filepath.Join(root, "recall.json"),
		maxRecords: maxRecords,
		now:        time.Now,
		random:     rand.Reader,
	}, nil
}

func (s *RecallStore) readDocument() (recallDocument, error) {
	doc := recallDocument{Version: recallDocumentVersion, NextSequence: 1, Records: []RecallRecord{}}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return recallDocument{}, fmt.Errorf("read recall index: %w", err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return recallDocument{}, fmt.Errorf("decode recall index: %w", err)
	}
	if doc.Version == 0 {
		doc.Version = recallDocumentVersion
	}
	if doc.NextSequence == 0 {
		for _, record := range doc.Records {
			if record.Sequence >= doc.NextSequence {
				doc.NextSequence = record.Sequence + 1
			}
		}
		if doc.NextSequence == 0 {
			doc.NextSequence = 1
		}
	}
	if doc.Records == nil {
		doc.Records = []RecallRecord{}
	}
	return doc, nil
}

func (s *RecallStore) writeDocument(doc recallDocument) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode recall index: %w", err)
	}
	if err := fileutil.WriteFileAtomic(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write recall index: %w", err)
	}
	return os.Chmod(s.path, 0o600)
}

// AppendDeliveredTurn stores only the user input and assistant text that made
// it through the delivery acknowledgement path. It returns the durable turn
// sequence used by the background-review cursor.
func (s *RecallStore) AppendDeliveredTurn(
	caller CallerScope,
	turnID string,
	userContent string,
	assistantContent string,
	assistantMessageRef string,
) (uint64, error) {
	if strings.TrimSpace(caller.SessionRef) == "" {
		return 0, fmt.Errorf("recall session reference is required")
	}
	userContent = sanitizeRecallContent(userContent)
	assistantContent = sanitizeRecallContent(assistantContent)
	if userContent == "" && assistantContent == "" {
		return 0, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDocument()
	if err != nil {
		return 0, err
	}
	sequence := doc.NextSequence
	doc.NextSequence++
	if strings.TrimSpace(turnID) == "" {
		turnID, err = randomRecallID(s.random, "turn")
		if err != nil {
			return 0, err
		}
	}
	timestamp := s.now().UTC()
	appendRecord := func(role, content, messageRef string) error {
		if content == "" {
			return nil
		}
		id, err := randomRecallID(s.random, "rec")
		if err != nil {
			return err
		}
		doc.Records = append(doc.Records, RecallRecord{
			ID:         id,
			Sequence:   sequence,
			TurnID:     turnID,
			UserDigest: digestUserKey(caller.UserKey),
			Channel:    normalizeRecallDimension(caller.Channel),
			Account:    normalizeRecallDimension(caller.Account),
			GroupID:    normalizeRecallDimension(caller.GroupID),
			TopicID:    strings.TrimSpace(caller.TopicID),
			TopicName:  compactSingleLine(caller.TopicName, 160),
			SessionRef: caller.SessionRef,
			Timestamp:  timestamp,
			Role:       role,
			Content:    content,
			MessageRef: strings.TrimSpace(messageRef),
		})
		return nil
	}
	if err := appendRecord("user", userContent, caller.MessageRef); err != nil {
		return 0, err
	}
	if err := appendRecord("assistant", assistantContent, assistantMessageRef); err != nil {
		return 0, err
	}
	if overflow := len(doc.Records) - s.maxRecords; overflow > 0 {
		doc.Records = append([]RecallRecord(nil), doc.Records[overflow:]...)
	}
	if err := s.writeDocument(doc); err != nil {
		return 0, err
	}
	return sequence, nil
}

func (s *RecallStore) Search(
	caller CallerScope,
	query string,
	opts RecallSearchOptions,
) ([]RecallResult, error) {
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "" {
		mode = RecallModeIsolated
	}
	if mode == RecallModeIsolated {
		return []RecallResult{}, nil
	}
	if mode != RecallModeUserRecall && mode != RecallModeGroupRecall {
		return nil, fmt.Errorf("unsupported recall mode")
	}
	queryTokens := lexicalTokens(query)
	if len(queryTokens) == 0 {
		return []RecallResult{}, nil
	}
	if opts.MaxResults <= 0 || opts.MaxResults > 20 {
		opts.MaxResults = 5
	}
	if opts.MaxChars <= 0 || opts.MaxChars > 20_000 {
		opts.MaxChars = 4_000
	}
	userDigest := digestUserKey(caller.UserKey)
	if mode == RecallModeUserRecall && userDigest == "" {
		return nil, ErrUserScopeUnavailable
	}
	groupID := normalizeRecallDimension(caller.GroupID)
	if mode == RecallModeGroupRecall && groupID == "" {
		return []RecallResult{}, nil
	}

	s.mu.RLock()
	doc, err := s.readDocument()
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	now := s.now()
	results := make([]RecallResult, 0)
	for _, record := range doc.Records {
		if record.SessionRef == caller.SessionRef {
			continue
		}
		if !recallRecordAllowed(record, caller, userDigest, groupID, mode) {
			continue
		}
		counts := lexicalTokenCounts(record.Content + " " + record.TopicName)
		matches := 0
		uniqueMatches := 0
		for token := range queryTokens {
			if count := counts[token]; count > 0 {
				uniqueMatches++
				matches += min(count, 4)
			}
		}
		if uniqueMatches == 0 {
			continue
		}
		coverage := float64(uniqueMatches) / float64(len(queryTokens))
		ageDays := math.Max(0, now.Sub(record.Timestamp).Hours()/24)
		recency := 1 / (1 + ageDays/30)
		score := float64(matches) + coverage*3 + recency
		results = append(results, RecallResult{
			TopicID:    record.TopicID,
			TopicName:  record.TopicName,
			SessionRef: record.SessionRef,
			Timestamp:  record.Timestamp,
			Role:       record.Role,
			Excerpt:    matchingExcerpt(record.Content, queryTokens, maxRecallExcerptChars),
			MessageRef: record.MessageRef,
			Score:      score,
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Timestamp.After(results[j].Timestamp)
	})
	if len(results) > opts.MaxResults {
		results = results[:opts.MaxResults]
	}
	bounded := make([]RecallResult, 0, len(results))
	chars := 0
	for _, result := range results {
		remaining := opts.MaxChars - chars
		if remaining <= 0 {
			break
		}
		if utf8.RuneCountInString(result.Excerpt) > remaining {
			result.Excerpt = truncateRunes(result.Excerpt, remaining)
		}
		chars += utf8.RuneCountInString(result.Excerpt)
		bounded = append(bounded, result)
	}
	return bounded, nil
}

func recallRecordAllowed(
	record RecallRecord,
	caller CallerScope,
	userDigest string,
	groupID string,
	mode string,
) bool {
	if normalizeRecallDimension(record.Channel) != normalizeRecallDimension(caller.Channel) {
		return false
	}
	if normalizeRecallDimension(record.Account) != normalizeRecallDimension(caller.Account) {
		return false
	}
	switch mode {
	case RecallModeUserRecall:
		return record.UserDigest != "" && record.UserDigest == userDigest
	case RecallModeGroupRecall:
		return normalizeRecallDimension(record.GroupID) == groupID
	default:
		return false
	}
}

func (s *RecallStore) RecordsAfter(caller CallerScope, sequence uint64, maxChars int) ([]RecallRecord, uint64, error) {
	if maxChars <= 0 || maxChars > 20_000 {
		maxChars = 8_000
	}
	if maxChars < 512 {
		maxChars = 512
	}
	sessionRef := strings.TrimSpace(caller.SessionRef)
	userDigest := digestUserKey(caller.UserKey)
	if sessionRef == "" || userDigest == "" {
		return nil, sequence, ErrUserScopeUnavailable
	}
	s.mu.RLock()
	doc, err := s.readDocument()
	s.mu.RUnlock()
	if err != nil {
		return nil, sequence, err
	}
	eligible := make([]RecallRecord, 0)
	for _, record := range doc.Records {
		if record.SessionRef != sessionRef || record.Sequence <= sequence ||
			record.UserDigest != userDigest ||
			normalizeRecallDimension(record.Channel) != normalizeRecallDimension(caller.Channel) ||
			normalizeRecallDimension(record.Account) != normalizeRecallDimension(caller.Account) {
			continue
		}
		eligible = append(eligible, record)
	}

	var out []RecallRecord
	latest := sequence
	chars := 0
	for start := 0; start < len(eligible); {
		end := start + 1
		for end < len(eligible) && eligible[end].Sequence == eligible[start].Sequence {
			end++
		}
		group := eligible[start:end]
		groupChars := 0
		for _, record := range group {
			groupChars += utf8.RuneCountInString(record.Content)
		}
		remaining := maxChars - chars
		if groupChars > remaining && len(out) > 0 {
			break
		}
		for index, record := range group {
			copyRecord := record
			recordBudget := utf8.RuneCountInString(copyRecord.Content)
			if groupChars > remaining {
				messagesLeft := len(group) - index
				recordBudget = max(1, remaining/messagesLeft)
			}
			copyRecord.Content = truncateRunes(copyRecord.Content, recordBudget)
			used := utf8.RuneCountInString(copyRecord.Content)
			remaining -= used
			chars += used
			out = append(out, copyRecord)
		}
		latest = group[0].Sequence
		start = end
	}
	return out, latest, nil
}

func (s *RecallStore) ForgetSession(sessionRef string) error {
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDocument()
	if err != nil {
		return err
	}
	filtered := doc.Records[:0]
	for _, record := range doc.Records {
		if record.SessionRef != sessionRef {
			filtered = append(filtered, record)
		}
	}
	if len(filtered) == len(doc.Records) {
		return nil
	}
	doc.Records = append([]RecallRecord(nil), filtered...)
	return s.writeDocument(doc)
}

func sanitizeRecallContent(content string) string {
	content = RedactMemoryText(strings.TrimSpace(content))
	return truncateRunes(content, maxRecallRecordChars)
}

func digestUserKey(userKey string) string {
	userKey = strings.TrimSpace(userKey)
	if userKey == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(userKey))
	return hex.EncodeToString(digest[:])
}

func randomRecallID(source io.Reader, prefix string) (string, error) {
	var raw [8]byte
	if _, err := io.ReadFull(source, raw[:]); err != nil {
		return "", fmt.Errorf("generate recall id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(raw[:]), nil
}

func normalizeRecallDimension(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func matchingExcerpt(content string, query map[string]struct{}, maxChars int) string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) <= maxChars {
		return string(runes)
	}
	lower := strings.ToLower(content)
	matchByte := -1
	for token := range query {
		if idx := strings.Index(lower, token); idx >= 0 && (matchByte < 0 || idx < matchByte) {
			matchByte = idx
		}
	}
	if matchByte < 0 {
		return truncateRunes(content, maxChars)
	}
	matchRune := utf8.RuneCountInString(content[:matchByte])
	start := max(0, matchRune-maxChars/3)
	end := min(len(runes), start+maxChars)
	excerpt := string(runes[start:end])
	if start > 0 {
		excerpt = "…" + excerpt
	}
	if end < len(runes) {
		excerpt += "…"
	}
	return excerpt
}

func truncateRunes(value string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxChars {
		return string(runes)
	}
	if maxChars == 1 {
		return "…"
	}
	return string(runes[:maxChars-1]) + "…"
}

func compactSingleLine(value string, maxChars int) string {
	return truncateRunes(strings.Join(strings.Fields(value), " "), maxChars)
}
