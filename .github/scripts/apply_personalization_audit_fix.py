#!/usr/bin/env python3
from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if old not in text:
        raise SystemExit(f"expected pattern not found in {path}: {old[:120]!r}")
    if text.count(old) != 1:
        raise SystemExit(f"expected exactly one pattern in {path}, found {text.count(old)}")
    p.write_text(text.replace(old, new, 1))


def append_once(path: str, marker: str, block: str) -> None:
    p = Path(path)
    text = p.read_text()
    if marker in text:
        return
    p.write_text(text.rstrip() + "\n\n" + block.strip() + "\n")


# Evidence semantics: cap weak evidence confidence, require repeated observations,
# and ensure non-explicit evidence never carries user-confirmation timestamps.
replace_once(
    "pkg/memory/curated_schema.go",
    '''func DefaultConfidenceForEvidence(evidence string) float64 {
\tswitch NormalizeEvidenceKind(evidence) {
\tcase CuratedEvidenceExplicit:
\t\treturn 1.0
\tcase CuratedEvidenceObserved:
\t\treturn 0.72
\tcase CuratedEvidenceInferred:
\t\treturn 0.45
\tdefault:
\t\treturn 1.0
\t}
}
''',
    '''func DefaultConfidenceForEvidence(evidence string) float64 {
\tswitch NormalizeEvidenceKind(evidence) {
\tcase CuratedEvidenceExplicit:
\t\treturn 1.0
\tcase CuratedEvidenceObserved:
\t\treturn 0.72
\tcase CuratedEvidenceInferred:
\t\treturn 0.45
\tdefault:
\t\treturn 1.0
\t}
}

func maxConfidenceForEvidence(evidence string) float64 {
\tswitch NormalizeEvidenceKind(evidence) {
\tcase CuratedEvidenceInferred:
\t\treturn 0.60
\tcase CuratedEvidenceObserved:
\t\treturn 0.85
\tdefault:
\t\treturn 1.0
\t}
}

func normalizeConfidenceForEvidence(evidence string, confidence float64) float64 {
\tif confidence <= 0 {
\t\tconfidence = DefaultConfidenceForEvidence(evidence)
\t}
\tif max := maxConfidenceForEvidence(evidence); confidence > max {
\t\treturn max
\t}
\treturn confidence
}
''',
)
replace_once(
    "pkg/memory/curated_schema.go",
    '''\tentry.EvidenceKind = entry.EffectiveEvidenceKind()
\tif legacyBackgroundInference {
\t\t// V1 background-review records were historically stored as confidence=1
\t\t// and auto-verified even though they could be model inference. On upgrade,
\t\t// reinterpret only this known-unsafe legacy provenance conservatively.
\t\tentry.Confidence = DefaultConfidenceForEvidence(CuratedEvidenceInferred)
\t\tentry.LastConfirmedAt = nil
\t\tentry.LastVerifiedAt = nil
\t} else {
\t\tentry.Confidence = entry.EffectiveConfidence()
\t}
\tentry.PreferenceKey = NormalizePreferenceKey(entry.PreferenceKey)
\tentry.PreferenceValue = strings.TrimSpace(entry.PreferenceValue)
\tif entry.EvidenceCount < 0 {
\t\tentry.EvidenceCount = 0
\t}
\tif entry.ObservationCount < 0 {
\t\tentry.ObservationCount = 0
\t}
\treturn entry
''',
    '''\tentry.EvidenceKind = entry.EffectiveEvidenceKind()
\tentry.PreferenceKey = NormalizePreferenceKey(entry.PreferenceKey)
\tentry.PreferenceValue = strings.TrimSpace(entry.PreferenceValue)
\tif entry.EvidenceCount < 0 {
\t\tentry.EvidenceCount = 0
\t}
\tif entry.ObservationCount < 0 {
\t\tentry.ObservationCount = 0
\t}
\tif entry.EvidenceKind == CuratedEvidenceObserved {
\t\tif entry.ObservationCount == 0 {
\t\t\tentry.ObservationCount = entry.EvidenceCount
\t\t}
\t\t// "observed" means repeated behavior. A single observation is only a
\t\t// cautious inference and must not receive observed authority.
\t\tif entry.ObservationCount < 2 {
\t\t\tentry.EvidenceKind = CuratedEvidenceInferred
\t\t\tentry.Confidence = DefaultConfidenceForEvidence(CuratedEvidenceInferred)
\t\t}
\t}
\tif legacyBackgroundInference {
\t\t// V1 background-review records were historically stored as confidence=1
\t\t// and auto-verified even though they could be model inference. On upgrade,
\t\t// reinterpret only this known-unsafe legacy provenance conservatively.
\t\tentry.Confidence = DefaultConfidenceForEvidence(CuratedEvidenceInferred)
\t} else {
\t\tentry.Confidence = normalizeConfidenceForEvidence(entry.EvidenceKind, entry.EffectiveConfidence())
\t}
\tif entry.EvidenceKind == CuratedEvidenceObserved || entry.EvidenceKind == CuratedEvidenceInferred {
\t\t// Confirmation is direct user evidence. If a record is no longer explicit,
\t\t// stale confirmation timestamps must not make it look user-verified.
\t\tentry.LastConfirmedAt = nil
\t\tentry.LastVerifiedAt = nil
\t}
\treturn entry
''',
)

