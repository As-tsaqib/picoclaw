package telegram

import (
	"net/url"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

const telegramRichBlockMaxDepth = 24

func buildTypedNativeRichBlocks(
	blocks []bus.StructuredBlock,
	totalBytes *int,
	blockUnits *int,
	depth int,
) ([]telego.InputRichBlock, bool) {
	if depth >= telegramRichBlockMaxDepth {
		return nil, false
	}
	out := make([]telego.InputRichBlock, 0, len(blocks))
	for _, block := range blocks {
		kind := strings.ToLower(strings.TrimSpace(block.Type))
		text := strings.TrimSpace(block.Text)
		var native telego.InputRichBlock
		switch kind {
		case "paragraph":
			if text == "" {
				continue
			}
			native = tu.RichBlockParagraph(tu.RichTextPlain(text))
			*totalBytes += len([]byte(text))
		case "heading":
			if text == "" {
				continue
			}
			size := block.Level
			if size < 1 {
				size = 2
			}
			if size > 6 {
				return nil, false
			}
			native = tu.RichBlockSectionHeading(tu.RichTextPlain(text), size)
			*totalBytes += len([]byte(text))
		case "preformatted", "code":
			if text == "" {
				continue
			}
			pre := tu.RichBlockPreformatted(tu.RichTextPlain(text))
			pre.Language = strings.TrimSpace(block.Language)
			native = pre
			*totalBytes += len([]byte(text)) + len([]byte(pre.Language))
		case "footer":
			if text == "" {
				continue
			}
			native = tu.RichBlockFooter(tu.RichTextPlain(text))
			*totalBytes += len([]byte(text))
		case "divider":
			native = tu.RichBlockDivider()
		case "math", "expression":
			if text == "" {
				continue
			}
			native = tu.RichBlockMathematicalExpression(text)
			*totalBytes += len([]byte(text))
		case "anchor":
			if text == "" || strings.ContainsAny(text, "\r\n\x00") {
				return nil, false
			}
			native = tu.RichBlockAnchor(text)
			*totalBytes += len([]byte(text))
		case "link":
			if text == "" || !safeRichLink(block.URL) {
				return nil, false
			}
			link := strings.TrimSpace(block.URL)
			native = tu.RichBlockParagraph(tu.RichTextURL(tu.RichTextPlain(text), link))
			*totalBytes += len([]byte(text)) + len([]byte(link))
		case "list":
			items := make([]telego.InputRichBlockListItem, 0, len(block.Items))
			for index, item := range block.Items {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				listItem := tu.RichBlockListItem(tu.RichBlockParagraph(tu.RichTextPlain(item)))
				if block.Ordered {
					listItem.Value = index + 1
					listItem.Type = "1"
				}
				items = append(items, listItem)
				*totalBytes += len([]byte(item))
				(*blockUnits)++
			}
			if len(items) == 0 {
				continue
			}
			native = tu.RichBlockList(items...)
		case "quote", "quotation", "block_quote":
			nestedSource := append([]bus.StructuredBlock(nil), block.Blocks...)
			if text != "" {
				nestedSource = append([]bus.StructuredBlock{{Type: "paragraph", Text: text}}, nestedSource...)
			}
			nested, ok := buildTypedNativeRichBlocks(nestedSource, totalBytes, blockUnits, depth+1)
			if !ok || len(nested) == 0 {
				return nil, false
			}
			native = tu.RichBlockBlockQuotation(nested...)
		case "pull_quote":
			if text == "" {
				continue
			}
			native = tu.RichBlockPullQuotation(tu.RichTextPlain(text))
			*totalBytes += len([]byte(text))
		case "details", "disclosure":
			if text == "" {
				return nil, false
			}
			nested, ok := buildTypedNativeRichBlocks(block.Blocks, totalBytes, blockUnits, depth+1)
			if !ok || len(nested) == 0 {
				return nil, false
			}
			native = tu.RichBlockDetails(tu.RichTextPlain(text), nested...)
			*totalBytes += len([]byte(text))
		case "table":
			if block.Table == nil {
				return nil, false
			}
			table, units, bytes, ok := nativeStructuredTable(*block.Table)
			if !ok {
				return nil, false
			}
			native = table
			*blockUnits += units
			*totalBytes += bytes
		default:
			return nil, false
		}
		if native != nil {
			out = append(out, native)
			(*blockUnits)++
		}
		if *totalBytes > richMessageMaxBytes || *blockUnits > richMessageMaxBlocks {
			return nil, false
		}
	}
	return out, true
}

func nativeStructuredTable(table bus.StructuredTable) (*telego.InputRichBlockTable, int, int, bool) {
	if len(table.Columns) == 0 || len(table.Columns) > richMessageMaxColumns {
		return nil, 0, 0, false
	}
	cells := make([][]telego.RichBlockTableCell, 0, len(table.Rows)+1)
	bytes := 0
	if table.Header {
		header := make([]telego.RichBlockTableCell, len(table.Columns))
		for i, value := range table.Columns {
			bytes += len([]byte(value))
			header[i] = nativeTableCell(value, true)
		}
		cells = append(cells, header)
	}
	for _, row := range table.Rows {
		if len(row) > len(table.Columns) {
			return nil, 0, 0, false
		}
		nativeRow := make([]telego.RichBlockTableCell, len(table.Columns))
		for i := range table.Columns {
			value := ""
			if i < len(row) {
				value = row[i]
			}
			bytes += len([]byte(value))
			nativeRow[i] = nativeTableCell(value, false)
		}
		cells = append(cells, nativeRow)
	}
	tableBlock := tu.RichBlockTableGrid(cells)
	tableBlock.IsBordered = table.Border
	tableBlock.IsStriped = table.Striped
	if caption := strings.TrimSpace(table.Caption); caption != "" {
		bytes += len([]byte(caption))
		tableBlock.Caption = tu.RichTextPlain(caption)
	}
	return tableBlock, 1 + len(cells), bytes, true
}

func safeRichLink(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "http":
		return true
	default:
		return false
	}
}
