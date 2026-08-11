package memory

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type curatedScoredEntry struct {
	entry CuratedEntry
	score float64
}

// Retrieve returns bounded active memory selected for a concrete request. It
// deliberately works on one already-authorized target; trusted caller scope
// and agent-root isolation remain enforced by CuratedStore.scopePath.
func (s *CuratedStore) Retrieve(
	target string,
	caller CallerScope,
	opts CuratedRetrievalOptions,
) (CuratedRetrievalResult, error) {
	entries, err := s.List(target, caller)
	if err != nil {
		return CuratedRetrievalResult{}, err
	}
	now := opts.Now.UTC()
	if now.IsZero() {
		now = s.now().UTC()
	}
	eligible := make([]CuratedEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.PromptEligible(now) {
			eligible = append(eligible, entry)
		}
	}
	if len(eligible) == 0 {
		return CuratedRetrievalResult{}, nil
	}

	if opts.MaxResults <= 0 || opts.MaxResults > 100 {
		opts.MaxResults = 8
	}
	if opts.MaxChars <= 0 {
		opts.MaxChars = 4_000
	}
	if opts.PinnedChars <= 0 || opts.PinnedChars > opts.MaxChars {
		opts.PinnedChars = minIntValue(opts.MaxChars, 1_200)
	}
	if opts.RecencyHalfLifeDays <= 0 {
		opts.RecencyHalfLifeDays = 90
	}
	if opts.StaleAfterDays <= 0 {
		opts.StaleAfterDays = 180
	}
	if opts.RecencyWeight < 0 {
		opts.RecencyWeight = 0
	}
	if opts.FuzzyWeight < 0 {
		opts.FuzzyWeight = 0
	}
	if opts.RecentFallbackCount < 0 {
		opts.RecentFallbackCount = 0
	}

	queryCounts := lexicalTokenCounts(opts.Query)
	documentFrequency := make(map[string]int)
	for _, entry := range eligible {
		for token := range lexicalTokenCounts(curatedSearchText(entry)) {
			documentFrequency[token]++
		}
	}

	pinned := make([]curatedScoredEntry, 0)
	scored := make([]curatedScoredEntry, 0, len(eligible))
	for _, entry := range eligible {
		score := curatedRelevanceScore(entry, queryCounts, documentFrequency, len(eligible), opts, now)
		item := curatedScoredEntry{entry: entry, score: score}
		if entry.Pinned {
			pinned = append(pinned, item)
			continue
		}
		if len(queryCounts) == 0 || score >= opts.MinimumScore {
			scored = append(scored, item)
		}
	}
	sortCuratedScores(pinned)
	sortCuratedScores(scored)

	selected := make([]CuratedEntry, 0, opts.MaxResults)
	seen := make(map[string]struct{}, opts.MaxResults)
	used := 0
	pinnedUsed := 0
	appendEntry := func(entry CuratedEntry, charBudget int) bool {
		if len(selected) >= opts.MaxResults || used >= opts.MaxChars || charBudget <= 0 {
			return false
		}
		if _, ok := seen[entry.ID]; ok {
			return true
		}
		remaining := minIntValue(opts.MaxChars-used, charBudget)
		if remaining <= 0 {
			return false
		}
		entry = normalizedCuratedEntry(entry)
		entry.Content = truncateCuratedRunes(entry.Content, remaining)
		chars := utf8.RuneCountInString(entry.Content)
		if chars == 0 {
			return true
		}
		selected = append(selected, entry)
		seen[entry.ID] = struct{}{}
		used += chars
		return true
	}
	for _, item := range pinned {
		remainingPinned := opts.PinnedChars - pinnedUsed
		before := used
		if !appendEntry(item.entry, remainingPinned) {
			break
		}
		pinnedUsed += used - before
	}

	if len(queryCounts) == 0 {
		// With no meaningful query, include only the explicitly configured
		// recent fallback. Type bonuses must not turn an empty-query prompt into
		// an implicit dump of the whole store.
		sort.SliceStable(scored, func(i, j int) bool {
			if !scored[i].entry.UpdatedAt.Equal(scored[j].entry.UpdatedAt) {
				return scored[i].entry.UpdatedAt.After(scored[j].entry.UpdatedAt)
			}
			return scored[i].entry.ID < scored[j].entry.ID
		})
		if opts.RecentFallbackCount == 0 {
			scored = nil
		} else if len(scored) > opts.RecentFallbackCount {
			scored = scored[:opts.RecentFallbackCount]
		}
	}
	for _, item := range scored {
		if !appendEntry(item.entry, opts.MaxChars-used) {
			break
		}
	}
	return CuratedRetrievalResult{Entries: selected, Characters: used}, nil
}