# Store invariants: structured supersedes must target the same dimension,
# replace must not refresh confirmation implicitly, and restore must reconcile.
replace_once(
    "pkg/memory/curated_store.go",
    '''\tknown := make(map[string]struct{}, len(doc.Entries)+len(mutations))
\tknownStatus := make(map[string]string, len(doc.Entries)+len(mutations))
\tfor _, entry := range doc.Entries {
\t\tknown[entry.ID] = struct{}{}
\t\tknownStatus[entry.ID] = entry.EffectiveStatus()
\t}
''',
    '''\tknown := make(map[string]struct{}, len(doc.Entries)+len(mutations))
\tknownStatus := make(map[string]string, len(doc.Entries)+len(mutations))
\tknownPreferenceKey := make(map[string]string, len(doc.Entries)+len(mutations))
\tfor _, entry := range doc.Entries {
\t\tknown[entry.ID] = struct{}{}
\t\tknownStatus[entry.ID] = entry.EffectiveStatus()
\t\tknownPreferenceKey[entry.ID] = NormalizePreferenceKey(entry.PreferenceKey)
\t}
''',
)
replace_once(
    "pkg/memory/curated_store.go",
    '''\t\t\tif mutation.Supersedes != "" {
\t\t\t\tif !validStableEntryID(mutation.Supersedes) {
\t\t\t\t\treturn nil, ErrCuratedInvalidAction
\t\t\t\t}
\t\t\t\tif _, exists := known[mutation.Supersedes]; !exists {
\t\t\t\t\treturn nil, ErrCuratedEntryNotFound
\t\t\t\t}
\t\t\t\tif status := knownStatus[mutation.Supersedes]; status != CuratedStatusActive {
\t\t\t\t\treturn nil, ErrCuratedInvalidAction
\t\t\t\t}
\t\t\t}
''',
    '''\t\t\tif mutation.Supersedes != "" {
\t\t\t\tif !validStableEntryID(mutation.Supersedes) {
\t\t\t\t\treturn nil, ErrCuratedInvalidAction
\t\t\t\t}
\t\t\t\tif _, exists := known[mutation.Supersedes]; !exists {
\t\t\t\t\treturn nil, ErrCuratedEntryNotFound
\t\t\t\t}
\t\t\t\tif status := knownStatus[mutation.Supersedes]; status != CuratedStatusActive {
\t\t\t\t\treturn nil, ErrCuratedInvalidAction
\t\t\t\t}
\t\t\t\tif priorKey := knownPreferenceKey[mutation.Supersedes]; mutation.PreferenceKey != "" &&
\t\t\t\t\tpriorKey != "" && priorKey != mutation.PreferenceKey {
\t\t\t\t\treturn nil, ErrCuratedInvalidPreferenceKey
\t\t\t\t}
\t\t\t}
''',
)
replace_once(
    "pkg/memory/curated_store.go",
    '''\t\t\tknown[mutation.ID] = struct{}{}
\t\t\tknownStatus[mutation.ID] = CuratedStatusActive
\t\t\tif mutation.Supersedes != "" {
\t\t\t\tknownStatus[mutation.Supersedes] = CuratedStatusSuperseded
\t\t\t}
''',
    '''\t\t\tknown[mutation.ID] = struct{}{}
\t\t\tknownStatus[mutation.ID] = CuratedStatusActive
\t\t\tknownPreferenceKey[mutation.ID] = mutation.PreferenceKey
\t\t\tif mutation.Supersedes != "" {
\t\t\t\tknownStatus[mutation.Supersedes] = CuratedStatusSuperseded
\t\t\t}
''',
)
replace_once(
    "pkg/memory/curated_store.go",
    '''\t\t\tif mutation.Supersedes != "" && mutation.Supersedes != mutation.ID {
\t\t\t\tif !validStableEntryID(mutation.Supersedes) {
\t\t\t\t\treturn nil, ErrCuratedInvalidAction
\t\t\t\t}
\t\t\t\tif _, exists := known[mutation.Supersedes]; !exists {
\t\t\t\t\treturn nil, ErrCuratedEntryNotFound
\t\t\t\t}
\t\t\t}
''',
    '''\t\t\teffectivePreferenceKey := mutation.PreferenceKey
\t\t\tif effectivePreferenceKey == "" {
\t\t\t\teffectivePreferenceKey = knownPreferenceKey[mutation.ID]
\t\t\t}
\t\t\tif mutation.Supersedes != "" && mutation.Supersedes != mutation.ID {
\t\t\t\tif !validStableEntryID(mutation.Supersedes) {
\t\t\t\t\treturn nil, ErrCuratedInvalidAction
\t\t\t\t}
\t\t\t\tif _, exists := known[mutation.Supersedes]; !exists {
\t\t\t\t\treturn nil, ErrCuratedEntryNotFound
\t\t\t\t}
\t\t\t\tif status := knownStatus[mutation.Supersedes]; status != CuratedStatusActive {
\t\t\t\t\treturn nil, ErrCuratedInvalidAction
\t\t\t\t}
\t\t\t\tif priorKey := knownPreferenceKey[mutation.Supersedes]; effectivePreferenceKey != "" &&
\t\t\t\t\tpriorKey != "" && priorKey != effectivePreferenceKey {
\t\t\t\t\treturn nil, ErrCuratedInvalidPreferenceKey
\t\t\t\t}
\t\t\t\tknownStatus[mutation.Supersedes] = CuratedStatusSuperseded
\t\t\t}
\t\t\tknownPreferenceKey[mutation.ID] = effectivePreferenceKey
''',
)
replace_once(
    "pkg/memory/curated_store.go",
    '''\t\t\tif mutation.LastConfirmedAt != nil {
\t\t\t\tentries[idx].LastConfirmedAt = mutation.LastConfirmedAt
\t\t\t\tentries[idx].LastVerifiedAt = mutation.LastConfirmedAt
\t\t\t} else if mutation.LastVerifiedAt != nil {
\t\t\t\tentries[idx].LastVerifiedAt = mutation.LastVerifiedAt
\t\t\t\tentries[idx].LastConfirmedAt = mutation.LastVerifiedAt
\t\t\t} else if entries[idx].EffectiveEvidenceKind() == CuratedEvidenceExplicit {
\t\t\t\tconfirmed := now
\t\t\t\tentries[idx].LastConfirmedAt = &confirmed
\t\t\t\tentries[idx].LastVerifiedAt = &confirmed
\t\t\t}
''',
    '''\t\t\tif mutation.LastConfirmedAt != nil {
\t\t\t\tentries[idx].LastConfirmedAt = mutation.LastConfirmedAt
\t\t\t\tentries[idx].LastVerifiedAt = mutation.LastConfirmedAt
\t\t\t} else if mutation.LastVerifiedAt != nil {
\t\t\t\tentries[idx].LastVerifiedAt = mutation.LastVerifiedAt
\t\t\t\tentries[idx].LastConfirmedAt = mutation.LastVerifiedAt
\t\t\t} else if NormalizeEvidenceKind(mutation.EvidenceKind) == CuratedEvidenceExplicit {
\t\t\t\t// Only a mutation that explicitly carries direct-user evidence refreshes
\t\t\t\t// confirmation. A curator rewrite of an existing explicit memory does not.
\t\t\t\tconfirmed := now
\t\t\t\tentries[idx].LastConfirmedAt = &confirmed
\t\t\t\tentries[idx].LastVerifiedAt = &confirmed
\t\t\t} else if mutation.EvidenceKind != "" {
\t\t\t\tentries[idx].LastConfirmedAt = nil
\t\t\t\tentries[idx].LastVerifiedAt = nil
\t\t\t}
''',
)
replace_once(
    "pkg/memory/curated_store.go",
    '''\t\tcase CuratedActionRestore:
\t\t\tif idx < 0 {
\t\t\t\treturn nil, nil, ErrCuratedEntryNotFound
\t\t\t}
\t\t\tentries[idx].Status = CuratedStatusActive
\t\t\tentries[idx].ArchivedAt = nil
\t\t\tentries[idx].UpdatedAt = now
\t\t\tentries[idx].Provenance = mutation.Provenance
\t\t\tentries[idx] = normalizedCuratedEntry(entries[idx])
\t\t\tapplied = append(applied, entries[idx])
''',
    '''\t\tcase CuratedActionRestore:
\t\t\tif idx < 0 {
\t\t\t\treturn nil, nil, ErrCuratedEntryNotFound
\t\t\t}
\t\t\tentries[idx].Status = CuratedStatusActive
\t\t\tentries[idx].ArchivedAt = nil
\t\t\tentries[idx].UpdatedAt = now
\t\t\tentries[idx].Provenance = mutation.Provenance
\t\t\tentries[idx] = normalizedCuratedEntry(entries[idx])
\t\t\treconcilePreferenceKey(entries, mutation.ID, now)
\t\t\tif current, ok := curatedEntryByID(entries, mutation.ID); ok {
\t\t\t\tapplied = append(applied, current)
\t\t\t}
''',
)

