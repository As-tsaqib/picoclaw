package bus

import (
	"context"
	"strings"
)

// StructuredTable is a channel-neutral tabular response. Channels that
// support rich messages may render it natively; all other channels use the
// fallback text carried by StructuredContent.Fallback.
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
	Kind          string
	OwnerID       string
	Channel       string
	Account       string
	ChatID        string
	TopicID       string
	AgentID       string
	Scope         string
	DashboardMode string
	Inbound       InboundContext
	Page          int
	Pages         int
	Entries       []InteractionEntry
	Current       string
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
	Kind          string
	Action        string
	Value         string
	OwnerID       string
	Channel       string
	Account       string
	ChatID        string
	TopicID       string
	MessageID     string
	AgentID       string
	Scope         string
	DashboardMode string
	Inbound       InboundContext
	Page          int
	SessionKey    string
}

type InternalCallbackResponse struct {
	Content *StructuredContent
	Text    string
	Close   bool
}

// InternalCallbackHandler is implemented by the agent loop and installed on
// capable channels by the channel manager.
type InternalCallbackHandler func(ctx context.Context, req InternalCallbackRequest) (*InternalCallbackResponse, error)

func (s *StructuredContent) Clone() *StructuredContent {
	if s == nil {
		return nil
	}
	out := *s
	out.Paragraphs = append([]string(nil), s.Paragraphs...)
	out.Tables = make([]StructuredTable, len(s.Tables))
	for i, table := range s.Tables {
		out.Tables[i] = table
		out.Tables[i].Columns = append([]string(nil), table.Columns...)
		out.Tables[i].Rows = make([][]string, len(table.Rows))
		for j, row := range table.Rows {
			out.Tables[i].Rows[j] = append([]string(nil), row...)
		}
	}
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

// FallbackText returns the readable text representation, preferring the
// explicitly supplied fallback and otherwise producing a compact table.
func (s *StructuredContent) FallbackText() string {
	if s == nil {
		return ""
	}
	if strings.TrimSpace(s.Fallback) != "" {
		return s.Fallback
	}
	parts := append([]string(nil), s.Paragraphs...)
	for _, table := range s.Tables {
		if len(table.Columns) == 0 {
			continue
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
	}
	return strings.Join(parts, "\n")
}