func curatedRelevanceScore(
	entry CuratedEntry,
	query map[string]int,
	documentFrequency map[string]int,
	documentCount int,
	opts CuratedRetrievalOptions,
	now time.Time,
) float64 {
	content := lexicalTokenCounts(curatedSearchText(entry))
	queryTotal := 0
	bm25 := 0.0
	matched := 0
	for token, queryTF := range query {
		queryTotal += queryTF
		tf := content[token]
		if tf == 0 {
			continue
		}
		matched += minIntValue(tf, queryTF)
		df := documentFrequency[token]
		idf := math.Log(1 + (float64(documentCount-df)+0.5)/(float64(df)+0.5))
		bm25 += idf * (float64(tf) * 2.2 / (float64(tf) + 1.2))
	}
	overlap := 0.0
	if queryTotal > 0 {
		overlap = float64(matched) / float64(queryTotal)
	}
	fuzzy := 0.0
	if len(query) > 0 && opts.FuzzyWeight > 0 {
		fuzzy = trigramSimilarity(strings.Join(sortedTokenKeys(query), " "), strings.ToLower(curatedSearchText(entry)))
	}
	if len(query) > 0 && matched == 0 && fuzzy < 0.08 {
		return -1
	}

	ageDays := now.Sub(entry.UpdatedAt.UTC()).Hours() / 24
	if ageDays < 0 {
		ageDays = 0
	}
	halfLife := curatedTypeRecencyHalfLife(entry.EffectiveType(), opts.RecencyHalfLifeDays)
	recency := math.Pow(0.5, ageDays/halfLife) * opts.RecencyWeight
	stalePenalty := 0.0
	staleAfter := curatedTypeStaleThreshold(entry.EffectiveType(), opts.StaleAfterDays)
	if staleAfter > 0 && ageDays > staleAfter {
		stalePenalty = math.Min(0.75, 0.25*(ageDays/staleAfter))
	}
	presentation := 0.0
	presentedAt := entry.LastPresentedAt
	if presentedAt == nil {
		presentedAt = entry.LastUsedAt // compatibility with v1 stores
	}
	if presentedAt != nil {
		presentedAgeDays := now.Sub(presentedAt.UTC()).Hours() / 24
		if presentedAgeDays < 0 {
			presentedAgeDays = 0
		}
		presentation = 0.03 * math.Pow(0.5, presentedAgeDays/30)
	}
	confirmation := 0.0
	if confirmedAt := entry.EffectiveLastConfirmedAt(); confirmedAt != nil {
		confirmedAgeDays := now.Sub(confirmedAt.UTC()).Hours() / 24
		if confirmedAgeDays < 0 {
			confirmedAgeDays = 0
		}
		confirmation = 0.10 * math.Pow(0.5, confirmedAgeDays/180)
	}
	typeBonus := curatedTypeScore(entry.EffectiveType())
	confidence := 0.15 * entry.EffectiveConfidence()
	evidence := 0.04 * float64(entry.EvidenceAuthority())
	pinnedBonus := 0.0
	if entry.Pinned {
		pinnedBonus = 2
	}
	return bm25 + (overlap * 2) + (fuzzy * opts.FuzzyWeight) + recency + presentation + confirmation + typeBonus + confidence + evidence + pinnedBonus - stalePenalty
}

func curatedSearchText(entry CuratedEntry) string {
	parts := []string{entry.Content}
	if key := NormalizePreferenceKey(entry.PreferenceKey); key != "" {
		parts = append(parts, strings.ReplaceAll(key, ".", " "), entry.PreferenceValue)
	}
	return strings.Join(parts, " ")
}

func curatedTypeRecencyHalfLife(entryType string, fallback float64) float64 {
	if fallback <= 0 {
		fallback = 90
	}
	switch entryType {
	case CuratedTypeIdentity, CuratedTypeCommunicationPreference, CuratedTypeCorrection:
		return fallback * 8
	case CuratedTypeWorkflowPreference, CuratedTypeRelationship:
		return fallback * 3
	case CuratedTypeEnvironment:
		return fallback * 1.5
	case CuratedTypeEpisodicFact:
		return math.Max(14, fallback*0.35)
	default:
		return fallback
	}
}

func curatedTypeStaleThreshold(entryType string, fallback float64) float64 {
	if fallback <= 0 {
		fallback = 180
	}
	switch entryType {
	case CuratedTypeIdentity, CuratedTypeCommunicationPreference, CuratedTypeCorrection:
		return 0 // stable until explicitly superseded/expired
	case CuratedTypeWorkflowPreference, CuratedTypeRelationship:
		return fallback * 3
	case CuratedTypeEnvironment:
		return fallback * 1.5
	case CuratedTypeEpisodicFact:
		return math.Max(30, fallback*0.5)
	default:
		return fallback
	}
}

func curatedTypeScore(entryType string) float64 {
	switch entryType {
	case CuratedTypeCorrection:
		return 0.75
	case CuratedTypeCommunicationPreference, CuratedTypeWorkflowPreference:
		return 0.45
	case CuratedTypeIdentity, CuratedTypeProjectFact, CuratedTypeEnvironment:
		return 0.25
	default:
		return 0.1
	}
}

func sortCuratedScores(values []curatedScoredEntry) {
	sort.SliceStable(values, func(i, j int) bool {
		if math.Abs(values[i].score-values[j].score) > 0.0000001 {
			return values[i].score > values[j].score
		}
		if !values[i].entry.UpdatedAt.Equal(values[j].entry.UpdatedAt) {
			return values[i].entry.UpdatedAt.After(values[j].entry.UpdatedAt)
		}
		return values[i].entry.ID < values[j].entry.ID
	})
}

func sortedTokenKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func trigramSimilarity(left, right string) float64 {
	a := trigrams(left)
	b := trigrams(right)
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for value := range a {
		if _, ok := b[value]; ok {
			intersection++
		}
	}
	return (2 * float64(intersection)) / float64(len(a)+len(b))
}

func trigrams(value string) map[string]struct{} {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	runes := []rune(value)
	if len(runes) < 3 {
		if len(runes) == 0 {
			return nil
		}
		return map[string]struct{}{string(runes): {}}
	}
	out := make(map[string]struct{}, len(runes)-2)
	for i := 0; i+3 <= len(runes); i++ {
		out[string(runes[i:i+3])] = struct{}{}
	}
	return out
}

func truncateCuratedRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	if limit <= 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func minIntValue(a, b int) int {
	if a < b {
		return a
	}
	return b
}
