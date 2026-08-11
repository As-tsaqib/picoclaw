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

func (entry CuratedEntry) EffectiveType() string {
	return NormalizeCuratedType(entry.Type)
}

func (entry CuratedEntry) EffectiveStatus() string {
	return NormalizeCuratedStatus(entry.Status)
}

func (entry CuratedEntry) EffectiveConfidence() float64 {
	if entry.Confidence <= 0 {
		return 1
	}
	if entry.Confidence > 1 {
		return 1
	}
	return entry.Confidence
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

func normalizedCuratedEntry(entry CuratedEntry) CuratedEntry {
	entry.Type = entry.EffectiveType()
	entry.Status = entry.EffectiveStatus()
	entry.Confidence = entry.EffectiveConfidence()
	return entry
}
