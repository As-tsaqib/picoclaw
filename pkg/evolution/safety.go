package evolution

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/As-tsaqib/picoclaw/pkg/skills"
)

var evolutionSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:sk-live-|sk_test_)[a-z0-9_-]+`),
	regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`),
	regexp.MustCompile(
		`(?i)\b(?:api[_ -]?key|access[_ -]?token|refresh[_ -]?token|` +
			`client[_ -]?secret|password|passwd|cookie|authorization)\s*[:=]\s*["']?[^\s"']{4,}`,
	),
	regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{12,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
}

var evolutionPIIPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}\b`),
	regexp.MustCompile(`(?i)(?:\+?\d[\d .()-]{7,}\d)`),
	regexp.MustCompile(
		`(?i)\b(?:my name is|nama saya|call me|panggil saya|my timezone is|zona waktu saya|` +
			`i prefer|saya lebih suka|my role is|jabatan saya)\b[^\n.!?]{0,160}`,
	),
	regexp.MustCompile(
		`(?i)\b(?:user|pengguna)\s+[a-z0-9][a-z0-9._-]{0,39}\s+` +
			`(?:prefers?|likes?|wants?|lebih suka|menyukai|ingin)\b[^\n.!?]{0,160}`,
	),
	regexp.MustCompile(
		`\b[A-Z][A-Za-z'’-]{1,39}(?:\s+[A-Z][A-Za-z'’-]{1,39}){0,2}\s+` +
			`(?:prefers?|likes?|wants?|lebih suka|menyukai|ingin)\b[^\n.!?]{0,160}`,
	),
	regexp.MustCompile(
		`(?i)\b(?:the user|this user|pengguna ini|user tersebut|he|she|dia|beliau)\s+` +
			`(?:prefers?|likes?|wants?|lebih suka|menyukai|ingin)\b[^\n.!?]{0,160}`,
	),
	regexp.MustCompile(
		`(?i)\b(?:user|pengguna)\s+[a-z0-9][a-z0-9._-]{0,39}\s+` +
			`(?:lives|works|is located|tinggal|berdomisili|berada|bekerja)\b[^\n.!?]{0,160}`,
	),
	regexp.MustCompile(
		`\b[A-Z][A-Za-z'’-]{1,39}(?:\s+[A-Z][A-Za-z'’-]{1,39}){0,2}\s+` +
			`(?:lives|works as|is located|tinggal|berdomisili|berada|bekerja sebagai)\b[^\n.!?]{0,160}`,
	),
	regexp.MustCompile(
		`(?i)\b(?:telegram|discord|slack|whatsapp|account|user)[ _-]?` +
			`(?:id|handle)\s*[:=]\s*[@a-z0-9._-]+`,
	),
	regexp.MustCompile(`(?i)@[a-z0-9_]{3,32}\b`),
}

var evolutionInjectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bignore\s+(?:all\s+)?(?:previous|prior|above)\s+(?:instructions?|prompts?|rules?)\b`),
	regexp.MustCompile(`(?i)\b(?:system|developer)\s+(?:prompt|message|instruction)\s*:`),
	regexp.MustCompile(`(?i)<\/?(?:system|developer|assistant|tool)(?:\s|>)`),
	regexp.MustCompile(`(?i)\[(?:INST|SYS)\]`),
	regexp.MustCompile(`(?i)\babaikan\s+(?:semua\s+)?(?:instruksi|perintah|aturan)\s+(?:sebelumnya|di atas)\b`),
}

var evolutionForbiddenPathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[/\\])(?:AGENT|SOUL|USER)\.md\b`),
	regexp.MustCompile(`(?i)(?:^|[/\\])memory[/\\]users(?:[/\\]|$)`),
	regexp.MustCompile(`(?i)(?:^|[/\\])structured-memory(?:[/\\]|$)`),
	regexp.MustCompile(`(?i)\b(?:current_user|per-user|private user) memory\b`),
}

var evolutionForbiddenTargetPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[-_])(?:soul|personality)(?:$|[-_])`),
	regexp.MustCompile(
		`(?i)(?:^|[-_])(?:private|personal|per[-_]?user|current[-_]?user)[-_]?` +
			`(?:memory|profile|preferences?|identity)(?:$|[-_])`,
	),
	regexp.MustCompile(
		`(?i)(?:^|[-_])(?:user|agent)[-_]` +
			`(?:memory|profile|preferences?|identity|personality)(?:$|[-_])`,
	),
}