# Profile cache/budget: expire cached snapshots on time boundaries and measure
# the serialized representation that is actually injected into prompts.
replace_once(
    "pkg/memory/user_profile.go",
    '''import (
\t"hash/fnv"
''',
    '''import (
\t"encoding/json"
\t"hash/fnv"
''',
)
replace_once(
    "pkg/memory/user_profile.go",
    '''type cachedUserProfile struct {
\tRevision uint64
\tMaxChars int
\tMinScore float64
\tSnapshot UserProfileSnapshot
}
''',
    '''type cachedUserProfile struct {
\tRevision   uint64
\tMaxChars   int
\tMinScore   float64
\tValidUntil time.Time
\tSnapshot   UserProfileSnapshot
}
''',
)
replace_once(
    "pkg/memory/user_profile.go",
    '''\tprofileRevision := userProfileRevision(doc.Entries)

\tif cached, ok := s.profileCache.Load(path); ok {
\t\titem, valid := cached.(cachedUserProfile)
\t\tif valid && item.Revision == profileRevision && item.MaxChars == opts.MaxChars &&
\t\t\titem.MinScore == opts.MinConfidence {
\t\t\treturn cloneUserProfileSnapshot(item.Snapshot), nil
\t\t}
\t}
''',
    '''\tprofileRevision := userProfileRevision(doc.Entries)
\tvar validUntil time.Time
\tfor _, raw := range doc.Entries {
\t\tentry := normalizedCuratedEntry(raw)
\t\tif entry.EffectiveStatus() != CuratedStatusActive || profileCategory(entry) == "" || entry.ExpiresAt == nil {
\t\t\tcontinue
\t\t}
\t\texpires := entry.ExpiresAt.UTC()
\t\tif expires.After(now) && (validUntil.IsZero() || expires.Before(validUntil)) {
\t\t\tvalidUntil = expires
\t\t}
\t}

\tif cached, ok := s.profileCache.Load(path); ok {
\t\titem, valid := cached.(cachedUserProfile)
\t\tcacheTimeValid := item.ValidUntil.IsZero() || now.Before(item.ValidUntil)
\t\tif valid && cacheTimeValid && item.Revision == profileRevision && item.MaxChars == opts.MaxChars &&
\t\t\titem.MinScore == opts.MinConfidence {
\t\t\treturn cloneUserProfileSnapshot(item.Snapshot), nil
\t\t}
\t}
''',
)
replace_once(
    "pkg/memory/user_profile.go",
    '''\t\tcost := profileFieldCharacters(field)
\t\tremaining := opts.MaxChars - snapshot.Characters
\t\tif cost > remaining && field.Content != "" && remaining > 32 {
\t\t\tfield.Content = truncateCuratedRunes(field.Content, remaining-24)
\t\t\tcost = profileFieldCharacters(field)
\t\t}
\t\tif cost <= 0 || cost > remaining {
\t\t\tcontinue
\t\t}
\t\tappendUserProfileField(&snapshot, candidate.category, field)
\t\tsnapshot.Characters += cost
\t\tsnapshot.SourceIDs = append(snapshot.SourceIDs, entry.ID)
\t\tif snapshot.UpdatedAt.IsZero() || entry.UpdatedAt.After(snapshot.UpdatedAt) {
\t\t\tsnapshot.UpdatedAt = entry.UpdatedAt
\t\t}
''',
    '''\t\tcandidateSnapshot := cloneUserProfileSnapshot(snapshot)
\t\tappendUserProfileField(&candidateSnapshot, candidate.category, field)
\t\tcandidateSnapshot.SourceIDs = append(candidateSnapshot.SourceIDs, entry.ID)
\t\tif candidateSnapshot.UpdatedAt.IsZero() || entry.UpdatedAt.After(candidateSnapshot.UpdatedAt) {
\t\t\tcandidateSnapshot.UpdatedAt = entry.UpdatedAt
\t\t}
\t\tfinalizeUserProfileCharacters(&candidateSnapshot)
\t\tif candidateSnapshot.Characters <= 0 || candidateSnapshot.Characters > opts.MaxChars {
\t\t\tcontinue
\t\t}
\t\tsnapshot = candidateSnapshot
''',
)
replace_once(
    "pkg/memory/user_profile.go",
    '''\ts.profileCache.Store(path, cachedUserProfile{
\t\tRevision: profileRevision,
\t\tMaxChars: opts.MaxChars,
\t\tMinScore: opts.MinConfidence,
\t\tSnapshot: cloneUserProfileSnapshot(snapshot),
\t})
''',
    '''\ts.profileCache.Store(path, cachedUserProfile{
\t\tRevision:   profileRevision,
\t\tMaxChars:   opts.MaxChars,
\t\tMinScore:   opts.MinConfidence,
\t\tValidUntil: validUntil,
\t\tSnapshot:   cloneUserProfileSnapshot(snapshot),
\t})
''',
)
replace_once(
    "pkg/memory/user_profile.go",
    '''func profileFieldCharacters(field UserProfileField) int {
\treturn utf8.RuneCountInString(field.Key) + utf8.RuneCountInString(field.Value) +
\t\tutf8.RuneCountInString(field.Content) + 16
}
''',
    '''func userProfileJSONCharacters(snapshot UserProfileSnapshot) int {
\tdata, err := json.Marshal(snapshot)
\tif err != nil {
\t\treturn 0
\t}
\treturn utf8.RuneCount(data)
}

func finalizeUserProfileCharacters(snapshot *UserProfileSnapshot) {
\tif snapshot == nil {
\t\treturn
\t}
\tfor i := 0; i < 4; i++ {
\t\tnext := userProfileJSONCharacters(*snapshot)
\t\tif next == snapshot.Characters {
\t\t\treturn
\t\t}
\t\tsnapshot.Characters = next
\t}
\tsnapshot.Characters = userProfileJSONCharacters(*snapshot)
}
''',
)

