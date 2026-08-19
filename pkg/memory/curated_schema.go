package memory

import (
	"strings"
	"time"
)

var curatedTypes = map[string]struct{}{
	CuratedTypeIdentity:                {},
	CuratedTypeCommunicationPreference: {},
	CuratedTypeWorkflowPreference:      {},
	CuratedTypeCorrection:              {},
	CuratedTypeEnvironment:             {},
	CuratedTypeProjectFact:             {},
	CuratedTypeRelationship:            {},
	CuratedTypeEpisodicFact:            {},
	CuratedTypeOther:                   {},
}

var curatedEvidenceKinds = map[string]struct{}{
	CuratedEvidenceExplicit: {},
	CuratedEvidenceObserved: {},
	CuratedEvidenceInferred: {},
	CuratedEvidenceLegacy:   {},
}

func NormalizeCuratedType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if _, ok := curatedTypes[value]; ok {
		return value
	}
	return CuratedTypeOther
}

func ValidCuratedType(value string) bool {
	_, ok := curatedTypes[strings.ToLower(strings.TrimSpace(value))]
	return ok
}

func NormalizeCuratedStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CuratedStatusSuperseded:
		return CuratedStatusSuperseded
	case CuratedStatusArchived:
		return CuratedStatusArchived
	default:
		return CuratedStatusActive
	}
}

func ValidCuratedStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CuratedStatusActive, CuratedStatusSuperseded, CuratedStatusArchived:
		return true
	default:
		return false
	}
}

func NormalizeEvidenceKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if _, ok := curatedEvidenceKinds[value]; ok {
		return value
	}
	return ""
}

func ValidEvidenceKind(value string) bool {
	_, ok := curatedEvidenceKinds[strings.ToLower(strings.TrimSpace(value))]
	return ok
}

var canonicalPreferenceAliases = map[string]string{
	"formatting.response_style":    "communication.response_format",
	"communication.response_style": "communication.response_format",
	"formatting.style":             "communication.response_format",
	"communication.format":         "communication.response_format",
	"workflow.quiz_format":         "presentation.quiz.mode",
	"presentation.quiz_format":     "presentation.quiz.mode",
	"presentation.quiz":            "presentation.quiz.mode",
	"workflow.poll_format":         "presentation.poll.mode",
	"presentation.poll_format":     "presentation.poll.mode",
	"presentation.poll":            "presentation.poll.mode",
	"interaction.buttons":          "interaction.button_style",
	"interaction.keyboard":         "interaction.button_style",
	"language.primary":             "communication.language",
	"language.preferred":           "communication.language",
	"language":                     "communication.language",
	"communication.lang":           "communication.language",
	"communication.style":          "communication.verbosity",
	"workflow.verbosity":           "communication.verbosity",
	"coding.style":                 "coding.formatting",
	"tooling.style":                "tooling.mode",
}

func NormalizePreferenceKey(value string) string {
	k := strings.ToLower(strings.TrimSpace(value))
	if canonical, ok := canonicalPreferenceAliases[k]; ok {
		return canonical
	}
	return k
}

func NormalizePreferenceValue(key, value string) string {
	key = NormalizePreferenceKey(key)
	val := strings.ToLower(strings.TrimSpace(value))
	if key == "presentation.quiz.mode" || key == "presentation.poll.mode" {
		switch val {
		case "telegram_native_quiz", "telegram_native", "native_quiz", "native":
			return "native"
		case "text_quiz", "plain_text", "plain", "text":
			return "text"
		case "automatic", "auto", "default":
			return "auto"
		}
	}
	return strings.TrimSpace(value)
}

func ValidPreferenceKey(value string) bool {
	value = NormalizePreferenceKey(value)
	if value == "" || len(value) > 96 {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		if i > 0 && (r == '.' || r == '_' || r == '-') {
			continue
		}
		return false
	}
	return true
}

func (entry CuratedEntry) EffectiveType() string {
	return NormalizeCuratedType(entry.Type)
}

func (entry CuratedEntry) EffectiveStatus() string {
	return NormalizeCuratedStatus(entry.Status)
}