var opaqueEvolutionSessionPattern = regexp.MustCompile(`^session-[0-9a-f]{16}$`)

// ScrubEvolutionText removes secrets and personal identifiers from the
// procedural learning path. It returns category-only findings so logs and
// review UIs never need the rejected values.
func ScrubEvolutionText(value string) (string, []string) {
	if !utf8.ValidString(value) {
		return "", []string{"invalid UTF-8 removed"}
	}
	clean := value
	findings := make([]string, 0, 4)
	for _, pattern := range evolutionSecretPatterns {
		if pattern.MatchString(clean) {
			clean = pattern.ReplaceAllString(clean, "[REDACTED SECRET]")
			findings = appendUniqueStrings(findings, "secret-like token or credential redacted")
		}
	}
	for _, pattern := range evolutionPIIPatterns {
		if pattern.MatchString(clean) {
			clean = pattern.ReplaceAllString(clean, "[REDACTED PERSONAL DATA]")
			findings = appendUniqueStrings(findings, "personal identifier redacted")
		}
	}
	for _, pattern := range evolutionInjectionPatterns {
		if pattern.MatchString(clean) {
			clean = pattern.ReplaceAllString(clean, "[REDACTED UNTRUSTED INSTRUCTION]")
			findings = appendUniqueStrings(findings, "prompt-injection-shaped text redacted")
		}
	}
	for _, pattern := range evolutionForbiddenPathPatterns {
		if pattern.MatchString(clean) {
			clean = pattern.ReplaceAllString(clean, "[REDACTED FORBIDDEN PATH]")
			findings = appendUniqueStrings(findings, "personality or private-memory path redacted")
		}
	}
	for _, pattern := range evolutionForbiddenTargetPatterns {
		if pattern.MatchString(clean) {
			clean = pattern.ReplaceAllString(clean, "[REDACTED FORBIDDEN TARGET]")
			findings = appendUniqueStrings(findings, "personality or private-memory target redacted")
		}
	}
	var b strings.Builder
	removedControl := false
	for _, r := range clean {
		if r == '\n' || r == '\t' || (!unicode.IsControl(r) && !isEvolutionBidiControl(r)) {
			b.WriteRune(r)
		} else {
			removedControl = true
		}
	}
	if removedControl {
		findings = appendUniqueStrings(findings, "control characters removed")
	}
	return strings.TrimSpace(b.String()), findings
}

func scrubTurnCaseInput(input TurnCaseInput) (TurnCaseInput, []string) {
	var findings []string
	input.UserMessage, findings = appendScrubFindings(input.UserMessage, findings)
	input.FinalContent, findings = appendScrubFindings(input.FinalContent, findings)
	input.ActiveSkillNames, findings = scrubEvolutionSkillNames(input.ActiveSkillNames, findings)
	input.AttemptedSkillNames, findings = scrubEvolutionSkillNames(input.AttemptedSkillNames, findings)
	input.FinalSuccessfulPath, findings = scrubEvolutionSkillNames(input.FinalSuccessfulPath, findings)
	for index := range input.SkillContextSnapshots {
		input.SkillContextSnapshots[index].Trigger, findings = appendScrubFindings(
			input.SkillContextSnapshots[index].Trigger,
			findings,
		)
		input.SkillContextSnapshots[index].SkillNames, findings = scrubEvolutionSkillNames(
			input.SkillContextSnapshots[index].SkillNames,
			findings,
		)
	}
	for i := range input.ToolExecutions {
		input.ToolExecutions[i].ErrorSummary, findings = appendScrubFindings(
			input.ToolExecutions[i].ErrorSummary,
			findings,
		)
		input.ToolExecutions[i].ErrorSummary = truncateControlText(
			input.ToolExecutions[i].ErrorSummary,
			300,
		)
		input.ToolExecutions[i].SkillNames, findings = scrubEvolutionSkillNames(
			input.ToolExecutions[i].SkillNames,
			findings,
		)
	}
	if len(input.ToolExecutions) > 100 {
		input.ToolExecutions = append([]ToolExecutionRecord(nil), input.ToolExecutions[:100]...)
		findings = append(findings, "excess tool evidence omitted")
	}
	return input, appendUniqueStrings(nil, findings...)
}

