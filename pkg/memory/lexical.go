package memory

import (
	"strings"
	"unicode"
)

func lexicalTokens(text string) map[string]struct{} {
	counts := lexicalTokenCounts(text)
	out := make(map[string]struct{}, len(counts))
	for token := range counts {
		out[token] = struct{}{}
	}
	return out
}

func lexicalTokenCounts(text string) map[string]int {
	counts := make(map[string]int)
	var token strings.Builder
	flush := func() {
		value := strings.ToLower(token.String())
		token.Reset()
		if len([]rune(value)) < 2 || lexicalStopwords[value] {
			return
		}
		counts[value]++
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			token.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return counts
}

var lexicalStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "for": true, "from": true, "in": true, "is": true,
	"it": true, "of": true, "on": true, "or": true, "that": true, "the": true,
	"this": true, "to": true, "was": true, "we": true, "were": true, "with": true,
	"yang": true, "dan": true, "di": true, "ke": true, "dari": true, "ini": true,
	"itu": true, "kita": true, "saya": true, "aku": true, "anda": true, "untuk": true,
	"dengan": true, "tadi": true, "sebelumnya": true, "tentang": true,
}