func (entry CuratedEntry) EffectiveEvidenceKind() string {
	if value := NormalizeEvidenceKind(entry.EvidenceKind); value != "" {
		return value
	}
	// Legacy records predate evidence semantics. Background-review provenance
	// is conservatively treated as inference; other persisted records retain
	// legacy authority so upgrades do not silently weaken established memory.
	if strings.EqualFold(strings.TrimSpace(entry.Provenance.Source), "background_review") {
		return CuratedEvidenceInferred
	}
	return CuratedEvidenceLegacy
}

func DefaultConfidenceForEvidence(evidence string) float64 {
	switch NormalizeEvidenceKind(evidence) {
	case CuratedEvidenceExplicit:
		return 1.0
	case CuratedEvidenceObserved:
		return 0.72
	case CuratedEvidenceInferred:
		return 0.45
	default:
		return 1.0
	}
}

func maxConfidenceForEvidence(evidence string) float64 {
	switch NormalizeEvidenceKind(evidence) {
	case CuratedEvidenceInferred:
		return 0.60
	case CuratedEvidenceObserved:
		return 0.85
	default:
		return 1.0
	}
}

func normalizeConfidenceForEvidence(evidence string, confidence float64) float64 {
	if confidence <= 0 {
		confidence = DefaultConfidenceForEvidence(evidence)
	}
	if maxConfidence := maxConfidenceForEvidence(evidence); confidence > maxConfidence {
		return maxConfidence
	}
	return confidence
}

func (entry CuratedEntry) EffectiveConfidence() float64 {
	if entry.Confidence > 0 {
		if entry.Confidence > 1 {
			return 1
		}
		return entry.Confidence
	}
	return DefaultConfidenceForEvidence(entry.EffectiveEvidenceKind())
}

func (entry CuratedEntry) EvidenceAuthority() int {
	switch entry.EffectiveEvidenceKind() {
	case CuratedEvidenceExplicit:
		return 4
	case CuratedEvidenceLegacy:
		return 3
	case CuratedEvidenceObserved:
		return 2
	case CuratedEvidenceInferred:
		return 1
	default:
		return 0
	}
}

func (entry CuratedEntry) EffectiveLastConfirmedAt() *time.Time {
	if entry.LastConfirmedAt != nil {
		return entry.LastConfirmedAt
	}
	return entry.LastVerifiedAt
}

func (entry CuratedEntry) PromptEligible(now time.Time) bool {
	if entry.EffectiveStatus() != CuratedStatusActive {
		return false
	}
	return entry.ExpiresAt == nil || now.Before(entry.ExpiresAt.UTC())
}

func curatedTypeAllowedForTarget(target, entryType string) bool {
	if strings.EqualFold(strings.TrimSpace(target), CuratedTargetCurrentUser) {
		return true
	}
	switch NormalizeCuratedType(entryType) {
	case CuratedTypeIdentity,
		CuratedTypeCommunicationPreference,
		CuratedTypeWorkflowPreference,
		CuratedTypeRelationship,
		CuratedTypeEpisodicFact:
		return false
	default:
		return true
	}
}

func preferenceKeyAllowedForTarget(target, key string) bool {
	if strings.TrimSpace(key) == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(target), CuratedTargetCurrentUser)
}