// sanitizeLearningRecordForPersistence is a final store-boundary defense for
// both hot-path task records and model-produced pattern records. Runtime
// capture already scrubs normal turns, but callers and clusterers must not be
// able to persist private text through a less common store API.
func sanitizeLearningRecordForPersistence(record LearningRecord) LearningRecord {
	findings := make([]string, 0, 8)
	record.Summary, findings = appendScrubFindings(record.Summary, findings)
	record.UserGoal, findings = appendScrubFindings(record.UserGoal, findings)
	record.FinalOutput, findings = appendScrubFindings(record.FinalOutput, findings)
	record.ClusterReason, findings = appendScrubFindings(record.ClusterReason, findings)
	record.FinalSnapshotTrigger, findings = appendScrubFindings(
		record.FinalSnapshotTrigger,
		findings,
	)

	record.Label, findings = appendScrubFindings(record.Label, findings)
	if record.Label != "" && validateEvolutionSkillTarget(record.Label) != nil {
		record.Label = ""
		findings = appendUniqueStrings(findings, "unsafe pattern label omitted")
	}
	record.InitialSkillNames, findings = scrubEvolutionSkillNames(record.InitialSkillNames, findings)
	record.AddedSkillNames, findings = scrubEvolutionSkillNames(record.AddedSkillNames, findings)
	record.UsedSkillNames, findings = scrubEvolutionSkillNames(record.UsedSkillNames, findings)
	record.AllLoadedSkillNames, findings = scrubEvolutionSkillNames(record.AllLoadedSkillNames, findings)
	record.ActiveSkillNames, findings = scrubEvolutionSkillNames(record.ActiveSkillNames, findings)
	record.WinningPath, findings = scrubEvolutionSkillNames(record.WinningPath, findings)
	record.LateAddedSkills, findings = scrubEvolutionSkillNames(record.LateAddedSkills, findings)
	record.MatchedSkillNames, findings = scrubEvolutionSkillNames(record.MatchedSkillNames, findings)
	record.ToolKinds, findings = scrubEvolutionTextSlice(record.ToolKinds, findings)
	record.ToolKinds = uniqueTrimmedNames(record.ToolKinds)

	if record.AttemptTrail != nil {
		trail := *record.AttemptTrail
		trail.AttemptedSkills, findings = scrubEvolutionSkillNames(trail.AttemptedSkills, findings)
		trail.FinalSuccessfulPath, findings = scrubEvolutionSkillNames(
			trail.FinalSuccessfulPath,
			findings,
		)
		trail.SkillContextSnapshots = append(
			[]SkillContextSnapshot(nil),
			trail.SkillContextSnapshots...,
		)
		for index := range trail.SkillContextSnapshots {
			trail.SkillContextSnapshots[index].Trigger, findings = appendScrubFindings(
				trail.SkillContextSnapshots[index].Trigger,
				findings,
			)
			trail.SkillContextSnapshots[index].SkillNames, findings = scrubEvolutionSkillNames(
				trail.SkillContextSnapshots[index].SkillNames,
				findings,
			)
		}
		record.AttemptTrail = &trail
	}

	record.ToolExecutions = append([]ToolExecutionRecord(nil), record.ToolExecutions...)
	for index := range record.ToolExecutions {
		record.ToolExecutions[index].Name, findings = appendScrubFindings(
			record.ToolExecutions[index].Name,
			findings,
		)
		record.ToolExecutions[index].ErrorSummary, findings = appendScrubFindings(
			record.ToolExecutions[index].ErrorSummary,
			findings,
		)
		record.ToolExecutions[index].ErrorSummary = truncateControlText(
			record.ToolExecutions[index].ErrorSummary,
			300,
		)
		record.ToolExecutions[index].SkillNames, findings = scrubEvolutionSkillNames(
			record.ToolExecutions[index].SkillNames,
			findings,
		)
	}
	if len(record.ToolExecutions) > 100 {
		record.ToolExecutions = record.ToolExecutions[:100]
		findings = appendUniqueStrings(findings, "excess tool evidence omitted")
	}

	record.Source, findings = scrubEvolutionMetadataMap(record.Source, findings)
	record.Signals, findings = scrubEvolutionTextSlice(record.Signals, findings)
	record.Signals = appendUniqueStrings(record.Signals, findings...)
	if sessionKey := strings.TrimSpace(record.SessionKey); sessionKey != "" &&
		!opaqueEvolutionSessionPattern.MatchString(sessionKey) {
		record.SessionKey = evolutionSessionReference(sessionKey)
		record.Signals = appendUniqueStrings(record.Signals, "raw session reference replaced")
	}
	return record
}

