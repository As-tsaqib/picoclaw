package telegram

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

var (
	richHTMLTablePattern = regexp.MustCompile(`(?is)<table\b[^>]*>.*?</table\s*>`)
	tableSeparatorCell   = regexp.MustCompile(`^:?-{3,}:?$`)
)

// markdownTextSegment is a section of Markdown that is either normal text or a
// fenced code block. Tables shown as examples inside code fences must never
// trigger rich-message delivery or be rewritten by the fallback renderer.
type markdownTextSegment struct {
	text   string
	fenced bool
}

type parsedMarkdownTable struct {
	rows [][]string
	end  int // exclusive line index
}

// hasTelegramRichTable reports whether content contains a Markdown or HTML
// table that Telegram's rich-message parser can render natively.
func hasTelegramRichTable(content string) bool {
	for _, segment := range splitMarkdownFencedSegments(content) {
		if segment.fenced {
			continue
		}

		protected := extractInlineCodes(segment.text)
		if richHTMLTablePattern.MatchString(protected.text) {
			return true
		}

		lines := strings.Split(protected.text, "\n")
		for i := 0; i+1 < len(lines); i++ {
			if _, ok := parseMarkdownTableAt(lines, i); ok {
				return true
			}
		}
	}

	return false
}

// telegramTableFallbackMarkdown converts native-table input into fenced code
// blocks. The existing HTML/MarkdownV2 formatter then maps those fences to a
// Telegram "pre" entity, providing a readable monospaced fallback.
func telegramTableFallbackMarkdown(content string) string {
	return renderTelegramTableFallback(content, true)
}

// telegramTableFallbackPlainText produces the final no-parse-mode fallback.
// It keeps the aligned table but omits the generated Markdown fences when
// Telegram rejects both formatted attempts.
func telegramTableFallbackPlainText(content string) string {
	return renderTelegramTableFallback(content, false)
}

func renderTelegramTableFallback(content string, fenced bool) string {
	segments := splitMarkdownFencedSegments(content)
	var result strings.Builder

	for _, segment := range segments {
		if segment.fenced {
			result.WriteString(segment.text)
			continue
		}

		inlineCodes := extractInlineCodes(segment.text)
		var htmlFallbacks []string
		text := richHTMLTablePattern.ReplaceAllStringFunc(inlineCodes.text, func(tableHTML string) string {
			rows, ok := parseHTMLTableRows(tableHTML)
			if !ok {
				return tableHTML
			}
			placeholder := fmt.Sprintf("\x00HT%d\x00", len(htmlFallbacks))
			htmlFallbacks = append(htmlFallbacks, surroundTableFallback(renderMonospacedTable(rows), fenced))
			return placeholder
		})
		text = replaceMarkdownTables(text, fenced)
		for i, fallback := range htmlFallbacks {
			text = strings.ReplaceAll(text, fmt.Sprintf("\x00HT%d\x00", i), fallback)
		}

		for i, code := range inlineCodes.codes {
			text = strings.ReplaceAll(text, fmt.Sprintf("\x00IC%d\x00", i), "`"+code+"`")
		}
		result.WriteString(text)
	}

	return result.String()
}

func replaceMarkdownTables(content string, fenced bool) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))

	for i := 0; i < len(lines); {
		if table, ok := parseMarkdownTableAt(lines, i); ok {
			out = append(out, surroundTableFallback(renderMonospacedTable(table.rows), fenced))
			i = table.end
			continue
		}
		out = append(out, lines[i])
		i++
	}

	return strings.Join(out, "\n")
}

func parseMarkdownTableAt(lines []string, start int) (parsedMarkdownTable, bool) {
	if start < 0 || start+1 >= len(lines) {
		return parsedMarkdownTable{}, false
	}

	header, hasHeaderPipes := splitMarkdownTableRow(lines[start])
	separator, hasSeparatorPipes := splitMarkdownTableRow(lines[start+1])
	if !hasHeaderPipes || !hasSeparatorPipes || len(header) == 0 || len(separator) != len(header) {
		return parsedMarkdownTable{}, false
	}
	for _, cell := range separator {
		cell = strings.Join(strings.Fields(cell), "")
		if !tableSeparatorCell.MatchString(cell) {
			return parsedMarkdownTable{}, false
		}
	}

	rows := [][]string{normalizeTableRow(header, len(header))}
	end := start + 2
	for end < len(lines) {
		if strings.TrimSpace(lines[end]) == "" {
			break
		}
		row, hasPipes := splitMarkdownTableRow(lines[end])
		if !hasPipes {
			break
		}
		rows = append(rows, normalizeTableRow(row, len(header)))
		end++
	}

	return parsedMarkdownTable{rows: rows, end: end}, true
}

// splitMarkdownTableRow handles escaped pipes and pipes inside inline code. It
// intentionally accepts optional leading/trailing pipes, matching GFM tables.
func splitMarkdownTableRow(line string) ([]string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, false
	}

	var (
		cells        []string
		cell         strings.Builder
		hasPipe      bool
		codeTicks    int
		leadingPipe  bool
		trailingPipe bool
	)

	for i := 0; i < len(line); {
		switch line[i] {
		case '\\':
			if i+1 < len(line) && line[i+1] == '|' {
				cell.WriteByte('|')
				i += 2
				continue
			}
			cell.WriteByte(line[i])
			i++
		case '`':
			run := 1
			for i+run < len(line) && line[i+run] == '`' {
				run++
			}
			cell.WriteString(line[i : i+run])
			if codeTicks == 0 {
				codeTicks = run
			} else if codeTicks == run {
				codeTicks = 0
			}
			i += run
		case '|':
			if codeTicks != 0 {
				cell.WriteByte('|')
				i++
				continue
			}
			hasPipe = true
			if len(cells) == 0 && strings.TrimSpace(cell.String()) == "" {
				leadingPipe = true
			}
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
			i++
			trailingPipe = strings.TrimSpace(line[i:]) == ""
		default:
			cell.WriteByte(line[i])
			i++
		}
	}
	cells = append(cells, strings.TrimSpace(cell.String()))

	if leadingPipe && len(cells) > 0 {
		cells = cells[1:]
	}
	if trailingPipe && len(cells) > 0 {
		cells = cells[:len(cells)-1]
	}
	return cells, hasPipe
}