# Prompt precedence: retrieved durable memory must outrank legacy USER.md seed,
# while the seed remains above weak session summary context.
replace_once(
    "pkg/agent/prompt.go",
    '''\tcase PromptSlotLegacyUser:
\t\treturn 720
\tcase PromptSlotMemory:
\t\treturn 700
''',
    '''\tcase PromptSlotLegacyUser:
\t\treturn 685
\tcase PromptSlotMemory:
\t\treturn 700
''',
)

# /clear is destructive to recall/review state. If the pre-clear reviewer cannot
# finish within its bounded timeout, fail closed rather than silently losing
# unreviewed durable information.
replace_once(
    "pkg/agent/agent_command.go",
    '''\t\t\tflushCtx, flushCancel := context.WithTimeout(ctx, flushTimeout)
\t\t\tif err := al.flushMemoryReview(flushCtx, agent, caller); err != nil && flushCtx.Err() == nil {
\t\t\t\tlogger.WarnCF("memory", "Pre-clear memory flush failed", safeMemoryLogFields(err))
\t\t\t}
\t\t\tflushCancel()
\t\t\tif err := al.contextManager.Clear(ctx, opts.SessionKey); err != nil {
''',
    '''\t\t\tflushCtx, flushCancel := context.WithTimeout(ctx, flushTimeout)
\t\t\tif err := al.flushMemoryReview(flushCtx, agent, caller); err != nil {
\t\t\t\tlogger.WarnCF("memory", "Pre-clear memory flush failed", safeMemoryLogFields(err))
\t\t\t\tflushCancel()
\t\t\t\treturn fmt.Errorf("memory flush before clear failed; history was not cleared")
\t\t\t}
\t\t\tflushCancel()
\t\t\tif err := al.contextManager.Clear(ctx, opts.SessionKey); err != nil {
''',
)

