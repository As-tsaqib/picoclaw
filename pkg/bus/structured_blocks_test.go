package bus

import (
	"strings"
	"testing"
)

func TestStructuredContentCloneAndFallbackTypedBlocks(t *testing.T) {
	content := &StructuredContent{
		Title:      "Legacy title",
		Paragraphs: []string{"Legacy paragraph"},
		Blocks: []StructuredBlock{
			{Type: "paragraph", Text: "Typed paragraph"},
			{Type: "heading", Text: "Heading", Level: 2},
			{Type: "code", Text: "fmt.Println(1)", Language: "go"},
			{Type: "footer", Text: "Footer"},
			{Type: "divider"},
			{Type: "math", Text: "x^2"},
			{Type: "anchor", Text: "section-a"},
			{Type: "link", Text: "OpenAI", URL: "https://openai.com"},
			{Type: "list", Items: []string{"one", "two"}, Ordered: true},
			{Type: "quote", Text: "quoted"},
			{Type: "pull_quote", Text: "pull"},
			{Type: "details", Text: "More", Blocks: []StructuredBlock{{Type: "paragraph", Text: "detail"}}},
			{Type: "table", Table: &StructuredTable{Columns: []string{"A"}, Rows: [][]string{{"B"}}, Header: true}},
		},
		Tables: []StructuredTable{{Columns: []string{"Legacy"}, Rows: [][]string{{"Table"}}, Header: true}},
	}
	clone := content.Clone()
	if clone == nil || len(clone.Blocks) != len(content.Blocks) {
		t.Fatalf("clone blocks = %#v", clone)
	}
	clone.Blocks[8].Items[0] = "changed"
	clone.Blocks[11].Blocks[0].Text = "changed detail"
	clone.Blocks[12].Table.Rows[0][0] = "changed table"
	if content.Blocks[8].Items[0] != "one" || content.Blocks[11].Blocks[0].Text != "detail" ||
		content.Blocks[12].Table.Rows[0][0] != "B" {
		t.Fatal("Clone shared nested rich-block storage with the original")
	}

	fallback := content.FallbackText()
	for _, want := range []string{
		"Legacy title", "Legacy paragraph", "Typed paragraph", "## Heading", "```go", "fmt.Println(1)",
		"Footer", "---", "$$x^2$$", "#section-a", "[OpenAI](https://openai.com)", "1. one", "2. two",
		"> quoted", "> pull", "More", "detail", "| A |", "| B |", "| Legacy |", "| Table |",
	} {
		if !strings.Contains(fallback, want) {
			t.Fatalf("fallback missing %q:\n%s", want, fallback)
		}
	}
}

func TestStructuredContentFallbackBoundsMalformedNesting(t *testing.T) {
	block := StructuredBlock{Type: "paragraph", Text: "leaf"}
	for range structuredBlockMaxDepth + 3 {
		block = StructuredBlock{Type: "details", Text: "nested", Blocks: []StructuredBlock{block}}
	}
	fallback := (&StructuredContent{Blocks: []StructuredBlock{block}}).FallbackText()
	if !strings.Contains(fallback, "[nested rich content omitted]") {
		t.Fatalf("deep nesting was not bounded: %q", fallback)
	}
}