func normalizeTableRow(row []string, columns int) []string {
	normalized := make([]string, columns)
	for i := 0; i < columns && i < len(row); i++ {
		normalized[i] = collapseTableCellWhitespace(row[i])
	}
	return normalized
}

func collapseTableCellWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func parseHTMLTableRows(tableHTML string) ([][]string, bool) {
	doc, err := html.Parse(strings.NewReader(tableHTML))
	if err != nil {
		return nil, false
	}

	table := findHTMLElement(doc, "table")
	if table == nil {
		return nil, false
	}

	var rows [][]string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "table" && node != table {
			return
		}
		if node.Type == html.ElementNode && node.Data == "tr" && nearestHTMLAncestor(node, "table") == table {
			var cells []string
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				collectHTMLRowCells(child, node, &cells)
			}
			if len(cells) > 0 {
				rows = append(rows, cells)
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(table)

	if len(rows) == 0 {
		return nil, false
	}
	columns := 0
	for _, row := range rows {
		if len(row) > columns {
			columns = len(row)
		}
	}
	for i := range rows {
		rows[i] = normalizeTableRow(rows[i], columns)
	}
	return rows, true
}

func findHTMLElement(node *html.Node, name string) *html.Node {
	if node.Type == html.ElementNode && node.Data == name {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findHTMLElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

func nearestHTMLAncestor(node *html.Node, name string) *html.Node {
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode && parent.Data == name {
			return parent
		}
	}
	return nil
}

func collectHTMLRowCells(node, row *html.Node, cells *[]string) {
	if node.Type == html.ElementNode && (node.Data == "td" || node.Data == "th") &&
		nearestHTMLAncestor(node, "tr") == row {
		*cells = append(*cells, collapseTableCellWhitespace(htmlNodeText(node)))
		return
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectHTMLRowCells(child, row, cells)
	}
}

func htmlNodeText(node *html.Node) string {
	var result strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			result.WriteString(current.Data)
			return
		}
		if current.Type == html.ElementNode && current.Data == "br" {
			result.WriteByte(' ')
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return result.String()
}

func renderMonospacedTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}

	columns := 0
	for _, row := range rows {
		if len(row) > columns {
			columns = len(row)
		}
	}
	if columns == 0 {
		return ""
	}

	widths := make([]int, columns)
	for _, row := range rows {
		for column := 0; column < columns; column++ {
			value := ""
			if column < len(row) {
				value = collapseTableCellWhitespace(row[column])
			}
			if width := utf8.RuneCountInString(value); width > widths[column] {
				widths[column] = width
			}
		}
	}

	var result strings.Builder
	writeRow := func(row []string) {
		result.WriteString("| ")
		for column := 0; column < columns; column++ {
			if column > 0 {
				result.WriteString(" | ")
			}
			value := ""
			if column < len(row) {
				value = collapseTableCellWhitespace(row[column])
			}
			result.WriteString(value)
			result.WriteString(strings.Repeat(" ", widths[column]-utf8.RuneCountInString(value)))
		}
		result.WriteString(" |\n")
	}

	writeRow(rows[0])
	result.WriteString("|-")
	for column, width := range widths {
		if column > 0 {
			result.WriteString("-|-")
		}
		if width < 3 {
			width = 3
		}
		result.WriteString(strings.Repeat("-", width))
	}
	result.WriteString("-|\n")
	for _, row := range rows[1:] {
		writeRow(row)
	}

	return strings.TrimSuffix(result.String(), "\n")
}

func surroundTableFallback(table string, fenced bool) string {
	if !fenced {
		return table
	}
	// Avoid accidentally terminating the generated code block when a cell
	// contains a literal fence.
	table = strings.ReplaceAll(table, "```", "`` `")
	return "```\n" + table + "\n```"
}

func splitMarkdownFencedSegments(content string) []markdownTextSegment {
	lines := strings.SplitAfter(content, "\n")
	segments := make([]markdownTextSegment, 0, 3)
	var current strings.Builder
	var fence byte
	var fenceLength int
	currentFenced := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		segments = append(segments, markdownTextSegment{text: current.String(), fenced: currentFenced})
		current.Reset()
	}

	for _, line := range lines {
		marker, length, ok := markdownFenceMarker(line)
		if fence == 0 && ok {
			flush()
			currentFenced = true
			fence = marker
			fenceLength = length
			current.WriteString(line)
			continue
		}

		current.WriteString(line)
		if fence != 0 && ok && marker == fence && length >= fenceLength {
			flush()
			currentFenced = false
			fence = 0
			fenceLength = 0
		}
	}
	flush()

	return segments
}

func markdownFenceMarker(line string) (byte, int, bool) {
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	indent := 0
	for indent < len(line) && indent < 4 && line[indent] == ' ' {
		indent++
	}
	if indent > 3 || indent >= len(line) {
		return 0, 0, false
	}
	marker := line[indent]
	if marker != '`' && marker != '~' {
		return 0, 0, false
	}
	length := 1
	for indent+length < len(line) && line[indent+length] == marker {
		length++
	}
	return marker, length, length >= 3
}