# Documentation reflects the hardened invariants.
replace_once(
    "docs/guides/curated-memory.md",
    '''The snapshot is a cache, **not another source of truth**. It is rebuilt automatically when the underlying structured memory revision changes. Low-confidence inference is excluded by default, while explicit user preferences remain eligible. The default profile budget is 1,200 characters. Private profiles are never loaded into shared/group prompts.
''',
    '''The snapshot is a cache, **not another source of truth**. It is rebuilt automatically when the underlying structured memory revision changes and cache validity also stops at the earliest relevant memory expiry, so expired preferences cannot survive through a stale profile cache. Low-confidence inference is excluded by default, while explicit user preferences remain eligible. The default profile budget is 1,200 serialized characters. Private profiles are never loaded into shared/group prompts.
''',
)
replace_once(
    "docs/guides/curated-memory.md",
    '''Evidence authority is intentionally different: direct user statements are `explicit`, repeated behavioral evidence may be `observed`, and cautious model conclusions are `inferred`. An inferred entry is not automatically confirmed simply because the curator created it.
''',
    '''Evidence authority is intentionally different: direct user statements are `explicit`, repeated behavioral evidence may be `observed`, and cautious model conclusions are `inferred`. `observed` requires at least two observations; a single observation is conservatively downgraded to inference. Inferred confidence is capped below profile eligibility, and neither observed nor inferred entries retain direct-user confirmation timestamps. An inferred entry is not automatically confirmed simply because the curator created it.
''',
)
replace_once(
    "docs/guides/curated-memory.md",
    '''Before `/clear` or `/reset` discards session recall, PicoClaw performs a bounded synchronous flush of still-unreviewed delivered turns when background memory review is enabled. Then `/clear` and its `/reset` alias clear the current session history/summary,
current-session recall records, and its reviewer cursor. They discard
''',
    '''Before `/clear` or `/reset` discards session recall, PicoClaw performs a bounded synchronous flush of still-unreviewed delivered turns when background memory review is enabled. The operation is fail-closed: if that bounded flush fails or times out, history is left intact and the user can retry instead of silently losing unreviewed durable information. After a successful flush, `/clear` and its `/reset` alias clear the current session history/summary,
current-session recall records, and its reviewer cursor. They discard
''',
)

