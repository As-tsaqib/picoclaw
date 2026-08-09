package telegram

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasTelegramRichTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "markdown table",
			content: `| Name | Value |
|------|------:|
| CPU  | 42%   |`,
			want: true,
		},
		{
			name: "markdown table without outer pipes",
			content: `Name | Value
--- | ---
CPU | 42%`,
			want: true,
		},
		{
			name:    "html table",
			content: `Before <TABLE striped><TR><TH>Name</TH></TR><TR><TD>CPU</TD></TR></TABLE> after`,
			want:    true,
		},
		{
			name: "table with inline code pipe",
			content: "| Expression | Result |\n" +
				"|------------|--------|\n" +
				"| `a|b`      | true   |",
			want: true,
		},
		{
			name:    "ordinary text with pipes",
			content: `Use a | b when choosing one value.`,
			want:    false,
		},
		{
			name: "missing delimiter",
			content: `| Name | Value |
| CPU  | 42%   |`,
			want: false,
		},
		{
			name:    "markdown table in fenced code",
			content: "```markdown\n| Name | Value |\n|---|---|\n| CPU | 42% |\n```",
			want:    false,
		},
		{
			name:    "html table in inline code",
			content: "Use `<table><tr><td>x</td></tr></table>` as an example.",
			want:    false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, hasTelegramRichTable(tt.content))
		})
	}
}

func TestTelegramTableFallbackMarkdown(t *testing.T) {
	t.Parallel()

	input := `Summary:

| Name | Value |
|------|------:|
| CPU  | 42%   |

- healthy`

	fallback := telegramTableFallbackMarkdown(input)
	assert.Contains(t, fallback, "```\n| Name | Value |")
	assert.Contains(t, fallback, "| CPU")
	assert.NotContains(t, fallback, "|------|------:|")
	assert.Contains(t, fallback, "\n```\n\n- healthy")

	htmlFormatted := markdownToTelegramHTML(fallback)
	assert.Contains(t, htmlFormatted, "<pre><code>")
	assert.Contains(t, htmlFormatted, "| Name | Value |")
	assert.Contains(t, htmlFormatted, "</code></pre>")
}

func TestTelegramHTMLTableFallback(t *testing.T) {
	t.Parallel()

	input := `<p>Metrics</p>
<table bordered>
  <tr><th>Name</th><th>Value</th></tr>
  <tr><td>Latency</td><td><b>12 &amp; 13 ms</b></td></tr>
</table>`

	fallback := telegramTableFallbackMarkdown(input)
	assert.Contains(t, fallback, "```")
	assert.Contains(t, fallback, "| Name")
	assert.Contains(t, fallback, "| Latency")
	assert.Contains(t, fallback, "12 & 13 ms")
	assert.NotContains(t, fallback, "<table")
}

func TestTelegramTableFallbackPreservesExistingCodeBlocks(t *testing.T) {
	t.Parallel()

	codeBlock := "```markdown\n| Example | Only |\n|---|---|\n| a | b |\n```"
	input := codeBlock + "\n\n| Real | Table |\n|---|---|\n| c | d |"

	fallback := telegramTableFallbackMarkdown(input)
	assert.Contains(t, fallback, codeBlock)
	assert.Equal(t, 4, strings.Count(fallback, "```"))
}

func TestTelegramTableFallbackPlainTextHasNoAddedFence(t *testing.T) {
	t.Parallel()

	input := "| Name | Value |\n|---|---|\n| CPU | 42% |"
	fallback := telegramTableFallbackPlainText(input)

	assert.NotContains(t, fallback, "```")
	assert.Contains(t, fallback, "| Name")
	assert.Contains(t, fallback, "| CPU")
	assert.NotContains(t, fallback, "|---|---|")
}
