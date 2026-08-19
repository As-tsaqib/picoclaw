package bus

import (
	"context"
	"fmt"
	"strings"
)

const structuredBlockMaxDepth = 24

// StructuredTable is a channel-neutral tabular response. Channels that
// support rich messages may render it natively; all other channels use the
// deterministic text representation from StructuredContent.FallbackText.
type StructuredTable struct {
	Caption string     `json:"caption,omitempty"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Border  bool       `json:"border,omitempty"`
	Striped bool       `json:"striped,omitempty"`
	Header  bool       `json:"header,omitempty"`
}

// StructuredButton is deliberately independent from any platform SDK. The
// Telegram adapter maps Style and CallbackData onto Bot API buttons while
// other channels may ignore the keyboard and use the fallback text.
type StructuredButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	Style        string `json:"style,omitempty"`
}

// StructuredContent carries optional native rendering data alongside a plain
// fallback. It is safe to add to OutboundMessage because existing channels
// continue to consume Content only.
type StructuredContent struct {
	Kind       string               `json:"kind,omitempty"`
	Title      string               `json:"title,omitempty"`
	Paragraphs []string             `json:"paragraphs,omitempty"`
	Blocks     []StructuredBlock    `json:"blocks,omitempty"`
	Tables     []StructuredTable    `json:"tables,omitempty"`
	Keyboard   [][]StructuredButton `json:"keyboard,omitempty"`
	Fallback   string               `json:"fallback,omitempty"`
	// Interaction is an opaque, process-local menu description. It is never
	// sent to a client; a capable channel turns it into short callback tokens.
	Interaction *InteractionMenu `json:"-"`
}

// InteractionMenu describes an owner-bound interactive menu without exposing
// session keys in callback_data. The channel stores this object server-side
// and only sends a short opaque menu token to the platform.
type InteractionMenu struct {
	Kind    string
	OwnerID string
	Channel string
	Account string
	ChatID  string
	TopicID string
	AgentID string
	Scope   string
	Inbound InboundContext
	Page    int
	Pages   int
	Entries []InteractionEntry
	Current string
}

type InteractionEntry struct {
	Label  string
	Action string
	Value  string
}

// InternalCallbackRequest is the validated platform envelope passed from a
// channel adapter to the agent. The adapter validates menu ownership and
// message location; the agent validates the session scope again before
// mutating durable state.
type InternalCallbackRequest struct {
	Kind       string
	Action     string
	Value      string
	OwnerID    string
	Channel    string
	Account    string
	ChatID     string
	TopicID    string
	MessageID  string
	AgentID    string
	Scope      string
	Inbound    InboundContext
	Page       int
	SessionKey string
}

type InternalCallbackResponse struct {
	Content *StructuredContent
	Text    string
	Close   bool
}

// InternalCallbackHandler is implemented by the agent loop and installed on
// capable channels by the channel manager.
type InternalCallbackHandler func(
	ctx context.Context,
	req InternalCallbackRequest,
) (*InternalCallbackResponse, error)

func (s *StructuredContent) Clone() *StructuredContent {
	if s == nil {
		return nil
	}
	out := *s
	out.Paragraphs = append([]string(nil), s.Paragraphs...)
	out.Blocks = cloneStructuredBlocks(s.Blocks, 0)
	out.Tables = cloneStructuredTables(s.Tables)
	if len(s.Keyboard) > 0 {
		out.Keyboard = make([][]StructuredButton, len(s.Keyboard))
		for i, row := range s.Keyboard {
			out.Keyboard[i] = append([]StructuredButton(nil), row...)
		}
	}
	if s.Interaction != nil {
		menu := *s.Interaction
		menu.Entries = append([]InteractionEntry(nil), s.Interaction.Entries...)
		menu.Inbound = normalizeInboundContext(s.Interaction.Inbound)
		out.Interaction = &menu
	}
	return &out
}

func cloneStructuredBlocks(blocks []StructuredBlock, depth int) []StructuredBlock {
	if len(blocks) == 0 || depth >= structuredBlockMaxDepth {
		return nil
	}
	out := make([]StructuredBlock, len(blocks))
	for i, block := range blocks {
		out[i] = block
		out[i].Items = append([]string(nil), block.Items...)
		out[i].Blocks = cloneStructuredBlocks(block.Blocks, depth+1)
		if block.Table != nil {
			tables := cloneStructuredTables([]StructuredTable{*block.Table})
			if len(tables) == 1 {
				out[i].Table = &tables[0]
			}
		}
	}
	return out
}

func cloneStructuredTables(tables []StructuredTable) []StructuredTable {
	out := make([]StructuredTable, len(tables))
	for i, table := range tables {
		out[i] = table
		out[i].Columns = append([]string(nil), table.Columns...)
		out[i].Rows = make([][]string, len(table.Rows))
		for j, row := range table.Rows {
			out[i].Rows[j] = append([]string(nil), row...)
		}
	}
	return out
}

// FallbackText returns a deterministic readable representation. An explicitly
// supplied fallback remains authoritative; otherwise legacy fields and typed
// Rich Message v2 blocks are rendered without channel-specific types.
func (s *StructuredContent) FallbackText() string {
	if s == nil {
		return ""
	}
	if strings.TrimSpace(s.Fallback) != "" {
		return s.Fallback
	}
	parts := make([]string, 0, 1+len(s.Paragraphs)+len(s.Blocks)+len(s.Tables))
	if title := strings.TrimSpace(s.Title); title != "" {
		parts = append(parts, title)
	}
	for _, paragraph := range s.Paragraphs {
		if paragraph = strings.TrimSpace(paragraph); paragraph != "" {
			parts = append(parts, paragraph)
		}
	}
	parts = append(parts, structuredBlocksFallback(s.Blocks, 0)...)
	for _, table := range s.Tables {
		if rendered := structuredTableFallback(table); rendered != "" {
			parts = append(parts, rendered)
		}
	}
	return strings.Join(parts, "\n")
}

func structuredBlocksFallback(blocks []StructuredBlock, depth int) []string {
	if depth >= structuredBlockMaxDepth {
		return []string{"[nested rich content omitted]"}
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		kind := strings.ToLower(strings.TrimSpace(block.Type))
		text := strings.TrimSpace(block.Text)
		switch kind {
		case "paragraph":
			if text != "" {
				parts = append(parts, text)
			}
		case "heading":
			if text != "" {
				level := block.Level
				if level < 1 {
					level = 2
				}
				if level > 6 {
					level = 6
				}
				parts = append(parts, strings.Repeat("#", level)+" "+text)
			}
		case "preformatted", "code":
			if text != "" {
				parts = append(parts, "```"+strings.TrimSpace(block.Language)+"\n"+text+"\n```")
			}
		case "footer":
			if text != "" {
				parts = append(parts, text)
			}
		case "divider":
			parts = append(parts, "---")
		case "math", "expression":
			if text != "" {
				parts = append(parts, "$$"+text+"$$")
			}
		case "anchor":
			if text != "" {
				parts = append(parts, "#"+text)
			}
		case "link":
			if text != "" && strings.TrimSpace(block.URL) != "" {
				parts = append(parts, "["+text+"]("+strings.TrimSpace(block.URL)+")")
			} else if text != "" {
				parts = append(parts, text)
			}
		case "list":
			for i, item := range block.Items {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				prefix := "- "
				if block.Ordered {
					prefix = fmt.Sprintf("%d. ", i+1)
				}
				parts = append(parts, prefix+item)
			}
		case "quote", "quotation", "block_quote":
			quoteParts := structuredBlocksFallback(block.Blocks, depth+1)
			if text != "" {
				quoteParts = append([]string{text}, quoteParts...)
			}
			for _, value := range quoteParts {
				if value = strings.TrimSpace(value); value != "" {
					parts = append(parts, "> "+strings.ReplaceAll(value, "\n", "\n> "))
				}
			}
		case "pull_quote":
			if text != "" {
				parts = append(parts, "> "+strings.ReplaceAll(text, "\n", "\n> "))
			}
		case "details", "disclosure":
			if text != "" {
				parts = append(parts, text)
			}
			parts = append(parts, structuredBlocksFallback(block.Blocks, depth+1)...)
		case "table":
			if block.Table != nil {
				if rendered := structuredTableFallback(*block.Table); rendered != "" {
					parts = append(parts, rendered)
				}
			}
		default:
			// Unknown block types fail closed to readable text instead of being
			// silently interpreted as a platform capability.
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return parts
}

func structuredTableFallback(table StructuredTable) string {
	if len(table.Columns) == 0 {
		return ""
	}
	parts := make([]string, 0, len(table.Rows)+3)
	if caption := strings.TrimSpace(table.Caption); caption != "" {
		parts = append(parts, caption)
	}
	parts = append(parts, "| "+strings.Join(table.Columns, " | ")+" |")
	parts = append(parts, "| "+strings.Trim(strings.Repeat("--- | ", len(table.Columns)), " |")+" |")
	for _, row := range table.Rows {
		cells := append([]string(nil), row...)
		for len(cells) < len(table.Columns) {
			cells = append(cells, "")
		}
		parts = append(parts, "| "+strings.Join(cells[:len(table.Columns)], " | ")+" |")
	}
	return strings.Join(parts, "\n")
}

// StructuredBlock is the channel-neutral Rich Message v2 block model. Type is
// normalized by adapters; Telegram SDK types never cross this boundary.
type StructuredBlock struct {
	Type     string            `json:"type"` // paragraph, heading, preformatted/code, footer, divider, math/expression, anchor/link, list, quote, pull_quote, details/disclosure, table
	Text     string            `json:"text,omitempty"`
	URL      string            `json:"url,omitempty"`      // link only; adapters validate safe schemes
	Level    int               `json:"level,omitempty"`    // heading
	Language string            `json:"language,omitempty"` // preformatted/code
	Items    []string          `json:"items,omitempty"`    // list
	Ordered  bool              `json:"ordered,omitempty"`  // list
	Blocks   []StructuredBlock `json:"blocks,omitempty"`   // details/quotation
	Table    *StructuredTable  `json:"table,omitempty"`    // table
}