# Tests: evidence semantics and confirmation integrity.
replace_once(
    "pkg/memory/curated_evidence_test.go",
    'import "testing"\n',
    'import (\n\t"testing"\n\t"time"\n)\n',
)
append_once(
    "pkg/memory/curated_evidence_test.go",
    "TestObservedEvidenceRequiresRepeatedObservations",
    r'''func TestObservedEvidenceRequiresRepeatedObservations(t *testing.T) {
	store, err := NewCuratedStore(t.TempDir(), CuratedStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	caller := CallerScope{AgentID: "main", UserKey: "telegram:u-observed", Channel: "telegram", ChatID: "u-observed"}
	high := 0.99
	weak, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Might prefer examples", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceObserved, EvidenceCount: 1, ObservationCount: 1, Confidence: &high,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	entry := weak.Applied[0]
	if entry.EffectiveEvidenceKind() != CuratedEvidenceInferred {
		t.Fatalf("single observation evidence = %q, want inferred", entry.EffectiveEvidenceKind())
	}
	if entry.EffectiveConfidence() > maxConfidenceForEvidence(CuratedEvidenceInferred) {
		t.Fatalf("single observation confidence = %.2f, exceeds inferred cap", entry.EffectiveConfidence())
	}
	if entry.LastConfirmedAt != nil || entry.LastVerifiedAt != nil {
		t.Fatalf("single observation retained confirmation: %#v", entry)
	}

	repeated, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Repeatedly asks for copy-paste commands", Type: CuratedTypeWorkflowPreference,
		EvidenceKind: CuratedEvidenceObserved, EvidenceCount: 2, ObservationCount: 2,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := repeated.Applied[0].EffectiveEvidenceKind(); got != CuratedEvidenceObserved {
		t.Fatalf("repeated observation evidence = %q, want observed", got)
	}
}

func TestReplaceWithoutExplicitEvidenceDoesNotRefreshConfirmation(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	store, err := NewCuratedStore(t.TempDir(), CuratedStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	caller := CallerScope{AgentID: "main", UserKey: "telegram:u-confirm", Channel: "telegram", ChatID: "u-confirm"}
	added, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers detailed explanations", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "detailed",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	original := added.Applied[0].LastConfirmedAt
	if original == nil {
		t.Fatal("explicit preference missing confirmation")
	}
	now = now.Add(2 * time.Hour)
	replaced, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionReplace, ID: added.Applied[0].ID, Content: "Prefers detailed technical explanations",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := replaced.Applied[0].LastConfirmedAt; got == nil || !got.Equal(*original) {
		t.Fatalf("implicit curator rewrite refreshed confirmation: old=%v new=%v", original, got)
	}
}

func TestReplaceToInferenceClearsConfirmationAndCapsConfidence(t *testing.T) {
	store, err := NewCuratedStore(t.TempDir(), CuratedStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	caller := CallerScope{AgentID: "main", UserKey: "telegram:u-demote", Channel: "telegram", ChatID: "u-demote"}
	added, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers Rust", Type: CuratedTypeWorkflowPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "workflow.programming_language", PreferenceValue: "rust",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	high := 0.99
	replaced, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionReplace, ID: added.Applied[0].ID, Content: "May prefer Rust",
		EvidenceKind: CuratedEvidenceInferred, Confidence: &high,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	entry := replaced.Applied[0]
	if entry.LastConfirmedAt != nil || entry.LastVerifiedAt != nil {
		t.Fatalf("inference retained explicit confirmation: %#v", entry)
	}
	if entry.EffectiveConfidence() > maxConfidenceForEvidence(CuratedEvidenceInferred) {
		t.Fatalf("inference confidence %.2f exceeds cap", entry.EffectiveConfidence())
	}
}''',
)