func normalizedCuratedEntry(entry CuratedEntry) CuratedEntry {
	entry.Type = entry.EffectiveType()
	entry.Status = entry.EffectiveStatus()

	rawEvidence := NormalizeEvidenceKind(entry.EvidenceKind)
	legacyBackgroundInference := rawEvidence == "" &&
		strings.EqualFold(strings.TrimSpace(entry.Provenance.Source), "background_review")
	entry.EvidenceKind = entry.EffectiveEvidenceKind()
	entry.Visibility = entry.EffectiveVisibility()
	entry.PreferenceKey = NormalizePreferenceKey(entry.PreferenceKey)
	entry.PreferenceValue = NormalizePreferenceValue(entry.PreferenceKey, entry.PreferenceValue)
	if entry.EvidenceCount < 0 {
		entry.EvidenceCount = 0
	}
	if entry.ObservationCount < 0 {
		entry.ObservationCount = 0
	}
	if entry.EvidenceKind == CuratedEvidenceObserved {
		if entry.ObservationCount == 0 {
			entry.ObservationCount = entry.EvidenceCount
		}
		// "observed" means repeated behavior. A single observation is only a
		// cautious inference and must not receive observed authority.
		if entry.ObservationCount < 2 {
			entry.EvidenceKind = CuratedEvidenceInferred
			entry.Confidence = DefaultConfidenceForEvidence(CuratedEvidenceInferred)
		}
	}
	if legacyBackgroundInference {
		// V1 background-review records were historically stored as confidence=1
		// and auto-verified even though they could be model inference. On upgrade,
		// reinterpret only this known-unsafe legacy provenance conservatively.
		entry.Confidence = DefaultConfidenceForEvidence(CuratedEvidenceInferred)
	} else {
		entry.Confidence = normalizeConfidenceForEvidence(entry.EvidenceKind, entry.EffectiveConfidence())
	}
	if entry.EvidenceKind == CuratedEvidenceObserved || entry.EvidenceKind == CuratedEvidenceInferred {
		// Confirmation is direct user evidence. If a record is no longer explicit,
		// stale confirmation timestamps must not make it look user-verified.
		entry.LastConfirmedAt = nil
		entry.LastVerifiedAt = nil
	}
	return entry
}

// NormalizeFactText standardizes text for conservative fact deduplication.
func NormalizeFactText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	var sb strings.Builder
	for _, r := range text {
		switch r {
		case '.', '!', '?', ',', ';', ':', '"', '\'', '(', ')':
			sb.WriteRune(' ')
			continue
		}
		sb.WriteRune(r)
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

// IsObviousFactDuplicate conservatively identifies whether two fact statements are duplicates.
// Contradictory or distinct facts are never merged.
func IsObviousFactDuplicate(fact1, fact2 string) bool {
	norm1 := NormalizeFactText(fact1)
	norm2 := NormalizeFactText(fact2)
	if norm1 == "" || norm2 == "" {
		return false
	}
	if norm1 == norm2 {
		return true
	}

	words1 := strings.Fields(norm1)
	words2 := strings.Fields(norm2)

	// Identify polarity/negation tokens to avoid merging contradictions
	polarityTokens := map[string]struct{}{
		"not": {}, "no": {}, "never": {}, "un": {}, "unavailable": {}, "available": {},
		"disabled": {}, "enabled": {}, "failed": {}, "success": {}, "error": {},
		"broken": {}, "stop": {}, "stopped": {}, "started": {}, "none": {},
	}

	pol1 := make(map[string]struct{})
	for _, w := range words1 {
		if _, ok := polarityTokens[w]; ok {
			pol1[w] = struct{}{}
		}
	}
	pol2 := make(map[string]struct{})
	for _, w := range words2 {
		if _, ok := polarityTokens[w]; ok {
			pol2[w] = struct{}{}
		}
	}

	// If polarity keywords differ, keep them separate
	if len(pol1) != len(pol2) {
		return false
	}
	for p := range pol1 {
		if _, ok := pol2[p]; !ok {
			return false
		}
	}

	// Filter common stop words
	stopWords := map[string]struct{}{
		"a": {}, "an": {}, "the": {}, "is": {}, "are": {}, "was": {}, "were": {},
		"in": {}, "on": {}, "at": {}, "to": {}, "for": {}, "of": {}, "by": {},
		"through": {}, "it": {}, "its": {},
	}

	sig1 := make(map[string]struct{})
	for _, w := range words1 {
		if _, isStop := stopWords[w]; !isStop {
			sig1[w] = struct{}{}
		}
	}
	sig2 := make(map[string]struct{})
	for _, w := range words2 {
		if _, isStop := stopWords[w]; !isStop {
			sig2[w] = struct{}{}
		}
	}

	if len(sig1) == 0 || len(sig2) == 0 {
		return false
	}

	// If significant non-stopwords match exactly
	if len(sig1) == len(sig2) {
		matchesAll := true
		for w := range sig1 {
			if _, ok := sig2[w]; !ok {
				matchesAll = false
				break
			}
		}
		if matchesAll {
			return true
		}
	}

	return false
}
