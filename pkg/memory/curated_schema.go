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

func NormalizePreferenceKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
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
	if max := maxConfidenceForEvidence(evidence); confidence > max {
		return max
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
	entry.PreferenceKey = NormalizePreferenceKey(entry.PreferenceKey)
	entry.PreferenceValue = strings.TrimSpace(entry.PreferenceValue)
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
