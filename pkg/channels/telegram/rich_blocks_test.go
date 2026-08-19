package telegram

import (
	"strings"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestBuildNativeRichMessageTypedBlocks(t *testing.T) {
	content := &bus.StructuredContent{
		Title:      "Legacy title",
		Paragraphs: []string{"Legacy paragraph"},
		Blocks: []bus.StructuredBlock{
			{Type: "paragraph", Text: "paragraph"},
			{Type: "heading", Text: "heading", Level: 2},
			{Type: "preformatted", Text: "printf", Language: "sh"},
			{Type: "footer", Text: "footer"},
			{Type: "divider"},
			{Type: "expression", Text: "a+b"},
			{Type: "anchor", Text: "section"},
			{Type: "link", Text: "link", URL: "https://example.com/path"},
			{Type: "list", Items: []string{"one", "two"}},
			{Type: "list", Items: []string{"first", "second"}, Ordered: true},
			{Type: "quotation", Blocks: []bus.StructuredBlock{{Type: "paragraph", Text: "quote"}}},
			{Type: "pull_quote", Text: "pull"},
			{Type: "disclosure", Text: "details", Blocks: []bus.StructuredBlock{{Type: "paragraph", Text: "inside"}}},
			{Type: "table", Table: &bus.StructuredTable{
				Caption: "typed table", Columns: []string{"A", "B"}, Rows: [][]string{{"1", "2"}}, Header: true,
				Border: true, Striped: true,
			}},
		},
		Tables: []bus.StructuredTable{{Columns: []string{"Legacy"}, Rows: [][]string{{"cell"}}, Header: true}},
	}
	if _, ok := buildNativeRichMessage(content); !ok {
		t.Fatal("all verified typed blocks should map to a native rich message")
	}
}

func TestBuildNativeRichMessageRejectsMalformedTypedBlocksForFallback(t *testing.T) {
	tests := []struct {
		name  string
		block bus.StructuredBlock
	}{
		{"unsafe link", bus.StructuredBlock{Type: "link", Text: "bad", URL: "javascript:alert(1)"}},
		{"heading too large", bus.StructuredBlock{Type: "heading", Text: "bad", Level: 7}},
		{"empty details", bus.StructuredBlock{Type: "details", Text: "summary"}},
		{"missing table", bus.StructuredBlock{Type: "table"}},
		{"unknown", bus.StructuredBlock{Type: "future_block", Text: "future"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content := &bus.StructuredContent{Blocks: []bus.StructuredBlock{tc.block}}
			if _, ok := buildNativeRichMessage(content); ok {
				t.Fatal("malformed/unsupported typed block unexpectedly mapped as native")
			}
			if fallback := strings.TrimSpace(content.FallbackText()); fallback == "" && tc.name != "missing table" {
				t.Fatal("malformed/unsupported typed block lost deterministic text fallback")
			}
		})
	}
}

func TestBuildNativeRichMessageRejectsDeepTypedNesting(t *testing.T) {
	block := bus.StructuredBlock{Type: "paragraph", Text: "leaf"}
	for range telegramRichBlockMaxDepth + 2 {
		block = bus.StructuredBlock{Type: "details", Text: "nested", Blocks: []bus.StructuredBlock{block}}
	}
	if _, ok := buildNativeRichMessage(&bus.StructuredContent{Blocks: []bus.StructuredBlock{block}}); ok {
		t.Fatal("over-deep rich block tree must fall back instead of recursing unboundedly")
	}
}
