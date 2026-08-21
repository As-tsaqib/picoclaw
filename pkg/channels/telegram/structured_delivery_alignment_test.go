package telegram

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestNativeTableCellAlignmentPolicy(t *testing.T) {
	header := nativeTableCell("HEADER", true)
	assert.True(t, header.IsHeader)
	assert.Equal(t, "center", header.Align)
	assert.Equal(t, "middle", header.Valign)

	body := nativeTableCell("value", false)
	assert.False(t, body.IsHeader)
	assert.Equal(t, "left", body.Align)
	assert.Equal(t, "middle", body.Valign)
}

func TestBuildNativeRichMessagePreservesContextualTablePresentation(t *testing.T) {
	cases := []struct {
		name string
		kind bus.CardHeaderKind
	}{
		{name: "detail", kind: bus.CardHeaderDetail},
		{name: "status", kind: bus.CardHeaderStatus},
		{name: "inventory", kind: bus.CardHeaderInventory},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			columns := bus.CardHeaderColumns(tc.kind, true)
			content := &bus.StructuredContent{Tables: []bus.StructuredTable{{
				Caption: "Category caption",
				Columns: columns,
				Rows: [][]string{{"row-key", "row-value"}},
				Border: true,
				Striped: true,
				Header: true,
			}}}

			rich, ok := buildNativeRichMessage(content)
			require.True(t, ok)
			raw, err := json.Marshal(rich)
			require.NoError(t, err)

			var payload struct {
				Blocks []json.RawMessage `json:"blocks"`
			}
			require.NoError(t, json.Unmarshal(raw, &payload))

			var table struct {
				Type       string          `json:"type"`
				IsBordered bool            `json:"is_bordered"`
				IsStriped  bool            `json:"is_striped"`
				Caption    json.RawMessage `json:"caption"`
				Cells      [][]struct {
					IsHeader bool            `json:"is_header"`
					Align    string          `json:"align"`
					Valign   string          `json:"valign"`
					Text     json.RawMessage `json:"text"`
				} `json:"cells"`
			}
			found := false
			for _, block := range payload.Blocks {
				var candidate struct {
					Type string `json:"type"`
				}
				require.NoError(t, json.Unmarshal(block, &candidate))
				if candidate.Type != telego.BlockTypeTable {
					continue
				}
				require.NoError(t, json.Unmarshal(block, &table))
				found = true
				break
			}
			require.True(t, found)
			assert.True(t, table.IsBordered)
			assert.True(t, table.IsStriped)
			assert.Contains(t, string(table.Caption), "Category caption")
			require.Len(t, table.Cells, 2)
			require.Len(t, table.Cells[0], len(columns))
			require.Len(t, table.Cells[1], len(columns))

			for i, column := range columns {
				assert.True(t, table.Cells[0][i].IsHeader)
				assert.Equal(t, "center", table.Cells[0][i].Align)
				assert.Equal(t, "middle", table.Cells[0][i].Valign)
				assert.True(t, strings.Contains(string(table.Cells[0][i].Text), column))
			}
			for i, value := range []string{"row-key", "row-value"} {
				assert.False(t, table.Cells[1][i].IsHeader)
				assert.Equal(t, "left", table.Cells[1][i].Align)
				assert.Equal(t, "middle", table.Cells[1][i].Valign)
				assert.True(t, strings.Contains(string(table.Cells[1][i].Text), value))
			}
		})
	}
}