# Tests: preference reconciliation, supersedes safety, cache expiry and real budget.
replace_once(
    "pkg/memory/user_profile_test.go",
    '''import (
\t"path/filepath"
\t"testing"
)
''',
    '''import (
\t"encoding/json"
\t"errors"
\t"path/filepath"
\t"testing"
\t"time"
\t"unicode/utf8"
)
''',
)
replace_once(
    "pkg/memory/user_profile_test.go",
    'profile, err := store.CompileUserProfile(userA, UserProfileOptions{MaxChars: 500, MinConfidence: 0.65})',
    'profile, err := store.CompileUserProfile(userA, UserProfileOptions{MaxChars: 900, MinConfidence: 0.65})',
)
replace_once(
    "pkg/memory/user_profile_test.go",
    'if len(profile.SourceIDs) != 2 || profile.Characters > 500 {',
    'if len(profile.SourceIDs) != 2 || profile.Characters > 900 {',
)
append_once(
    "pkg/memory/user_profile_test.go",
    "TestRestoredPreferenceReconcilesSameKey",
    r'''func TestRestoredPreferenceReconcilesSameKey(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	store, err := NewCuratedStore(filepath.Join(t.TempDir(), "curated"), CuratedStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	caller := testCaller("telegram:user-restore")
	old, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers concise responses", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "concise",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionArchive, ID: old.Applied[0].ID,
	}}, false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	current, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers detailed responses", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "detailed",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionRestore, ID: old.Applied[0].ID,
	}}, false); err != nil {
		t.Fatal(err)
	}
	oldEntry, _ := store.Inspect(CuratedTargetCurrentUser, caller, old.Applied[0].ID)
	currentEntry, _ := store.Inspect(CuratedTargetCurrentUser, caller, current.Applied[0].ID)
	if oldEntry.EffectiveStatus() == CuratedStatusActive && currentEntry.EffectiveStatus() == CuratedStatusActive {
		t.Fatalf("restore produced two active values: old=%#v current=%#v", oldEntry, currentEntry)
	}
	if currentEntry.EffectiveStatus() != CuratedStatusActive || currentEntry.PreferenceValue != "detailed" {
		t.Fatalf("newer confirmed preference lost after restore: %#v", currentEntry)
	}
}

func TestStructuredSupersedesRejectsDifferentPreferenceKey(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 10_000, 10_000)
	caller := testCaller("telegram:user-key-safety")
	language, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers Indonesian", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.language", PreferenceValue: "id",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Prefers detailed answers", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "detailed",
		Supersedes: language.Applied[0].ID,
	}}, false)
	if !errors.Is(err, ErrCuratedInvalidPreferenceKey) {
		t.Fatalf("cross-key supersedes error = %v, want ErrCuratedInvalidPreferenceKey", err)
	}
}

func TestCompileUserProfileCacheExpiresWithMemory(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	store, err := NewCuratedStore(filepath.Join(t.TempDir(), "curated"), CuratedStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	caller := testCaller("telegram:user-expiry")
	expires := now.Add(time.Hour)
	if _, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
		Action: CuratedActionAdd, Content: "Temporarily prefers concise answers", Type: CuratedTypeCommunicationPreference,
		EvidenceKind: CuratedEvidenceExplicit, PreferenceKey: "communication.verbosity", PreferenceValue: "concise",
		ExpiresAt: &expires,
	}}, false); err != nil {
		t.Fatal(err)
	}
	profile, err := store.CompileUserProfile(caller, UserProfileOptions{MaxChars: 800, Now: now})
	if err != nil || len(profile.SourceIDs) != 1 {
		t.Fatalf("initial profile = %#v, %v", profile, err)
	}
	now = now.Add(2 * time.Hour)
	expired, err := store.CompileUserProfile(caller, UserProfileOptions{MaxChars: 800, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(expired.SourceIDs) != 0 {
		t.Fatalf("expired preference survived profile cache: %#v", expired)
	}
}

func TestCompileUserProfileSerializedBudgetIsHardBound(t *testing.T) {
	store := newTestCuratedStore(t, filepath.Join(t.TempDir(), "curated"), 20_000, 20_000)
	caller := testCaller("telegram:user-budget")
	for i := 0; i < 12; i++ {
		content := "Stable interaction preference number " + string(rune('A'+i)) + " with additional descriptive text"
		if _, err := store.ApplyBatch(CuratedTargetCurrentUser, caller, []CuratedMutation{{
			Action: CuratedActionAdd, Content: content, Type: CuratedTypeCommunicationPreference,
			EvidenceKind: CuratedEvidenceExplicit,
		}}, false); err != nil {
			t.Fatal(err)
		}
	}
	const maxChars = 600
	profile, err := store.CompileUserProfile(caller, UserProfileOptions{MaxChars: maxChars})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if got := utf8.RuneCount(data); got > maxChars {
		t.Fatalf("serialized profile chars = %d, exceeds %d: %s", got, maxChars, data)
	}
	if profile.Characters != utf8.RuneCount(data) {
		t.Fatalf("profile Characters = %d, actual serialized = %d", profile.Characters, utf8.RuneCount(data))
	}
}''',
)

