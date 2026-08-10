package memory

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var curatedSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*["']?bearer\s+[a-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`(?i)\b(?:api[_ -]?key|access[_ -]?token|refresh[_ -]?token|client[_ -]?secret|password|passwd|cookie|authorization)\s*[:=]\s*["']?[^\s"']{4,}`),
	regexp.MustCompile(`(?i)\b[a-z][a-z0-9_]*(?:api[_]?key|token|secret|password|passwd)\s*[:=]\s*["']?[^\s"']{4,}`),
	regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`(?i)\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis)://[^\s:/]+:[^\s@/]+@`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{12,}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
}

var curatedInjectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bignore\s+(?:all\s+)?(?:previous|prior|above)\s+(?:instructions?|prompts?|rules?)\b`),
	regexp.MustCompile(`(?i)\b(?:system|developer)\s+(?:prompt|message|instruction)\s*:`),
	regexp.MustCompile(`(?i)<\/?(?:system|developer|assistant|tool)(?:\s|>)`),
	regexp.MustCompile(`(?i)\[(?:INST|SYS)\]`),
	regexp.MustCompile(`(?i)\byou\s+must\s+(?:now\s+)?(?:ignore|override|disobey)\b`),
	regexp.MustCompile(`(?i)\babaikan\s+(?:semua\s+)?(?:instruksi|perintah|aturan)\s+(?:sebelumnya|di atas)\b`),
	regexp.MustCompile(`(?i)<\/?(?:curated_memory|task_checkpoints|transcript_snapshot)(?:\s|>)`),
}

// ValidateCuratedContent rejects secrets, prompt-injection-shaped control
// text, invalid UTF-8, and hidden/control characters before anything reaches
// durable storage. It intentionally returns category-only errors so rejected
// sensitive values are never repeated into logs or notifications.
func ValidateCuratedContent(content string) error {
	content = strings.TrimSpace(content)
	if content == "" || !utf8.ValidString(content) {
		return ErrCuratedUnsafeContent
	}
	for _, r := range content {
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) || isBidiControl(r) || r == '\u200b' || r == '\ufeff' {
			return ErrCuratedUnsafeContent
		}
	}
	for _, pattern := range curatedSecretPatterns {
		if pattern.MatchString(content) {
			return ErrCuratedUnsafeContent
		}
	}
	for _, pattern := range curatedInjectionPatterns {
		if pattern.MatchString(content) {
			return ErrCuratedUnsafeContent
		}
	}
	return nil
}

// RedactMemoryText is a defense-in-depth notification helper. Stored entries
// have already passed validation, but previews are redacted again in case a
// future store version imports legacy data.
func RedactMemoryText(content string) string {
	for _, pattern := range curatedSecretPatterns {
		if pattern.MatchString(content) {
			return "[REDACTED SENSITIVE CONTENT]"
		}
	}
	var b strings.Builder
	for _, r := range content {
		if r == '\n' || r == '\t' ||
			(!unicode.IsControl(r) && !isBidiControl(r) && r != '\u200b' && r != '\ufeff') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isBidiControl covers the Unicode Bidirectional_Control property without
// relying on a Go-version-specific exported unicode table. These format
// characters can make durable text appear different from what is stored.
func isBidiControl(r rune) bool {
	switch {
	case r == '\u061c' || r == '\u200e' || r == '\u200f':
		return true
	case r >= '\u202a' && r <= '\u202e':
		return true
	case r >= '\u2060' && r <= '\u2064':
		return true
	case r >= '\u2066' && r <= '\u206f':
		return true
	default:
		return false
	}
}

func normalizeCuratedContent(content string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(content)), " "))
}