func scrubEvolutionMetadataMap(
	values map[string]any,
	findings []string,
) (map[string]any, []string) {
	if len(values) == 0 {
		return nil, findings
	}
	out := make(map[string]any, len(values))
	redactedIndex := 0
	for key, value := range values {
		cleanKey, keyFindings := ScrubEvolutionText(key)
		findings = append(findings, keyFindings...)
		if cleanKey == "" || len(keyFindings) > 0 {
			redactedIndex++
			cleanKey = fmt.Sprintf("redacted_field_%d", redactedIndex)
		}
		cleanValue, nextFindings := scrubEvolutionMetadataValue(value, findings)
		findings = nextFindings
		out[cleanKey] = cleanValue
	}
	return out, findings
}

func scrubEvolutionMetadataValue(value any, findings []string) (any, []string) {
	switch typed := value.(type) {
	case string:
		return appendScrubFindings(typed, findings)
	case []string:
		return scrubEvolutionTextSlice(typed, findings)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			clean, nextFindings := scrubEvolutionMetadataValue(item, findings)
			findings = nextFindings
			out = append(out, clean)
		}
		return out, findings
	case map[string]any:
		return scrubEvolutionMetadataMap(typed, findings)
	default:
		return value, findings
	}
}

func scrubEvolutionSkillNames(values []string, findings []string) ([]string, []string) {
	if len(values) == 0 {
		return nil, findings
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		clean, textFindings := ScrubEvolutionText(value)
		findings = append(findings, textFindings...)
		if clean == "" || validateEvolutionSkillTarget(clean) != nil {
			findings = appendUniqueStrings(findings, "unsafe skill reference omitted")
			continue
		}
		out = append(out, clean)
	}
	return uniqueTrimmedNames(out), findings
}

func appendScrubFindings(value string, existing []string) (string, []string) {
	clean, findings := ScrubEvolutionText(value)
	return clean, append(existing, findings...)
}

func ReviewDraftWithPolicy(draft SkillDraft, maxChars int) DraftReviewResult {
	findings := append([]string(nil), ValidateDraft(draft)...)
	body := strings.TrimSpace(draft.BodyOrPatch)
	if maxChars > 0 && utf8.RuneCountInString(body) > maxChars {
		findings = append(findings, "draft exceeds configured maximum size")
	}
	for _, value := range draftSafetyTextFields(draft) {
		if _, safetyFindings := ScrubEvolutionText(value); len(safetyFindings) > 0 {
			findings = append(findings, safetyFindings...)
		}
		for _, pattern := range evolutionInjectionPatterns {
			if pattern.MatchString(value) {
				findings = append(findings, "prompt-injection-shaped instruction detected")
				break
			}
		}
		for _, pattern := range evolutionForbiddenPathPatterns {
			if pattern.MatchString(value) {
				findings = append(findings, "forbidden personality or private-memory path detected")
				break
			}
		}
	}
	if isForbiddenEvolutionTarget(draft.TargetSkillName) {
		findings = append(findings, "forbidden personality or private-memory target detected")
	}
	findings = appendUniqueStrings(nil, findings...)
	result := DraftReviewResult{
		Status:      DraftStatusCandidate,
		Findings:    findings,
		ReviewNotes: []string{"structural, secret, PII, injection, tool-policy, and path review completed"},
	}
	if len(findings) > 0 {
		result.Status = DraftStatusQuarantined
	}
	return result
}