# Prompt and SOUL coverage gaps from the original acceptance plan.
append_once(
    "pkg/agent/prompt_test.go",
    "TestRenderPromptPartsLegacy_MemoryPrecedesLegacyUserSeed",
    r'''func TestRenderPromptPartsLegacy_MemoryPrecedesLegacyUserSeed(t *testing.T) {
	parts := []PromptPart{
		{ID: "legacy.user", Layer: PromptLayerContext, Slot: PromptSlotLegacyUser, Source: PromptSource{ID: PromptSourceLegacyUser}, Content: "legacy-user"},
		{ID: "memory.current", Layer: PromptLayerContext, Slot: PromptSlotMemory, Source: PromptSource{ID: PromptSourceCuratedMemory}, Content: "current-memory"},
		{ID: "summary", Layer: PromptLayerContext, Slot: PromptSlotSummary, Source: PromptSource{ID: PromptSourceSummary}, Content: "summary"},
	}
	got := renderPromptPartsLegacy(parts)
	memoryAt := strings.Index(got, "current-memory")
	legacyAt := strings.Index(got, "legacy-user")
	summaryAt := strings.Index(got, "summary")
	if memoryAt < 0 || legacyAt < 0 || summaryAt < 0 || !(memoryAt < legacyAt && legacyAt < summaryAt) {
		t.Fatalf("prompt precedence = %q, want memory > legacy USER.md > summary", got)
	}
}''',
)
append_once(
    "pkg/agent/definition_test.go",
    "TestEmptySoulFallsBackToBuiltInIdentity",
    r'''func TestEmptySoulFallsBackToBuiltInIdentity(t *testing.T) {
	tmpDir := setupWorkspace(t, map[string]string{
		"AGENT.md": "# Agent\nFollow workspace instructions.",
		"SOUL.md":  " \n\t\n",
	})
	defer cleanupWorkspace(t, tmpDir)
	cb := NewContextBuilder(tmpDir)
	prompt := cb.BuildSystemPromptWithCache()
	if !strings.Contains(prompt, "You are PicoClaw, a helpful, practical, lightweight personal AI assistant.") {
		t.Fatalf("empty SOUL.md did not use built-in fallback identity: %q", prompt)
	}
}''',
)

print("personalization audit fixes applied")