func sanitizeSkillDraftForPersistence(draft SkillDraft) (SkillDraft, []string) {
	findings := make([]string, 0, 8)
	originalTarget := strings.TrimSpace(draft.TargetSkillName)
	draft.HumanSummary, findings = appendScrubFindings(draft.HumanSummary, findings)
	draft.BodyOrPatch, findings = appendScrubFindings(draft.BodyOrPatch, findings)
	draft.IntendedUseCases, findings = scrubEvolutionTextSlice(draft.IntendedUseCases, findings)
	draft.PreferredEntryPath, findings = scrubEvolutionTextSlice(draft.PreferredEntryPath, findings)
	draft.AvoidPatterns, findings = scrubEvolutionTextSlice(draft.AvoidPatterns, findings)
	draft.MatchedSkillRefs, findings = scrubEvolutionSkillNames(draft.MatchedSkillRefs, findings)
	draft.ReviewNotes, findings = scrubEvolutionTextSlice(draft.ReviewNotes, findings)
	draft.ScanFindings, findings = scrubEvolutionTextSlice(draft.ScanFindings, findings)
	draft.DecisionSource, findings = appendScrubFindings(draft.DecisionSource, findings)

	target := originalTarget
	if target != "" {
		_, targetFindings := ScrubEvolutionText(target)
		if len(targetFindings) > 0 || validateEvolutionSkillTarget(target) != nil {
			draft.BodyOrPatch = strings.ReplaceAll(
				draft.BodyOrPatch,
				target,
				"[REDACTED FORBIDDEN TARGET]",
			)
			draft.TargetSkillName = "quarantined-skill"
			findings = append(findings, "unsafe or invalid target skill name removed")
		}
	}
	findings = appendUniqueStrings(nil, findings...)
	if len(findings) > 0 {
		draft.Status = DraftStatusQuarantined
		draft.ScanFindings = appendUniqueStrings(draft.ScanFindings, findings...)
	}
	return draft, findings
}

func scrubEvolutionTextSlice(values []string, findings []string) ([]string, []string) {
	if len(values) == 0 {
		return nil, findings
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		clean, nextFindings := ScrubEvolutionText(value)
		findings = append(findings, nextFindings...)
		if clean != "" {
			out = append(out, clean)
		}
	}
	return out, findings
}

func draftSafetyTextFields(draft SkillDraft) []string {
	capacity := 3 + len(draft.IntendedUseCases) + len(draft.PreferredEntryPath) +
		len(draft.AvoidPatterns) + len(draft.MatchedSkillRefs)
	values := make([]string, 0, capacity)
	values = append(values,
		draft.TargetSkillName,
		draft.HumanSummary,
		draft.BodyOrPatch,
	)
	values = append(values, draft.IntendedUseCases...)
	values = append(values, draft.PreferredEntryPath...)
	values = append(values, draft.AvoidPatterns...)
	values = append(values, draft.MatchedSkillRefs...)
	return values
}

func isForbiddenEvolutionTarget(target string) bool {
	target = strings.TrimSpace(target)
	for _, pattern := range evolutionForbiddenPathPatterns {
		if pattern.MatchString(target) {
			return true
		}
	}
	for _, pattern := range evolutionForbiddenTargetPatterns {
		if pattern.MatchString(target) {
			return true
		}
	}
	return false
}

func validateEvolutionSkillTarget(target string) error {
	target = strings.TrimSpace(target)
	if err := skills.ValidateSkillName(target); err != nil {
		return err
	}
	if isForbiddenEvolutionTarget(target) {
		return fmt.Errorf("personality and private-memory skill targets are forbidden")
	}
	return nil
}

// ValidSkillTarget reports whether a skill name is both syntactically valid
// and outside the administrator-controlled personality/private-memory paths.
func ValidSkillTarget(target string) bool {
	return validateEvolutionSkillTarget(target) == nil
}

func isEvolutionBidiControl(r rune) bool {
	switch {
	case r == '\u061c' || r == '\u200e' || r == '\u200f':
		return true
	case r >= '\u202a' && r <= '\u202e':
		return true
	case r >= '\u2060' && r <= '\u206f':
		return true
	default:
		return false
	}
}
